from pathlib import Path
import math
import sys

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from prodsplat.isolate import CameraRig, isolation_mask, parse_hex_color, support_color_match
from prodsplat.ply import read_gaussian_ply, write_gaussian_ply

BOX_BLUE = "#2050c8"


def box_shell(rng: np.random.Generator, count: int, half_x: float, half_y: float, z0: float, z1: float) -> np.ndarray:
    points = rng.uniform([-half_x, -half_y, z0], [half_x, half_y, z1], size=(count, 3))
    side = rng.integers(0, 4, count)
    points[side == 0, 0] = -half_x
    points[side == 1, 0] = half_x
    points[side == 2, 1] = -half_y
    points[side == 3, 1] = half_y
    return points


def disk(rng: np.random.Generator, count: int, radius: float, z0: float, z1: float) -> np.ndarray:
    angle = rng.uniform(0, 2 * math.pi, count)
    distance = radius * np.sqrt(rng.uniform(0, 1, count))
    return np.stack([distance * np.cos(angle), distance * np.sin(angle), rng.uniform(z0, z1, count)], axis=-1)


def pedestal_scene(seed: int = 0, box_half: float = 0.08) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    rng = np.random.default_rng(seed)
    sides = box_shell(rng, 3000, 0.15, 0.15, 0.15, 0.45)
    top = rng.uniform([-0.15, -0.15, 0.449], [0.15, 0.15, 0.45], size=(600, 3))
    underside = rng.uniform([-0.15, -0.15, 0.15], [0.15, 0.15, 0.151], size=(1500, 3))
    underside = underside[np.maximum(abs(underside[:, 0]), abs(underside[:, 1])) > box_half]
    product = np.concatenate([sides, top, underside])
    pedestal = np.concatenate([
        box_shell(rng, 1200, box_half, box_half, 0.0, 0.15),
        rng.uniform([-box_half, -box_half, 0.149], [box_half, box_half, 0.15], size=(100, 3)),
    ])
    table = disk(rng, 3000, 0.45, -0.012, 0.0)
    legs = np.concatenate([
        np.stack([x + rng.normal(0, 0.01, 300), y + rng.normal(0, 0.01, 300), rng.uniform(-0.6, -0.012, 300)], axis=-1)
        for x in (-0.4, 0.4) for y in (-0.4, 0.4)
    ])
    floor = disk(rng, 5000, 1.5, -0.62, -0.6)
    means = np.concatenate([product, pedestal, table, legs, floor])
    labels = np.concatenate([
        np.zeros(len(product), int), np.ones(len(pedestal), int), np.full(len(table), 2), np.full(len(legs), 3), np.full(len(floor), 4),
    ])
    colors = np.zeros((len(means), 3))
    colors[labels == 0] = rng.uniform([0.6, 0.6, 0.2], [1.0, 1.0, 0.6], size=(int((labels == 0).sum()), 3))
    colors[labels == 1] = rng.uniform([0.05, 0.15, 0.6], [0.25, 0.4, 1.0], size=(int((labels == 1).sum()), 3))
    colors[labels >= 2] = rng.uniform([0.3, 0.25, 0.2], [0.6, 0.5, 0.4], size=(int((labels >= 2).sum()), 3))
    return means, labels, colors


def pedestal_cameras() -> CameraRig:
    angles = np.linspace(0, 2 * math.pi, 48, endpoint=False)
    heights = np.tile([0.15, 0.35, 0.7], 16)
    positions = np.stack([np.cos(angles), np.sin(angles), heights], axis=-1)
    forwards = np.array([0.0, 0.0, 0.30]) - positions
    return CameraRig(positions=positions, forwards=forwards / np.linalg.norm(forwards, axis=1, keepdims=True))


def kept_share(kept: np.ndarray, labels: np.ndarray, label: int) -> float:
    return float(kept[labels == label].mean())


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


def test_pedestal_box_removed_by_narrowing():
    means, labels, _ = pedestal_scene()
    kept, report = isolation_mask(means, pedestal_cameras())
    assert kept_share(kept, labels, 0) > 0.9
    assert kept_share(kept, labels, 1) < 0.02
    assert all(kept_share(kept, labels, label) == 0.0 for label in (2, 3, 4))
    assert report.bottom_z is not None and 0.12 < report.bottom_z < 0.17
    assert report.narrow_radius is not None and report.body_radius is not None
    assert report.narrow_radius < 0.7 * report.body_radius
    underside = (labels == 0) & (means[:, 2] < 0.152)
    away_from_box = np.linalg.norm(means[:, :2], axis=1) > 1.2 * report.narrow_radius
    assert kept[underside & away_from_box].mean() > 0.99
    assert kept[(labels == 0) & (means[:, 2] > 0.16)].all()


def test_support_color_removes_remaining_box():
    means, labels, colors = pedestal_scene()
    kept, report = isolation_mask(means, pedestal_cameras(), colors=colors, support_color=BOX_BLUE)
    assert kept_share(kept, labels, 0) > 0.9
    assert kept_share(kept, labels, 1) == 0.0
    assert report.color_removed_count >= 0
    kept_wide, _ = isolation_mask(means, pedestal_cameras(), colors=colors, support_color=BOX_BLUE, narrowing_fraction=0.0)
    assert kept_share(kept_wide, labels, 1) < 0.01
    assert kept_share(kept_wide, labels, 0) == 1.0


def test_blue_label_on_upper_half_survives_color_rule():
    means, labels, colors = pedestal_scene()
    label_points = (labels == 0) & (means[:, 2] > 0.35)
    colors[label_points] = [0.1, 0.25, 0.9]
    kept, _ = isolation_mask(means, pedestal_cameras(), colors=colors, support_color=BOX_BLUE)
    assert kept[label_points].all()
    assert kept_share(kept, labels, 1) == 0.0


def test_narrowing_disabled_keeps_the_box():
    means, labels, _ = pedestal_scene()
    kept, report = isolation_mask(means, pedestal_cameras(), narrowing_fraction=0.0)
    assert kept_share(kept, labels, 1) > 0.5
    assert report.bottom_z is None


def test_tapered_product_is_not_cut():
    rng = np.random.default_rng(3)
    count = 4000
    z = rng.uniform(0.0, 0.30, count)
    radius = 0.15 * (0.85 + 0.15 * z / 0.30)
    angle = rng.uniform(0, 2 * math.pi, count)
    product = np.stack([radius * np.cos(angle), radius * np.sin(angle), z], axis=-1)
    table = disk(rng, 3000, 0.45, -0.012, 0.0)
    means = np.concatenate([product, table])
    labels = np.concatenate([np.zeros(count, int), np.full(len(table), 2)])
    kept, report = isolation_mask(means, orbit_cameras())
    assert report.bottom_z is None
    assert kept_share(kept, labels, 0) > 0.8
    assert kept_share(kept, labels, 2) == 0.0


def test_parse_hex_color_and_match():
    assert np.allclose(parse_hex_color("#1F4FD8"), [31 / 255, 79 / 255, 216 / 255])
    assert parse_hex_color("blue") is None
    assert parse_hex_color("#12") is None
    support = parse_hex_color(BOX_BLUE)
    rgb = np.array([[0.12, 0.3, 0.85], [0.9, 0.8, 0.2], [0.5, 0.5, 0.5], [0.2, 0.35, 0.8]])
    assert support_color_match(rgb, support).tolist() == [True, False, False, True]
    grey = support_color_match(rgb, np.array([0.5, 0.5, 0.5]))
    assert grey.tolist() == [False, False, True, False]
