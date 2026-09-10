from __future__ import annotations

from pathlib import Path
import json
import shutil

from .lease import LeaseGuard
from .process import run_command
from .ply import read_gaussian_ply
from .quality import (
    build_frame_plan,
    inspect_registration,
    inspect_video,
    registration_is_usable,
)


def _append_log(log_path: Path, message: str) -> None:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("a", encoding="utf-8") as log:
        log.write(message.rstrip() + "\n")


def _process_video(
    *,
    video_path: Path,
    dataset_dir: Path,
    frames: int,
    matching_method: str,
    log_path: Path,
    guard: LeaseGuard,
    progress: float,
    label: str,
) -> dict:
    shutil.rmtree(dataset_dir, ignore_errors=True)
    run_command([
        "ns-process-data", "video",
        "--data", str(video_path),
        "--output-dir", str(dataset_dir),
        "--num-frames-target", str(frames),
        "--matching-method", matching_method,
        "--sfm-tool", "colmap",
    ], log_path, guard, progress, label)
    registration = inspect_registration(dataset_dir)
    registration["requestedFrameTarget"] = frames
    registration["matchingMethod"] = matching_method
    return registration


def reconstruct(task: dict, guard: LeaseGuard):
    payload = task["payload"]
    attempt_dir = Path(payload["attemptDir"])
    video_path = Path(payload["videoPath"])
    task_id = task["id"]
    log_path = Path(task["logPath"])

    profile = payload.get("profile", "balanced")
    requested_frames = int(payload["frames"])
    iterations = int(payload["iterations"])
    data_downscale = int(payload["dataDownscale"])
    model_downscales = int(payload["modelDownscales"])
    resolution_schedule = int(payload["resolutionSchedule"])

    # Task-specific scratch makes lease retries idempotent. Stable output names are
    # only replaced after every stage succeeds.
    scratch = attempt_dir / "work" / task_id
    shutil.rmtree(scratch, ignore_errors=True)
    training_dir = scratch / "training"
    export_dir = scratch / "export"
    scratch.mkdir(parents=True, exist_ok=True)

    guard.update(0.02, "inspecting capture quality")
    capture_quality = inspect_video(str(video_path))
    frame_plan = build_frame_plan(capture_quality, profile, requested_frames)
    quality_path = attempt_dir / "quality.json"

    duration = capture_quality.get("durationSeconds")
    duration_text = f"{duration:.1f}s" if duration is not None else "unknown"
    _append_log(
        log_path,
        "[ProdSplat] adaptive frame plan: "
        f"profile={profile}, duration={duration_text}, source_frames={capture_quality['frameCount']}, "
        f"primary={frame_plan['primaryFrames']}, rescue={frame_plan['rescueFrames']}, "
        f"exhaustive_rescue={frame_plan['exhaustiveFrames']}",
    )

    candidates: list[tuple[Path, dict]] = []

    primary_dir = scratch / "dataset-primary"
    primary = _process_video(
        video_path=video_path,
        dataset_dir=primary_dir,
        frames=frame_plan["primaryFrames"],
        matching_method="sequential",
        log_path=log_path,
        guard=guard,
        progress=0.05,
        label=f"processing {frame_plan['primaryFrames']} adaptive frames with sequential COLMAP",
    )
    candidates.append((primary_dir, primary))
    _append_log(
        log_path,
        f"[ProdSplat] primary registration: {primary['registeredFrameCount']}/"
        f"{primary['extractedImageCount']} ({primary['registrationRatio']:.1%})",
    )

    selected_dir = primary_dir
    selected_registration = primary

    # If the initial adaptive pass is weak, retry sequential matching with a
    # denser temporal sample. This is especially important for long videos where
    # a fixed 90-frame extraction leaves multi-second gaps between views.
    if not registration_is_usable(selected_registration) and frame_plan["rescueFrames"] > frame_plan["primaryFrames"]:
        rescue_dir = scratch / "dataset-rescue"
        rescue = _process_video(
            video_path=video_path,
            dataset_dir=rescue_dir,
            frames=frame_plan["rescueFrames"],
            matching_method="sequential",
            log_path=log_path,
            guard=guard,
            progress=0.09,
            label=f"retrying with {frame_plan['rescueFrames']} denser sequential frames",
        )
        candidates.append((rescue_dir, rescue))
        _append_log(
            log_path,
            f"[ProdSplat] dense rescue registration: {rescue['registeredFrameCount']}/"
            f"{rescue['extractedImageCount']} ({rescue['registrationRatio']:.1%})",
        )
        if registration_is_usable(rescue) or (
            rescue["registeredFrameCount"], rescue["registrationRatio"]
        ) > (
            selected_registration["registeredFrameCount"], selected_registration["registrationRatio"]
        ):
            selected_dir = rescue_dir
            selected_registration = rescue

    # Last-resort topology recovery: exhaustive matching can reconnect a capture
    # whose useful views are not temporally adjacent. It is deliberately capped
    # to keep all-pairs matching bounded on a workstation GPU.
    if not registration_is_usable(selected_registration):
        exhaustive_dir = scratch / "dataset-exhaustive"
        exhaustive = _process_video(
            video_path=video_path,
            dataset_dir=exhaustive_dir,
            frames=frame_plan["exhaustiveFrames"],
            matching_method="exhaustive",
            log_path=log_path,
            guard=guard,
            progress=0.13,
            label=f"trying bounded exhaustive rescue with {frame_plan['exhaustiveFrames']} frames",
        )
        candidates.append((exhaustive_dir, exhaustive))
        _append_log(
            log_path,
            f"[ProdSplat] exhaustive rescue registration: {exhaustive['registeredFrameCount']}/"
            f"{exhaustive['extractedImageCount']} ({exhaustive['registrationRatio']:.1%})",
        )
        if registration_is_usable(exhaustive) or (
            exhaustive["registeredFrameCount"], exhaustive["registrationRatio"]
        ) > (
            selected_registration["registeredFrameCount"], selected_registration["registrationRatio"]
        ):
            selected_dir = exhaustive_dir
            selected_registration = exhaustive

    quality = {
        "capture": capture_quality,
        "framePlan": frame_plan,
        "registrationAttempts": [registration for _, registration in candidates],
        "registration": selected_registration,
    }
    quality_path.write_text(json.dumps(quality, indent=2), encoding="utf-8")

    if not registration_is_usable(selected_registration):
        raise RuntimeError(
            "camera registration remained too low after adaptive retries: "
            f"best {selected_registration['registeredFrameCount']}/"
            f"{selected_registration['extractedImageCount']} "
            f"({selected_registration['registrationRatio']:.1%}); "
            "inspect quality.json/task log and recapture with slower motion and stronger overlap if needed"
        )

    # Normalize the winning dataset path so the rest of the pipeline and the
    # retained attempt layout stay stable regardless of which recovery pass won.
    dataset_dir = scratch / "dataset"
    shutil.rmtree(dataset_dir, ignore_errors=True)
    selected_dir.rename(dataset_dir)
    for candidate_dir, _ in candidates:
        if candidate_dir != selected_dir:
            shutil.rmtree(candidate_dir, ignore_errors=True)

    guard.update(0.22, "starting Splatfacto optimization")
    run_command([
        "ns-train", "splatfacto",
        "--data", str(dataset_dir),
        "--output-dir", str(training_dir),
        "--max-num-iterations", str(iterations),
        "--vis", "tensorboard",
        "--pipeline.model.num-downscales", str(model_downscales),
        "--pipeline.model.resolution-schedule", str(resolution_schedule),
        "nerfstudio-data",
        "--downscale-factor", str(data_downscale),
    ], log_path, guard, 0.24, f"training Splatfacto ({iterations} iterations)")

    configs = sorted(training_dir.rglob("config.yml"), key=lambda p: p.stat().st_mtime)
    if not configs:
        raise RuntimeError("training completed but Nerfstudio config.yml was not found")
    config_path = configs[-1]

    export_dir.mkdir(parents=True, exist_ok=True)
    run_command([
        "ns-export", "gaussian-splat",
        "--load-config", str(config_path),
        "--output-dir", str(export_dir),
        "--output-filename", "splat.ply",
    ], log_path, guard, 0.90, "exporting Gaussian PLY")

    exported = export_dir / "splat.ply"
    if not exported.exists():
        matches = list(export_dir.rglob("*.ply"))
        if not matches:
            raise RuntimeError("Gaussian export completed but no PLY was produced")
        exported = matches[0]

    guard.update(0.96, "validating Gaussian asset")
    gaussian = read_gaussian_ply(exported)
    if gaussian.count < 100:
        raise RuntimeError(f"reconstruction produced suspiciously few Gaussians: {gaussian.count}")

    final_splat = attempt_dir / "splat.ply"
    final_config = attempt_dir / "config.yml"
    final_dataset = attempt_dir / "dataset"
    final_training = attempt_dir / "training"

    # Publish the compact asset first through a same-filesystem atomic rename.
    temp_splat = final_splat.with_suffix(".ply.partial")
    shutil.copy2(exported, temp_splat)
    temp_splat.replace(final_splat)

    # Move, rather than copy, the large processed dataset and training tree.
    # This avoids briefly doubling disk use on high-resolution jobs. These are
    # retained so an accepted reconstruction remains auditable/reproducible.
    shutil.rmtree(final_dataset, ignore_errors=True)
    dataset_dir.rename(final_dataset)
    shutil.rmtree(final_training, ignore_errors=True)
    training_dir.rename(final_training)

    # Nerfstudio config files contain absolute output/data paths. Rewrite the
    # paths after moving the retained trees so future inspection/resume commands
    # do not point at the task scratch directory.
    relative_config = config_path.relative_to(training_dir)
    moved_config = final_training / relative_config
    replacements = {
        str(dataset_dir): str(final_dataset),
        str(training_dir): str(final_training),
    }
    for candidate in final_training.rglob("config.yml"):
        text = candidate.read_text(encoding="utf-8")
        for source, destination in replacements.items():
            text = text.replace(source, destination)
        candidate.write_text(text, encoding="utf-8")

    # Keep a convenient top-level copy of the selected training configuration.
    shutil.copy2(moved_config, final_config.with_suffix(".yml.partial"))
    final_config.with_suffix(".yml.partial").replace(final_config)

    # Export scratch is no longer needed after splat.ply is committed.
    shutil.rmtree(scratch, ignore_errors=True)

    guard.update(0.99, "reconstruction ready for review")
    return {
        "splatPath": str(final_splat),
        "configPath": str(final_config),
        "gaussianCount": str(gaussian.count),
        "qualityPath": str(quality_path),
        "registrationRatio": f"{selected_registration['registrationRatio']:.6f}",
        "registeredFrameCount": str(selected_registration["registeredFrameCount"]),
        "selectedFrameTarget": str(selected_registration["requestedFrameTarget"]),
        "selectedMatchingMethod": selected_registration["matchingMethod"],
    }
