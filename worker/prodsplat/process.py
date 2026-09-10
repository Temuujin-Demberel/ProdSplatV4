from __future__ import annotations

import logging
import os
from pathlib import Path
import signal
import subprocess
import time

from .lease import LeaseGuard

LOG = logging.getLogger(__name__)


def terminate_process_tree(process: subprocess.Popen, grace: float = 10.0):
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    deadline = time.monotonic() + grace
    while time.monotonic() < deadline:
        if process.poll() is not None:
            return
        time.sleep(0.2)
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def run_command(cmd: list[str], log_path: str | Path, guard: LeaseGuard, progress: float, message: str, cwd: str | None = None):
    guard.update(progress, message)
    guard.check()
    log_path = Path(log_path)
    log_path.parent.mkdir(parents=True, exist_ok=True)
    LOG.info("running: %s", " ".join(cmd))

    with log_path.open("a", encoding="utf-8") as log:
        log.write("\n$ " + " ".join(cmd) + "\n")
        log.flush()
        process = subprocess.Popen(
            cmd,
            cwd=cwd,
            stdout=log,
            stderr=subprocess.STDOUT,
            text=True,
            start_new_session=True,
        )
        try:
            while True:
                code = process.poll()
                if code is not None:
                    if code != 0:
                        raise subprocess.CalledProcessError(code, cmd)
                    return
                guard.check()
                time.sleep(0.5)
        except BaseException:
            terminate_process_tree(process)
            raise
