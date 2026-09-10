from pathlib import Path
import math
import re
import sys

import numpy as np
import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from prodsplat.render import (
    ELEVATIONS_DEGREES,
    FILL_FRACTION,
    FOV_DEGREES,
    UP_AXES,
    VIEWS_PER_RING,
    CameraView,
    OrientationFrame,
    RenderOptions,
    _look_at,
    camera_rig,
    orientation_frame,
    sanitize_asset_name,
)

RENDER_STEM = re.compile(r"^(?P<name>.+?)__az(?P<az>\d{3})_el(?P<el>[+-]\d{2})$")
CENTER = np.zeros(3, np.float32)
EXTENT = np.array([1.0, 1.0, 2.0], np.float32)
ASSET = "orgiluun_lemon_lime_Pet"
EXPECTED_AZIMUTH_LABELS = {0, 23, 45, 68, 90, 113, 135, 158, 180, 203, 225, 248, 270, 293, 315, 338}


def rig(up_axis: str = "+z", front_azimuth: float = 0.0) -> tuple[list[CameraView], OrientationFrame]:
    frame = orientation_frame(up_axis, front_azimuth)
    return camera_rig(CENTER, EXTENT, frame), frame


def view(views: list[CameraView], azimuth: float, elevation: float) -> CameraView:
    return next(v for v in views if v.azimuth_degrees == azimuth and v.elevation_degrees == elevation)


def expected_radius() -> float:
    half_diag = float(np.linalg.norm(EXTENT)) * 0.5
    return half_diag / (FILL_FRACTION * math.tan(math.radians(FOV_DEGREES) * 0.5))


def test_rig_has_48_unique_apu_synth_filenames():
    views, _ = rig()
    names = [v.filename(ASSET) for v in views]
    assert len(views) == VIEWS_PER_RING * len(ELEVATIONS_DEGREES) == 48
    assert len(set(names)) == 48
    stems = [RENDER_STEM.match(name[:-4]) for name in names]
    assert all(stems)
    assert {match.group("name") for match in stems} == {ASSET}
    assert {int(match.group("az")) for match in stems} == EXPECTED_AZIMUTH_LABELS
    assert {match.group("el") for match in stems} == {"-30", "+00", "+30"}
    distances = [float(np.linalg.norm(v.camera - CENTER)) for v in views]
    assert np.allclose(distances, expected_radius(), atol=1e-5)


def test_front_view_lies_along_front_axis():
    views, frame = rig()
    direction = view(views, 0.0, 0.0).camera / expected_radius()
    assert np.allclose(direction, frame.front, atol=1e-6)
    assert np.allclose(frame.front, [1.0, 0.0, 0.0], atol=1e-6)


def test_positive_azimuth_is_product_right_side():
    views, frame = rig()
    direction = view(views, 90.0, 0.0).camera / expected_radius()
    assert np.allclose(direction, np.cross(frame.front, frame.up), atol=1e-6)
    assert np.allclose(direction, [0.0, -1.0, 0.0], atol=1e-6)


def test_positive_elevation_is_above_center():
    views, frame = rig()
    above = float(np.dot(view(views, 0.0, 30.0).camera - CENTER, frame.up))
    below = float(np.dot(view(views, 0.0, -30.0).camera - CENTER, frame.up))
    assert math.isclose(above, 0.5 * expected_radius(), rel_tol=1e-5)
    assert math.isclose(below, -0.5 * expected_radius(), rel_tol=1e-5)


def test_view_matrix_maps_target_to_forward_axis():
    views, frame = rig()
    for v in views:
        matrix = _look_at(v.camera, v.target, frame.up)
        in_camera = matrix @ np.append(v.target, 1.0)
        assert np.allclose(in_camera, [0.0, 0.0, expected_radius(), 1.0], atol=1e-4)
        assert float(matrix[1, :3] @ frame.up) < 0.0


def test_front_azimuth_rotates_rig():
    rotated, rotated_frame = rig("+z", 90.0)
    base, _ = rig("+z", 0.0)
    for elevation in ELEVATIONS_DEGREES:
        assert np.allclose(view(rotated, 0.0, elevation).camera, view(base, 90.0, elevation).camera, atol=1e-5)
    assert np.allclose(rotated_frame.front, [0.0, -1.0, 0.0], atol=1e-6)


@pytest.mark.parametrize("up_axis", sorted(UP_AXES))
def test_orientation_frames_are_orthonormal(up_axis: str):
    frame = orientation_frame(up_axis, 0.0)
    for vector in (frame.up, frame.front, frame.right):
        assert math.isclose(float(np.linalg.norm(vector)), 1.0, abs_tol=1e-6)
    assert abs(float(np.dot(frame.front, frame.up))) < 1e-6
    assert abs(float(np.dot(frame.right, frame.up))) < 1e-6
    assert abs(float(np.dot(frame.front, frame.right))) < 1e-6
    assert np.allclose(np.cross(frame.front, frame.up), frame.right, atol=1e-6)


def test_y_up_reference_frame():
    frame = orientation_frame("+y", 0.0)
    assert np.allclose(frame.front, [0.0, 0.0, 1.0], atol=1e-6)
    assert np.allclose(frame.right, [-1.0, 0.0, 0.0], atol=1e-6)


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("Orgiluun Tropical Can", "Orgiluun_Tropical_Can"),
        ("orgiluun_lemon_lime_Pet", "orgiluun_lemon_lime_Pet"),
        ("  sengur--can  ", "sengur_can"),
        ("a__b", "a_b"),
        ("_x_", "x"),
        ("Sengur Can (0.5L)", "Sengur_Can_0_5L"),
        ("---", ""),
        ("Сэнгүр can", "can"),
    ],
)
def test_sanitize_asset_name(raw: str, expected: str):
    assert sanitize_asset_name(raw) == expected


def test_render_options_from_payload():
    assert RenderOptions.from_payload({"assetName": "Bottle"}) == RenderOptions("Bottle", "+z", 0.0)
    options = RenderOptions.from_payload({"assetName": "Sengur Can", "upAxis": "-y", "frontAzimuthDegrees": "22.5"})
    assert options == RenderOptions("Sengur_Can", "-y", 22.5)
    with pytest.raises(ValueError):
        RenderOptions.from_payload({"assetName": "---"})
    with pytest.raises(ValueError):
        RenderOptions.from_payload({"assetName": "x", "upAxis": "+w"})
