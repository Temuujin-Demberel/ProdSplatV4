from __future__ import annotations

from prodsplat.quality import build_frame_plan, registration_is_usable


def test_balanced_short_capture_preserves_validated_90_frame_baseline():
    capture = {"frameCount": 900, "fps": 30.0, "durationSeconds": 30.0}
    plan = build_frame_plan(capture, "balanced", 90)
    assert plan["primaryFrames"] == 90
    assert plan["rescueFrames"] == 150


def test_balanced_long_capture_increases_sampling_and_caps_work():
    capture = {"frameCount": 10706, "fps": 30.0, "durationSeconds": 10706 / 30.0}
    plan = build_frame_plan(capture, "balanced", 90)
    assert plan["primaryFrames"] == 450
    assert plan["rescueFrames"] == 700
    assert plan["exhaustiveFrames"] == 260


def test_frame_plan_never_requests_more_than_source_frames():
    capture = {"frameCount": 70, "fps": 30.0, "durationSeconds": 70 / 30.0}
    plan = build_frame_plan(capture, "quality", 140)
    assert plan["primaryFrames"] == 70
    assert plan["rescueFrames"] == 70
    assert plan["exhaustiveFrames"] == 70


def test_registration_gate_rejects_tiny_model():
    assert not registration_is_usable({"registeredFrameCount": 2, "registrationRatio": 0.022})


def test_registration_gate_accepts_good_ratio():
    assert registration_is_usable({"registeredFrameCount": 40, "registrationRatio": 0.50})


def test_registration_gate_accepts_dense_sampling_with_many_usable_cameras():
    assert registration_is_usable({"registeredFrameCount": 100, "registrationRatio": 0.16})
