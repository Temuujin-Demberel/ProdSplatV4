from __future__ import annotations

import json
import logging
from pathlib import Path
import traceback

from .client import ControlPlane, LeaseLost
from .dataset import build_yolo_dataset
from .isolate import isolate_gaussians
from .lease import Cancelled, LeaseGuard
from .reconstruct import reconstruct
from .render import RenderOptions, render_rgba_views

LOG = logging.getLogger(__name__)


class TaskRunner:
    def __init__(self, client: ControlPlane, worker_id: str, heartbeat_interval: float):
        self.client = client
        self.worker_id = worker_id
        self.heartbeat_interval = heartbeat_interval

    def run(self, task: dict):
        task_id = task["id"]
        log_path = Path(task.get("logPath") or f"/workspace/runtime/{task_id}.log")
        log_path.parent.mkdir(parents=True, exist_ok=True)
        with log_path.open("a", encoding="utf-8") as log:
            log.write(f"\n=== task {task_id} type={task['type']} delivery={task.get('attemptCount')} ===\n")

        guard = LeaseGuard(self.client, task_id, self.worker_id, interval=self.heartbeat_interval).start()
        try:
            guard.update(0.01, "worker accepted task")
            # Immediate heartbeat catches cancellation/lease issues before expensive work.
            response = self.client.task_heartbeat(task_id, self.worker_id, 0.01, "worker accepted task")
            if response and response.get("cancelRequested"):
                raise Cancelled("cancelled before execution")

            task_type = task["type"]
            if task_type == "RECONSTRUCT":
                result = reconstruct(task, guard)
                message = "reconstruction ready for review"
            elif task_type == "RENDER":
                payload = task["payload"]
                options = RenderOptions.from_payload(payload)
                source = payload["splatPath"]
                result = {"renderDir": payload["renderDir"], "assetName": options.asset_name}
                manifest_extra: dict = {}
                if payload.get("isolate") == "1":
                    attempt_dir = Path(payload["attemptDir"])
                    isolated_path = attempt_dir / "isolated.ply"
                    guard.update(0.03, "auto-isolating the product from its surroundings")
                    report = isolate_gaussians(
                        Path(source), attempt_dir / "dataset", isolated_path, payload.get("supportColor", "")
                    )
                    with log_path.open("a", encoding="utf-8") as log:
                        log.write(f"[ProdSplat] auto-isolate: {json.dumps(report.as_dict())}\n")
                    source = str(isolated_path)
                    result["isolatedPath"] = source
                    result["isolatedCount"] = str(report.kept_count)
                    manifest_extra["isolation"] = report.as_dict()
                files = render_rgba_views(source, payload["renderDir"], guard, options, manifest_extra=manifest_extra)
                result["viewCount"] = str(len(files))
                message = f"{len(files)} transparent views rendered as {options.asset_name}__az*_el*.png"
            elif task_type == "DATASET":
                payload = task["payload"]
                zip_path, count = build_yolo_dataset(
                    payload["renderDir"], payload["backgroundDir"], payload["datasetDir"], guard
                )
                result = {"datasetZip": zip_path, "sampleCount": str(count)}
                message = f"synthetic dataset ready ({count} samples)"
            else:
                raise ValueError(f"unknown task type: {task_type}")

            guard.check()
            self.client.complete(task_id, self.worker_id, result, message)
            LOG.info("completed task %s", task_id)
        except Cancelled as exc:
            LOG.info("task %s cancelled: %s", task_id, exc)
            try:
                self.client.cancelled(task_id, self.worker_id, "cancelled by user")
            except Exception:
                LOG.exception("failed to acknowledge cancellation")
        except LeaseLost as exc:
            # Never report completion/failure after losing the lease: another worker
            # is allowed to own the durable task now.
            LOG.error("task %s stopped after lease loss: %s", task_id, exc)
        except Exception as exc:
            LOG.error("task %s failed: %s", task_id, exc)
            with log_path.open("a", encoding="utf-8") as log:
                traceback.print_exc(file=log)
            try:
                self.client.fail(
                    task_id,
                    self.worker_id,
                    f"{type(exc).__name__}: {exc}",
                    "processing failed; inspect the task log",
                )
            except LeaseLost:
                LOG.warning("task failed after its lease was already lost")
            except Exception:
                LOG.exception("failed to report task failure")
        finally:
            guard.close()
