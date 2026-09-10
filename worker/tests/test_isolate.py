from pathlib import Path
import math
import sys

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from prodsplat.isolate import CameraRig, isolation_mask
from prodsplat.ply import read_gaussian_ply, write_gaussian_ply


def orbit_cameras(count: int = 36, radius: float = 1.0, height: float = 0.4) -> CameraRig:
    angles = np.linspace(0.0, 2.0 * math.pi, count, endpoint=False)
    positions = np.stack([radius * np.cos(angles), radius * np.sin(angles), np.full(count, height)], axis=-1)
    forwards = -positions / np.linalg.norm(positions, axis=1, keepdims=True)
    return CameraRig(positions=positions, forwards=forwards)


def synthetic_scene(seed: int = 0) -> tuple[np.ndarray, np.ndarray]:
    rng = np.random.default_rng(seed)
    product = rng.uniform([-0.15, -0.15, 0.0], [0.15, 0.15, 0.30], size=(2000, 3))
    table_angle = rng.uniform(0, 2 * math.pi, 3000)
    table_radius = 0.45 * np.sqrt(rng.uniform(0, 1, 3000))
    table = np.stack([table_radius * np.cos(table_angle), table_radius * np.sin(table_angle), rng.uniform(-0.012, 0.0, 3000)], axis=-1)
    legs = np.concatenate([
        np.stack([x + rng.normal(0, 0.01, 300), y + rng.normal(0, 0.01, 300), rng.uniform(-0.6, -0.012, 300)], axis=-1)
        for x in (-0.4, 0.4) for y in (-0.4, 0.4)
    ])
    floor_angle = rng.uniform(0, 2 * math.pi, 5000)
    floor_radius = 1.5 * np.sqrt(rng.uniform(0, 1, 5000))
    floor = np.stack([floor_radius * np.cos(floor_angle), floor_radius * np.sin(floor_angle), rng.uniform(-0.62, -0.6, 5000)], axis=-1)
    wall = np.stack([np.full(2000, 1.6), rng.uniform(-1.5, 1.5, 2000), rng.uniform(-0.6, 1.0, 2000)], axis=-1)
    means = np.concatenate([product, table, legs, floor, wall])
    labels = np.concatenate([
        np.zeros(len(product), int), np.ones(len(table), int), np.full(len(legs), 2), np.full(len(floor), 3), np.full(len(wall), 4),
    ])
    return means, labels


def test_focus_and_orbit_from_cameras():
    cameras = orbit_cameras()
    focus = cameras.focus()
    assert np.allclose(focus, [0.0, 0.0, 0.0], atol=1e-6)
    assert math.isclose(cameras.orbit_radius(focus), math.sqrt(1.0 + 0.16), rel_tol=1e-6)


def test_isolation_keeps_only_the_product():
    means, labels = synthetic_scene()
    kept, report = isolation_mask(means, orbit_cameras())
    assert report.table_z is not None and -0.02 < report.table_z < 0.03
    assert set(labels[kept]) == {0}
    product_fraction = kept[labels == 0].mean()
    assert product_fraction > 0.8
    assert report.kept_count == int(kept.sum())
    assert report.input_count == len(means)


def test_isolation_without_support_keeps_product_body():
    means, labels = synthetic_scene()
    only_product_and_wall = np.isin(labels, [0, 4])
    kept, report = isolation_mask(means[only_product_and_wall], orbit_cameras())
    assert set(labels[only_product_and_wall][kept]) == {0}
    assert report.kept_count > 1500


def test_write_gaussian_ply_round_trip(tmp_path: Path):
    names = ["x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"]
    rng = np.random.default_rng(1)
    values = {name: rng.normal(size=5).astype(np.float32) for name in names}
    values["rot_0"] = np.ones(5, np.float32)
    path = tmp_path / "out.ply"
    write_gaussian_ply(values, path)
    ply = read_gaussian_ply(path)
    assert ply.count == 5
    assert list(ply.values) == names
    assert np.allclose(ply.require("f_dc_1"), values["f_dc_1"])
    assert not path.with_name("out.ply.partial").exists()
