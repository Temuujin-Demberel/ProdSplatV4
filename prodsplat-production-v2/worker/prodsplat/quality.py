from __future__ import annotations

from pathlib import Path
import json
import math
import statistics


# Frame sampling is intentionally adaptive. The old CLI workflow used 90 frames
# successfully for short object captures, but a fixed 90-frame target becomes too
# sparse for multi-minute videos. Each profile therefore keeps its historical
# frame value as a *minimum* while the worker increases sampling density for long
# captures. Rescue passes are only used when COLMAP registration is poor.
FRAME_POLICIES = {
    "fast": {
        "primary_fps": 2.0,
        "primary_cap": 240,
        "rescue_fps": 3.0,
        "rescue_cap": 360,
        "exhaustive_cap": 180,
    },
    "balanced": {
        "primary_fps": 3.0,
        "primary_cap": 450,
        "rescue_fps": 5.0,
        "rescue_cap": 700,
        "exhaustive_cap": 260,
    },
    "quality": {
        "primary_fps": 4.0,
        "primary_cap": 650,
        "rescue_fps": 6.0,
        "rescue_cap": 900,
        "exhaustive_cap": 320,
    },
}


def inspect_video(video_path: str, max_samples: int = 20) -> dict:
    import cv2
    import numpy as np

    cap = cv2.VideoCapture(video_path)
    if not cap.isOpened():
        raise ValueError("video cannot be opened")
    frame_count = int(cap.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    fps = float(cap.get(cv2.CAP_PROP_FPS) or 0.0)
    duration = frame_count / fps if fps > 0 else None
    if frame_count <= 0:
        cap.release()
        raise ValueError("video reports zero frames")

    sample_indices = np.linspace(0, max(frame_count - 1, 0), min(max_samples, frame_count), dtype=int)
    blur = []
    brightness = []
    for index in sample_indices:
        cap.set(cv2.CAP_PROP_POS_FRAMES, int(index))
        ok, frame = cap.read()
        if not ok or frame is None:
            continue
        gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
        blur.append(float(cv2.Laplacian(gray, cv2.CV_64F).var()))
        brightness.append(float(gray.mean()))
    cap.release()
    if not blur:
        raise ValueError("no video frames could be decoded")

    median_blur = statistics.median(blur)
    mean_brightness = statistics.mean(brightness)
    warnings = []
    if median_blur < 20:
        warnings.append("sampled frames appear very blurry; recapture may be required")
    if mean_brightness < 25:
        warnings.append("capture is very dark")
    if mean_brightness > 235:
        warnings.append("capture is strongly overexposed")
    if duration is not None and duration < 3:
        warnings.append("capture is very short and may not provide enough viewpoint coverage")
    if duration is not None and duration > 120:
        warnings.append(
            "capture is unusually long for an object scan; adaptive sampling will be used, "
            "but a shorter 20-90 second capture usually gives stronger overlap"
        )

    return {
        "frameCount": frame_count,
        "fps": fps,
        "durationSeconds": duration,
        "sampleCount": len(blur),
        "medianLaplacianVariance": median_blur,
        "meanBrightness": mean_brightness,
        "warnings": warnings,
    }


def build_frame_plan(capture: dict, profile: str, requested_frames: int) -> dict:
    """Return adaptive primary/rescue frame targets for a capture.

    `requested_frames` is the historical profile value (60/90/140) and is kept
    as a lower bound for normal captures. Longer captures receive more evenly
    spaced frames so sequential COLMAP matching does not see huge temporal gaps.
    """

    policy = FRAME_POLICIES.get(profile, FRAME_POLICIES["balanced"])
    frame_count = max(1, int(capture.get("frameCount") or 1))
    duration = capture.get("durationSeconds")
    duration = float(duration) if duration is not None else 0.0

    primary_by_duration = math.ceil(duration * policy["primary_fps"]) if duration > 0 else requested_frames
    primary = max(int(requested_frames), primary_by_duration)
    primary = min(primary, int(policy["primary_cap"]), frame_count)

    rescue_by_duration = math.ceil(duration * policy["rescue_fps"]) if duration > 0 else primary
    rescue = max(primary, rescue_by_duration)
    rescue = min(rescue, int(policy["rescue_cap"]), frame_count)

    # Exhaustive matching is a last-resort topology rescue. Cap the pair count so
    # a difficult upload cannot unexpectedly create an enormous all-pairs job.
    exhaustive_by_duration = math.ceil(duration * 1.5) if duration > 0 else requested_frames
    exhaustive = max(int(requested_frames), 120, exhaustive_by_duration)
    exhaustive = min(exhaustive, int(policy["exhaustive_cap"]), frame_count)

    return {
        "profile": profile,
        "requestedFrames": int(requested_frames),
        "primaryFrames": int(primary),
        "rescueFrames": int(rescue),
        "exhaustiveFrames": int(exhaustive),
        "primarySamplingFps": float(policy["primary_fps"]),
        "rescueSamplingFps": float(policy["rescue_fps"]),
    }


def inspect_registration(dataset_dir: str | Path) -> dict:
    dataset_dir = Path(dataset_dir)
    transforms = dataset_dir / "transforms.json"
    if not transforms.exists():
        raise ValueError("Nerfstudio transforms.json was not produced")
    metadata = json.loads(transforms.read_text(encoding="utf-8"))
    registered = len(metadata.get("frames", []))
    image_dir = dataset_dir / "images"
    extracted = len([p for p in image_dir.iterdir() if p.is_file()]) if image_dir.exists() else registered
    ratio = registered / max(extracted, 1)
    warnings = []
    if ratio < 0.7:
        warnings.append("low camera registration ratio")
    if registered < 30:
        warnings.append("very few cameras were registered")
    return {
        "extractedImageCount": extracted,
        "registeredFrameCount": registered,
        "registrationRatio": ratio,
        "warnings": warnings,
    }


def registration_is_usable(registration: dict) -> bool:
    """Decide whether a COLMAP model has enough cameras for Splatfacto.

    A ratio-only gate unfairly rejects dense adaptive extraction: e.g. 100 useful
    registered cameras out of 600 extracted frames is still a substantial 3DGS
    training set. We therefore accept either a healthy ratio or a sufficiently
    large absolute registered-camera set, while still rejecting tiny models.
    """

    registered = int(registration.get("registeredFrameCount") or 0)
    ratio = float(registration.get("registrationRatio") or 0.0)

    if registered < 24:
        return False
    if ratio >= 0.35:
        return True
    if registered >= 60 and ratio >= 0.10:
        return True
    if registered >= 100:
        return True
    return False
