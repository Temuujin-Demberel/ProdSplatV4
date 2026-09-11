from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING
import json
import math
import re
import shutil

import numpy as np
from PIL import Image

from .lease import LeaseGuard
from .ply import read_gaussian_ply

if TYPE_CHECKING:
    import torch

FOV_DEGREES = 50.0
FILL_FRACTION = 0.70
MIN_RADIUS = 0.25
VIEWS_PER_RING = 16
ELEVATIONS_DEGREES: tuple[float, ...] = (-30.0, 0.0, 30.0)
DEFAULT_SIZE = 1024
UP_AXES: dict[str, tuple[float, float, float]] = {
    "+x": (1.0, 0.0, 0.0),
    "-x": (-1.0, 0.0, 0.0),
    "+y": (0.0, 1.0, 0.0),
    "-y": (0.0, -1.0, 0.0),
    "+z": (0.0, 0.0, 1.0),
    "-z": (0.0, 0.0, -1.0),
}
REFERENCE_AXES: dict[str, tuple[float, float, float]] = {
    "x": (0.0, 1.0, 0.0),
    "y": (0.0, 0.0, 1.0),
    "z": (1.0, 0.0, 0.0),
}
NON_NAME_RUNS = re.compile(r"[^A-Za-z0-9]+")
MANIFEST_NAME = "render_manifest.json"


def sanitize_asset_name(raw: str) -> str:
    return NON_NAME_RUNS.sub("_", raw).strip("_")


@dataclass(frozen=True)
class RenderOptions:
    asset_name: str
    up_axis: str = "+z"
    front_azimuth_degrees: float = 0.0

    @classmethod
    def from_payload(cls, payload: dict[str, str]) -> "RenderOptions":
        asset_name = sanitize_asset_name(payload.get("assetName", ""))
        if not asset_name:
            raise ValueError("assetName is required")
        up_axis = payload.get("upAxis") or "+z"
        if up_axis not in UP_AXES:
            raise ValueError(f"unsupported upAxis: {up_axis}")
        return cls(asset_name, up_axis, float(payload.get("frontAzimuthDegrees") or "0"))


@dataclass(frozen=True)
class OrientationFrame:
    up: np.ndarray
    front: np.ndarray
    right: np.ndarray


@dataclass(frozen=True)
class CameraView:
    azimuth_degrees: float
    elevation_degrees: float
    camera: np.ndarray
    target: np.ndarray
    fov_degrees: float

    @property
    def azimuth_label(self) -> int:
        return int(math.floor(self.azimuth_degrees + 0.5)) % 360

    @property
    def elevation_label(self) -> int:
        return int(round(self.elevation_degrees))

    def filename(self, asset_name: str) -> str:
        return f"{asset_name}__az{self.azimuth_label:03d}_el{self.elevation_label:+03d}.png"


def _unit(vector: np.ndarray) -> np.ndarray:
    return vector / max(float(np.linalg.norm(vector)), 1e-8)


def orientation_frame(up_axis: str, front_azimuth_degrees: float) -> OrientationFrame:
    up = np.array(UP_AXES[up_axis], np.float32)
    reference = np.array(REFERENCE_AXES[up_axis[1]], np.float32)
    phi = math.radians(front_azimuth_degrees)
    front = _unit(math.cos(phi) * reference + math.sin(phi) * np.cross(reference, up))
    right = _unit(np.cross(front, up))
    return OrientationFrame(up=up, front=front.astype(np.float32), right=right.astype(np.float32))


def _look_at(camera: np.ndarray, target: np.ndarray, up: np.ndarray) -> np.ndarray:
    forward = _unit(target - camera)
    right = _unit(np.cross(forward, up))
    true_up = np.cross(right, forward)
    rotation = np.stack([right, -true_up, forward], axis=0).astype(np.float32)
    view = np.eye(4, dtype=np.float32)
    view[:3, :3] = rotation
    view[:3, 3] = -rotation @ camera
    return view


