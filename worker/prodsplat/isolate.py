from __future__ import annotations

from dataclasses import asdict, dataclass
from pathlib import Path
import os

import numpy as np

from .ply import read_gaussian_ply, write_gaussian_ply

CYLINDER_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_CYLINDER", "0.40"))
PLANE_MARGIN_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_PLANE_MARGIN", "0.03"))
BODY_START_FRACTION = float(os.environ.get("PRODSPLAT_ISOLATE_BODY_START", "0.08"))
FOOTPRINT_GROWTH = float(os.environ.get("PRODSPLAT_ISOLATE_FOOTPRINT", "1.08"))
FOOTPRINT_PERCENTILE = 97.0
PLANE_BINS = 80
MIN_KEPT = 100


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


def isolation_mask(
    means: np.ndarray,
    cameras: CameraRig,
    cylinder_fraction: float = CYLINDER_FRACTION,
    plane_margin_fraction: float = PLANE_MARGIN_FRACTION,
    body_start_fraction: float = BODY_START_FRACTION,
    footprint_growth: float = FOOTPRINT_GROWTH,
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
    if table_z is not None:
        body = above & (points[:, 2] > table_z + body_start_fraction * orbit)
        if body.any():
            center = np.median(points[body, :2], axis=0)
            radius = float(np.percentile(np.linalg.norm(points[body, :2] - center, axis=1), FOOTPRINT_PERCENTILE))
            kept = above & (np.linalg.norm(points[:, :2] - center, axis=1) < radius * footprint_growth)

    report = IsolationReport(
        focus=[float(value) for value in focus],
        orbit_radius=orbit,
        table_z=table_z,
        input_count=int(len(points)),
        cylinder_count=int(cylinder.sum()),
        above_plane_count=int(above.sum()),
        kept_count=int(kept.sum()),
    )
    return kept, report


def isolate_gaussians(source: Path, dataset_dir: Path, destination: Path) -> IsolationReport:
    cameras = load_training_cameras(dataset_dir)
    asset = read_gaussian_ply(source)
    kept, report = isolation_mask(asset.means(), cameras)
    if report.kept_count < MIN_KEPT:
        raise RuntimeError(
            f"auto-isolate kept only {report.kept_count} of {report.input_count} Gaussians; "
            "the asset is probably not in the reconstruction's coordinate frame"
        )
    write_gaussian_ply({name: values[kept] for name, values in asset.values.items()}, destination)
    return report
