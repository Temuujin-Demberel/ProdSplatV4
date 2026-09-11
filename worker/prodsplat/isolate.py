from __future__ import annotations

from dataclasses import asdict, dataclass
from pathlib import Path
import math
import os
import re

import numpy as np

from .ply import read_gaussian_ply, write_gaussian_ply

CYLINDER_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_CYLINDER", "0.40"))
PLANE_MARGIN_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_PLANE_MARGIN", "0.03"))
BODY_START_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_BODY_START", "0.08"))
FOOTPRINT_GROWTH = float(os.environ.get("PRODSPLAT_ISOLATE_FOOTPRINT", "1.08"))
NARROWING_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_NARROWING", "0.70"))
COLOR_HUE_DEGREES = float(os.environ.get("PRODSPLAT_ISOLATE_COLOR_HUE_DEG", "25"))
COLOR_MIN_SATURATION = float(os.environ.get("PRODSPLAT_ISOLATE_COLOR_MIN_SAT", "0.25"))
FOOTPRINT_PERCENTILE = 97.0
SLICE_FRACTION = 0.02
SLICE_MIN_POINTS = 20
SLICE_RADIUS_PERCENTILE = 90.0
NARROW_KEEP_FACTOR = 1.05
COLOR_BAND_FRACTION = 0.05
COLOR_RGB_DISTANCE = 0.18
SH_C0 = 0.28209479177387814
PLANE_BINS = 80
MIN_KEPT = 100
HEX_COLOR = re.compile(r"^#([0-9a-fA-F]{6})$")


@dataclass(frozen=True)
class CameraRig:
    positions: np.ndarray
    forwards: np.ndarray

    def focus(self) -> np.ndarray:
        normal_sum = np.zeros((3, 3))
        rhs = np.zeros(3)
        for position, forward in zip(self.positions, self.forwards):
            direction = forward / max(float(np.linalg.norm(forward)), 1e-8)
            projector = np.eye(3) - np.outer(direction, direction)
            normal_sum += projector
            rhs += projector @ position
        return np.linalg.solve(normal_sum, rhs)

    def orbit_radius(self, focus: np.ndarray) -> float:
        return float(np.median(np.linalg.norm(self.positions - focus, axis=1)))


@dataclass(frozen=True)
class IsolationReport:
    focus: list[float]
    orbit_radius: float
    table_z: float | None
    input_count: int
    cylinder_count: int
    above_plane_count: int
    kept_count: int
    bottom_z: float | None = None
    body_radius: float | None = None
    narrow_radius: float | None = None
    color_removed_count: int = 0

    def as_dict(self) -> dict:
        return asdict(self)


def load_training_cameras(dataset_dir: Path) -> CameraRig:
    from nerfstudio.data.dataparsers.nerfstudio_dataparser import NerfstudioDataParserConfig

    if not (dataset_dir / "transforms.json").exists():
        raise ValueError("auto-isolate needs the reconstruction cameras (dataset/transforms.json); this attempt has none")
    parser = NerfstudioDataParserConfig(data=dataset_dir, downscale_factor=1).setup()
    outputs = parser.get_dataparser_outputs(split="train")
    camera_to_world = outputs.cameras.camera_to_worlds.numpy().astype(np.float64)
    return CameraRig(positions=camera_to_world[:, :3, 3], forwards=-camera_to_world[:, :3, 2])


def parse_hex_color(value: str) -> np.ndarray | None:
    match = HEX_COLOR.match(value.strip())
    if not match:
        return None
    digits = match.group(1)
    return np.array([int(digits[i:i + 2], 16) / 255.0 for i in (0, 2, 4)])


def dc_to_rgb(values: dict[str, np.ndarray]) -> np.ndarray | None:
    if not all(f"f_dc_{i}" in values for i in range(3)):
        return None
    dc = np.stack([values[f"f_dc_{i}"] for i in range(3)], axis=-1).astype(np.float64)
    return np.clip(SH_C0 * dc + 0.5, 0.0, 1.0)


def rgb_to_hsv(rgb: np.ndarray) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    rgb = np.atleast_2d(rgb).astype(np.float64)
    red, green, blue = rgb[:, 0], rgb[:, 1], rgb[:, 2]
    maximum = rgb.max(axis=1)
    delta = maximum - rgb.min(axis=1)
    chromatic = delta > 1e-9
    red_max = chromatic & (maximum == red)
    green_max = chromatic & (maximum == green) & ~red_max
    blue_max = chromatic & ~red_max & ~green_max
    hue = np.zeros(len(rgb))
    safe_delta = np.where(chromatic, delta, 1.0)
    hue[red_max] = ((green - blue)[red_max] / safe_delta[red_max]) % 6.0
    hue[green_max] = (blue - red)[green_max] / safe_delta[green_max] + 2.0
    hue[blue_max] = (red - green)[blue_max] / safe_delta[blue_max] + 4.0
    hue *= 60.0
    saturation = np.where(maximum > 1e-9, delta / np.maximum(maximum, 1e-9), 0.0)
    return hue, saturation, maximum


def support_color_match(
    rgb: np.ndarray,
    support_rgb: np.ndarray,
    hue_tolerance_degrees: float = COLOR_HUE_DEGREES,
    min_saturation: float = COLOR_MIN_SATURATION,
) -> np.ndarray:
    support_hue, support_saturation, _ = rgb_to_hsv(support_rgb)
    if float(support_saturation[0]) < min_saturation:
        return np.linalg.norm(rgb - support_rgb, axis=1) < COLOR_RGB_DISTANCE
    hue, saturation, value = rgb_to_hsv(rgb)
    hue_distance = np.abs((hue - float(support_hue[0]) + 180.0) % 360.0 - 180.0)
    return (hue_distance <= hue_tolerance_degrees) & (saturation >= min_saturation) & (value >= min_saturation)


