from __future__ import annotations

from pathlib import Path
import json
import math
import shutil
import numpy as np
from PIL import Image
import torch

from .lease import LeaseGuard
from .ply import read_gaussian_ply

C0 = 0.28209479177387814


def _look_at(camera: np.ndarray, target: np.ndarray) -> np.ndarray:
    """Create OpenCV-style world-to-camera matrix (+x right, +y down, +z forward)."""
    world_up = np.array([0.0, 0.0, 1.0], np.float32)
    forward = target - camera
    forward = forward / max(np.linalg.norm(forward), 1e-8)
    right = np.cross(forward, world_up)
    if np.linalg.norm(right) < 1e-6:
        world_up = np.array([0.0, 1.0, 0.0], np.float32)
        right = np.cross(forward, world_up)
    right = right / max(np.linalg.norm(right), 1e-8)
    up = np.cross(right, forward)
    up = up / max(np.linalg.norm(up), 1e-8)
    rotation = np.stack([right, -up, forward], axis=0)
    translation = -rotation @ camera
    view = np.eye(4, dtype=np.float32)
    view[:3, :3] = rotation
    view[:3, 3] = translation
    return view


def _robust_bounds(means: np.ndarray):
    lo = np.percentile(means, 1.0, axis=0)
    hi = np.percentile(means, 99.0, axis=0)
    center = (lo + hi) * 0.5
    extent = np.maximum(hi - lo, 1e-4)
    return center.astype(np.float32), extent.astype(np.float32)


def _camera_rig(means: np.ndarray, fov_degrees: float = 50.0, views_per_ring: int = 16):
    center, extent = _robust_bounds(means)
    half_diag = float(np.linalg.norm(extent) * 0.5)
    radius = max(half_diag / math.tan(math.radians(fov_degrees) * 0.5) * 1.25, 0.25)
    upper_z = float(extent[2]) * 0.85
    for ring_name, z_offset in (("eye", 0.0), ("upper", upper_z)):
        for i in range(views_per_ring):
            angle = 2.0 * math.pi * i / views_per_ring
            camera = center + np.array([radius * math.cos(angle), radius * math.sin(angle), z_offset], np.float32)
            yield ring_name, i, camera, center, fov_degrees


def _load_tensors(ply_path: str):
    p = read_gaussian_ply(ply_path)
    device = torch.device("cuda")
    means_np = p.means()
    means = torch.from_numpy(means_np).to(device=device, dtype=torch.float32)
    quats = torch.from_numpy(np.stack([p.require(f"rot_{i}") for i in range(4)], axis=-1)).to(device=device, dtype=torch.float32)
    quats = torch.nn.functional.normalize(quats, dim=-1)
    log_scales = torch.from_numpy(np.stack([p.require(f"scale_{i}") for i in range(3)], axis=-1)).to(device=device, dtype=torch.float32)
    scales = torch.exp(log_scales)
    opacities = torch.sigmoid(torch.from_numpy(p.require("opacity")).to(device=device, dtype=torch.float32))

    sh, sh_degree = p.sh_coefficients()
    if sh is not None:
        colors = torch.from_numpy(sh).to(device=device, dtype=torch.float32)
    else:
        if not all(name in p.values for name in ("red", "green", "blue")):
            raise ValueError("PLY has neither SH coefficients nor RGB fields")
        rgb = np.stack([p.require("red"), p.require("green"), p.require("blue")], axis=-1) / 255.0
        colors = torch.from_numpy(rgb.astype(np.float32)).to(device)
        sh_degree = None
    return means_np, means, quats, scales, opacities, colors, sh_degree


def render_rgba_views(ply_path: str, output_dir: str, guard: LeaseGuard, size: int = 768, views_per_ring: int = 16):
    from gsplat import rasterization

    output = Path(output_dir)
    staging = output.with_name(output.name + ".partial")
    shutil.rmtree(staging, ignore_errors=True)
    staging.mkdir(parents=True, exist_ok=True)

    guard.update(0.08, "loading Gaussian asset")
    means_np, means, quats, scales, opacities, colors, sh_degree = _load_tensors(ply_path)
    guard.check()

    created = []
    camera_manifest = []
    cameras = list(_camera_rig(means_np, views_per_ring=views_per_ring))
    total = len(cameras)

    with torch.inference_mode():
        for index, (ring, view_index, camera, target, fov) in enumerate(cameras):
            guard.check()
            progress = 0.1 + 0.85 * (index / max(total, 1))
            guard.update(progress, f"rendering view {index + 1}/{total}")

            focal = 0.5 * size / math.tan(math.radians(fov) * 0.5)
            K = torch.tensor(
                [[focal, 0.0, size / 2.0], [0.0, focal, size / 2.0], [0.0, 0.0, 1.0]],
                dtype=torch.float32, device="cuda",
            )[None]
            viewmat_np = _look_at(camera, target)
            viewmat = torch.from_numpy(viewmat_np).to(device="cuda", dtype=torch.float32)[None]

            rendered, alpha, _ = rasterization(
                means=means,
                quats=quats,
                scales=scales,
                opacities=opacities,
                colors=colors,
                viewmats=viewmat,
                Ks=K,
                width=size,
                height=size,
                sh_degree=sh_degree,
                packed=True,
                backgrounds=torch.zeros((1, 3), device="cuda", dtype=torch.float32),
                render_mode="RGB",
                rasterize_mode="classic",
            )

            premul = rendered[0].clamp(0.0, 1.0)
            a = alpha[0].clamp(0.0, 1.0)
            # gsplat composites RGB against black. Convert premultiplied RGB to
            # straight-alpha RGBA to avoid dark fringes when composited later.
            straight = torch.where(a > 1e-5, premul / a.clamp_min(1e-5), torch.zeros_like(premul)).clamp(0.0, 1.0)
            rgba = torch.cat([straight, a], dim=-1)
            rgba8 = (rgba * 255.0 + 0.5).to(torch.uint8).cpu().numpy()

            filename = f"{ring}_{view_index:02d}.png"
            Image.fromarray(rgba8, "RGBA").save(staging / filename, optimize=True)
            created.append(filename)
            camera_manifest.append({
                "file": filename,
                "ring": ring,
                "index": view_index,
                "camera": camera.tolist(),
                "target": target.tolist(),
                "fovDegrees": fov,
                "viewMatrix": viewmat_np.tolist(),
                "size": size,
            })

    (staging / "render_manifest.json").write_text(json.dumps({
        "source": str(ply_path),
        "viewCount": len(created),
        "views": camera_manifest,
    }, indent=2), encoding="utf-8")
    guard.update(0.98, "committing rendered views")
    shutil.rmtree(output, ignore_errors=True)
    staging.rename(output)
    return created
