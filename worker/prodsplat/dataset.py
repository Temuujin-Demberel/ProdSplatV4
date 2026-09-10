from __future__ import annotations

from pathlib import Path
import hashlib
import json
import random
import shutil
import zipfile
from PIL import Image, ImageEnhance

from .lease import LeaseGuard

SUPPORTED = {".png", ".jpg", ".jpeg", ".webp"}


def _images(folder: str | Path):
    folder = Path(folder)
    if not folder.exists():
        return []
    return [p for p in sorted(folder.iterdir()) if p.is_file() and p.suffix.lower() in SUPPORTED]


def _bbox(alpha: Image.Image, threshold: int = 4):
    return alpha.point(lambda p: 255 if p > threshold else 0).getbbox()


def _safe_stem(path: Path) -> str:
    return "".join(c if c.isalnum() or c in "-_" else "_" for c in path.stem)


def build_yolo_dataset(
    render_dir: str,
    background_dir: str,
    dataset_dir: str,
    guard: LeaseGuard,
    copies_per_render: int = 4,
    validation_fraction: float = 0.1,
):
    renders = _images(render_dir)
    backgrounds = _images(background_dir)
    if not renders:
        raise ValueError("no RGBA renders found")
    if not backgrounds:
        raise ValueError("no background images found")

    root = Path(dataset_dir)
    staging = root.with_name(root.name + ".partial")
    shutil.rmtree(staging, ignore_errors=True)
    for split in ("train", "val"):
        (staging / "images" / split).mkdir(parents=True, exist_ok=True)
        (staging / "labels" / split).mkdir(parents=True, exist_ok=True)

    seed_material = "|".join(p.name for p in renders + backgrounds).encode("utf-8")
    seed = int(hashlib.sha256(seed_material).hexdigest()[:16], 16)
    rng = random.Random(seed)
    planned = len(renders) * copies_per_render
    manifest = []
    sample_index = 0

    for render_index, render_path in enumerate(renders):
        guard.check()
        source_obj = Image.open(render_path).convert("RGBA")
        for copy_index in range(copies_per_render):
            guard.check()
            progress = 0.05 + 0.9 * (sample_index / max(planned, 1))
            guard.update(progress, f"compositing synthetic sample {sample_index + 1}/{planned}")

            background_path = rng.choice(backgrounds)
            bg = Image.open(background_path).convert("RGB")

            # Mild photometric variation only on the product RGB channels.
            obj = source_obj.copy()
            rgb = Image.merge("RGB", obj.split()[:3])
            rgb = ImageEnhance.Brightness(rgb).enhance(rng.uniform(0.9, 1.1))
            rgb = ImageEnhance.Contrast(rgb).enhance(rng.uniform(0.9, 1.1))
            obj = Image.merge("RGBA", (*rgb.split(), obj.getchannel("A")))

            angle = rng.uniform(-6.0, 6.0)
            obj = obj.rotate(angle, resample=Image.Resampling.BICUBIC, expand=True)

            target_fraction = rng.uniform(0.16, 0.38)
            target_w = max(48, int(bg.width * target_fraction))
            scale = target_w / max(obj.width, 1)
            ow = max(8, int(obj.width * scale))
            oh = max(8, int(obj.height * scale))
            if oh > int(bg.height * 0.82):
                scale *= (bg.height * 0.82) / oh
                ow = max(8, int(obj.width * scale))
                oh = max(8, int(obj.height * scale))
            obj = obj.resize((ow, oh), Image.Resampling.LANCZOS)

            alpha_bbox = _bbox(obj.getchannel("A"))
            if alpha_bbox is None:
                continue

            # Bias y toward the lower 70% of the shelf image while remaining valid.
            max_x = max(0, bg.width - ow)
            max_y = max(0, bg.height - oh)
            x = rng.randint(0, max_x) if max_x else 0
            y_low = int(max_y * 0.25)
            y = rng.randint(y_low, max_y) if max_y > y_low else max_y

            canvas = bg.copy()
            canvas.paste(obj, (x, y), obj)

            left, top, right, bottom = alpha_bbox
            left += x; right += x; top += y; bottom += y
            xc = ((left + right) / 2.0) / bg.width
            yc = ((top + bottom) / 2.0) / bg.height
            bw = (right - left) / bg.width
            bh = (bottom - top) / bg.height

            # Deterministic split. The validation set is still synthetic; research
            # evaluation should use a separate real held-out test set.
            split = "val" if rng.random() < validation_fraction else "train"
            stem = f"synthetic_{sample_index:06d}"
            image_path = staging / "images" / split / f"{stem}.jpg"
            label_path = staging / "labels" / split / f"{stem}.txt"
            canvas.save(image_path, quality=92, subsampling=0)
            label_path.write_text(f"0 {xc:.6f} {yc:.6f} {bw:.6f} {bh:.6f}\n", encoding="utf-8")

            manifest.append({
                "sample": stem,
                "split": split,
                "render": render_path.name,
                "background": background_path.name,
                "rotationDegrees": angle,
                "placementXY": [x, y],
                "objectSizeWH": [ow, oh],
                "bboxPixels": [left, top, right, bottom],
                "yolo": [0, xc, yc, bw, bh],
            })
            sample_index += 1

    if sample_index == 0:
        raise ValueError("dataset generation produced zero valid samples")

    (staging / "data.yaml").write_text(
        "path: .\ntrain: images/train\nval: images/val\nnames:\n  0: product\n",
        encoding="utf-8",
    )
    (staging / "manifest.json").write_text(json.dumps({
        "seed": seed,
        "classNames": ["product"],
        "sampleCount": sample_index,
        "copiesPerRender": copies_per_render,
        "syntheticValidationWarning": "Use a separate real held-out set for scientific evaluation.",
        "samples": manifest,
    }, indent=2), encoding="utf-8")

    guard.update(0.97, "committing synthetic dataset")
    shutil.rmtree(root, ignore_errors=True)
    staging.rename(root)

    zip_path = root.parent / "dataset.zip"
    temp_zip = root.parent / "dataset.zip.partial"
    if temp_zip.exists():
        temp_zip.unlink()
    with zipfile.ZipFile(temp_zip, "w", zipfile.ZIP_DEFLATED) as archive:
        for path in root.rglob("*"):
            if path.is_file():
                archive.write(path, path.relative_to(root.parent))
    temp_zip.replace(zip_path)
    return str(zip_path), sample_index