def narrowing_cut(
    points: np.ndarray,
    kept: np.ndarray,
    center: np.ndarray,
    orbit: float,
    threshold: float,
) -> tuple[np.ndarray, float | None, float | None, float | None]:
    if threshold <= 0 or int(kept.sum()) < SLICE_MIN_POINTS * 3:
        return kept, None, None, None
    step = SLICE_FRACTION * orbit
    heights = points[:, 2]
    distances = np.linalg.norm(points[:, :2] - center, axis=1)
    z_top = float(np.percentile(heights[kept], 99.0))
    z_low = float(heights[kept].min())
    slices: list[tuple[float, float]] = []
    z = z_top
    while z - step > z_low:
        inside = kept & (heights <= z) & (heights > z - step)
        radius = float(np.percentile(distances[inside], SLICE_RADIUS_PERCENTILE)) if int(inside.sum()) >= SLICE_MIN_POINTS else math.nan
        slices.append((z, radius))
        z -= step
    upper = [radius for _, radius in slices[: max(3, len(slices) // 3)] if not math.isnan(radius)]
    if not upper:
        return kept, None, None, None
    body_radius = float(np.median(upper))
    limit = threshold * body_radius
    for index, (slice_top, radius) in enumerate(slices):
        if math.isnan(radius) or radius >= limit:
            continue
        following = [r for _, r in slices[index + 1:index + 2] if not math.isnan(r)]
        if following and following[0] >= limit:
            continue
        below = heights <= slice_top - step
        transition = (heights <= slice_top + step) & (heights > slice_top - step)
        core = transition & (distances <= radius * NARROW_KEEP_FACTOR)
        return kept & ~below & ~core, slice_top, body_radius, radius
    return kept, None, body_radius, None


def isolation_mask(
    means: np.ndarray,
    cameras: CameraRig,
    colors: np.ndarray | None = None,
    support_color: str = "",
    cylinder_fraction: float = CYLINDER_FRACTION,
    plane_margin_fraction: float = PLANE_MARGIN_FRACTION,
    body_start_fraction: float = BODY_START_FRACTION,
    footprint_growth: float = FOOTPRINT_GROWTH,
    narrowing_fraction: float = NARROWING_FRACTION,
) -> tuple[np.ndarray, IsolationReport]:
    focus = cameras.focus()
    orbit = cameras.orbit_radius(focus)
    points = means.astype(np.float64)
    reach = cylinder_fraction * orbit
    horizontal = np.linalg.norm(points[:, :2] - focus[:2], axis=1)
    cylinder = (horizontal < reach) & (np.abs(points[:, 2] - focus[2]) < reach)

    table_z: float | None = None
    above = cylinder
    if cylinder.any():
        edges = np.linspace(focus[2] - reach, focus[2] + reach, PLANE_BINS + 1)
        histogram, _ = np.histogram(points[cylinder, 2], bins=edges)
        below = edges[:-1] < focus[2]
        if below.any() and histogram[below].max() > 0:
            peak = int(np.argmax(np.where(below, histogram, 0)))
            table_z = float(edges[peak + 1])
            above = cylinder & (points[:, 2] > table_z + plane_margin_fraction * orbit)

    kept = above
    center = focus[:2]
    if table_z is not None:
        body = above & (points[:, 2] > table_z + body_start_fraction * orbit)
        if body.any():
            center = np.median(points[body, :2], axis=0)
            radius = float(np.percentile(np.linalg.norm(points[body, :2] - center, axis=1), FOOTPRINT_PERCENTILE))
            kept = above & (np.linalg.norm(points[:, :2] - center, axis=1) < radius * footprint_growth)
    elif kept.any():
        center = np.median(points[kept, :2], axis=0)

    kept, bottom_z, body_radius, narrow_radius = narrowing_cut(points, kept, center, orbit, narrowing_fraction)

    color_removed = 0
    support_rgb = parse_hex_color(support_color) if support_color else None
    if support_rgb is not None and colors is not None:
        ceiling = bottom_z + COLOR_BAND_FRACTION * orbit if bottom_z is not None else focus[2]
        matches = kept & (points[:, 2] <= ceiling) & support_color_match(colors, support_rgb)
        color_removed = int(matches.sum())
        kept = kept & ~matches

    report = IsolationReport(
        focus=[float(value) for value in focus],
        orbit_radius=orbit,
        table_z=table_z,
        input_count=int(len(points)),
        cylinder_count=int(cylinder.sum()),
        above_plane_count=int(above.sum()),
        kept_count=int(kept.sum()),
        bottom_z=bottom_z,
        body_radius=body_radius,
        narrow_radius=narrow_radius,
        color_removed_count=color_removed,
    )
    return kept, report


def isolate_gaussians(source: Path, dataset_dir: Path, destination: Path, support_color: str = "") -> IsolationReport:
    cameras = load_training_cameras(dataset_dir)
    asset = read_gaussian_ply(source)
    kept, report = isolation_mask(asset.means(), cameras, colors=dc_to_rgb(asset.values), support_color=support_color)
    if report.kept_count < MIN_KEPT:
        raise RuntimeError(
            f"auto-isolate kept only {report.kept_count} of {report.input_count} Gaussians; "
            "the asset is probably not in the reconstruction's coordinate frame"
        )
    write_gaussian_ply({name: values[kept] for name, values in asset.values.items()}, destination)
    return report
