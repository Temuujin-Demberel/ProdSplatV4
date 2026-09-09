from __future__ import annotations

import os
from pathlib import Path
import subprocess
import textwrap


WRAPPER = Path(__file__).resolve().parents[1] / "bin" / "colmap"


def _fake_colmap(tmp_path: Path) -> Path:
    fake = tmp_path / "fake-colmap"
    fake.write_text(
        textwrap.dedent(
            """\
            #!/usr/bin/env bash
            set -euo pipefail
            echo "REAL_ARGS:$*"
            if [[ " $* " == *" sequential_matcher "* && " $* " == *" --SiftMatching.use_gpu 1 "* && "${FAKE_GPU_FAIL:-0}" == "1" ]]; then
              echo 'Check failed: max_num_matches > 0 (0 vs. 0)'
              exit 9
            fi
            if [[ "${FAKE_OTHER_FAIL:-0}" == "1" ]]; then
              echo 'some unrelated failure'
              exit 7
            fi
            exit 0
            """
        ),
        encoding="utf-8",
    )
    fake.chmod(0o755)
    return fake


def _run(fake: Path, *args: str, **extra_env: str) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env.update(extra_env)
    env["PRODSPLAT_REAL_COLMAP"] = str(fake)
    return subprocess.run([str(WRAPPER), *args], env=env, text=True, capture_output=True)


def test_gpu_matcher_gets_explicit_safe_options(tmp_path: Path):
    fake = _fake_colmap(tmp_path)
    result = _run(fake, "sequential_matcher", "--database_path", "/tmp/db", "--SiftMatching.use_gpu", "1")
    assert result.returncode == 0
    assert "--SiftMatching.gpu_index 0" in result.stdout
    assert "--SiftMatching.max_num_matches 32768" in result.stdout


def test_known_gpu_matcher_failure_retries_cpu_only(tmp_path: Path):
    fake = _fake_colmap(tmp_path)
    result = _run(fake, "sequential_matcher", "--database_path", "/tmp/db", FAKE_GPU_FAIL="1")
    assert result.returncode == 0
    assert "--SiftMatching.use_gpu 1" in result.stdout
    assert "--SiftMatching.use_gpu 0" in result.stdout
    assert "retrying only this matching stage on CPU" in result.stderr


def test_unrelated_matcher_failure_is_not_hidden(tmp_path: Path):
    fake = _fake_colmap(tmp_path)
    result = _run(fake, "sequential_matcher", "--database_path", "/tmp/db", FAKE_OTHER_FAIL="1")
    assert result.returncode == 7
    assert "--SiftMatching.use_gpu 0" not in result.stdout
