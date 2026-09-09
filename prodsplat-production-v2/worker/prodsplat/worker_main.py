from __future__ import annotations

import logging
import os
from pathlib import Path
import socket
import time
import uuid

from .client import ControlPlane
from .preflight import run_preflight
from .runner import TaskRunner

logging.basicConfig(
    level=os.environ.get("LOG_LEVEL", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
LOG = logging.getLogger("prodsplat.worker")

HEALTH_FILE = Path("/tmp/prodsplat-worker-heartbeat")


def touch_health():
    HEALTH_FILE.touch()


def main():
    poll_seconds = float(os.environ.get("WORKER_POLL_SECONDS", "2"))
    heartbeat_seconds = float(os.environ.get("WORKER_HEARTBEAT_SECONDS", "5"))
    worker_id = os.environ.get("WORKER_ID") or f"{socket.gethostname()}-{uuid.uuid4().hex[:8]}"
    client = ControlPlane.from_env()
    runner = TaskRunner(client, worker_id, heartbeat_seconds)

    LOG.info("starting worker %s", worker_id)
    healthy, error, gpu, versions = run_preflight()
    LOG.info("preflight healthy=%s gpu=%s versions=%s", healthy, gpu, versions)
    if error:
        LOG.error("preflight: %s", error)

    last_worker_heartbeat = 0.0
    current_task_id = ""

    while True:
        touch_health()
        now = time.monotonic()
        if now - last_worker_heartbeat >= heartbeat_seconds:
            try:
                client.worker_heartbeat(
                    worker_id,
                    healthy,
                    error=error,
                    gpu=gpu,
                    versions=versions,
                    current_task_id=current_task_id,
                )
                last_worker_heartbeat = now
            except Exception as exc:
                LOG.warning("worker heartbeat failed: %s", exc)

        if not healthy:
            # Keep the container alive and visible as unhealthy to the app. This is
            # more diagnosable than a restart loop when the NVIDIA runtime is missing.
            time.sleep(min(max(poll_seconds, 2), 10))
            continue

        try:
            task = client.claim(worker_id)
        except Exception as exc:
            LOG.warning("task claim failed: %s", exc)
            time.sleep(poll_seconds)
            continue

        if task is None:
            time.sleep(poll_seconds)
            continue

        current_task_id = task["id"]
        try:
            client.worker_heartbeat(worker_id, True, gpu=gpu, versions=versions, current_task_id=current_task_id)
        except Exception:
            pass
        runner.run(task)
        current_task_id = ""
        try:
            client.worker_heartbeat(worker_id, True, gpu=gpu, versions=versions, current_task_id="")
            last_worker_heartbeat = time.monotonic()
        except Exception:
            pass


if __name__ == "__main__":
    main()