def _robust_bounds(means: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    lo = np.percentile(means, 1.0, axis=0)
    hi = np.percentile(means, 99.0, axis=0)
    center = (lo + hi) * 0.5
    extent = np.maximum(hi - lo, 1e-4)
    return center.astype(np.float32), extent.astype(np.float32)


def camera_rig(
    center: np.ndarray,
    extent: np.ndarray,
    frame: OrientationFrame,
    fov_degrees: float = FOV_DEGREES,
    views_per_ring: int = VIEWS_PER_RING,
    elevations: tuple[float, ...] = ELEVATIONS_DEGREES,
    fill_fraction: float = FILL_FRACTION,
) -> list[CameraView]:
    half_diag = float(np.linalg.norm(extent)) * 0.5
    radius = max(half_diag / (fill_fraction * math.tan(math.radians(fov_degrees) * 0.5)), MIN_RADIUS)
    target = center.astype(np.float32)
    views: list[CameraView] = []
    for elevation in elevations:
        el = math.radians(elevation)
        for i in range(views_per_ring):
            azimuth = 360.0 * i / views_per_ring
            az = math.radians(azimuth)
            horizontal = math.cos(az) * frame.front + math.sin(az) * frame.right
            direction = math.cos(el) * horizontal + math.sin(el) * frame.up
            camera = (target + radius * direction).astype(np.float32)
            views.append(CameraView(azimuth, elevation, camera, target, fov_degrees))
    return views


def _load_tensors(ply_path: str):
    import torch

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


def render_rgba_views(
    ply_path: str,
    output_dir: str,
    guard: LeaseGuard,
    options: RenderOptions,
    size: int = DEFAULT_SIZE,
    manifest_extra: dict | None = None,
) -> list[str]:
    import torch
    from gsplat import rasterization

    output = Path(output_dir)
    staging = output.with_name(output.name + ".partial")
    shutil.rmtree(staging, ignore_errors=True)
    staging.mkdir(parents=True, exist_ok=True)

    guard.update(0.08, "loading Gaussian asset")
    means_np, means, quats, scales, opacities, colors, sh_degree = _load_tensors(ply_path)
    guard.check()

    center, extent = _robust_bounds(means_np)
    frame = orientation_frame(options.up_axis, options.front_azimuth_degrees)
    views = camera_rig(center, extent, frame)
    total = len(views)
    created: list[str] = []
    view_manifest: list[dict] = []
    background = torch.zeros((1, 3), device="cuda", dtype=torch.float32)

    with torch.inference_mode():
        for index, view in enumerate(views):
            guard.check()
            progress = 0.1 + 0.85 * (index / max(total, 1))
            guard.update(
                progress,
                f"rendering view {index + 1}/{total} (az {view.azimuth_label:03d}, el {view.elevation_label:+03d})",
            )

            focal = 0.5 * size / math.tan(math.radians(view.fov_degrees) * 0.5)
            K = torch.tensor(
                [[focal, 0.0, size / 2.0], [0.0, focal, size / 2.0], [0.0, 0.0, 1.0]],
                dtype=torch.float32, device="cuda",
            )[None]
            viewmat_np = _look_at(view.camera, view.target, frame.up)
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
                backgrounds=background,
                render_mode="RGB",
                rasterize_mode="classic",
            )

            premul = rendered[0].clamp(0.0, 1.0)
            a = alpha[0].clamp(0.0, 1.0)
            straight = torch.where(a > 1e-5, premul / a.clamp_min(1e-5), torch.zeros_like(premul)).clamp(0.0, 1.0)
            rgba = torch.cat([straight, a], dim=-1)
            rgba8 = (rgba * 255.0 + 0.5).to(torch.uint8).cpu().numpy()

            filename = view.filename(options.asset_name)
            Image.fromarray(rgba8, "RGBA").save(staging / filename, optimize=True)
            created.append(filename)
            view_manifest.append({
                "file": filename,
                "azimuthDegrees": view.azimuth_degrees,
                "azimuthLabel": view.azimuth_label,
                "elevationDegrees": view.elevation_degrees,
                "camera": view.camera.tolist(),
                "target": view.target.tolist(),
                "fovDegrees": view.fov_degrees,
                "viewMatrix": viewmat_np.tolist(),
            })

    manifest = {
        "source": str(ply_path),
        "assetName": options.asset_name,
        "upAxis": options.up_axis,
        "frontAzimuthDegrees": options.front_azimuth_degrees,
        "elevations": list(ELEVATIONS_DEGREES),
        "viewsPerRing": VIEWS_PER_RING,
        "size": size,
        "fovDegrees": FOV_DEGREES,
        "viewCount": len(created),
        "views": view_manifest,
    }
    if manifest_extra:
        manifest.update(manifest_extra)
    (staging / MANIFEST_NAME).write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    guard.update(0.98, "committing rendered views")
    shutil.rmtree(output, ignore_errors=True)
    staging.rename(output)
    return created
