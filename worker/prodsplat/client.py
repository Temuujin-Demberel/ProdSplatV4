from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any


class ControlPlaneError(RuntimeError):
    pass


class LeaseLost(ControlPlaneError):
    pass


@dataclass
class ControlPlane:
    base_url: str
    token: str
    timeout: float = 10.0

    @classmethod
    def from_env(cls) -> "ControlPlane":
        return cls(
            base_url=os.environ.get("APP_INTERNAL_URL", "http://app:8080").rstrip("/"),
            token=os.environ["INTERNAL_TOKEN"],
        )

    def _request(self, method: str, path: str, body: dict[str, Any] | None = None):
        data = None if body is None else json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers={
                "Authorization": f"Bearer {self.token}",
                "Content-Type": "application/json",
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as response:
                if response.status == 204:
                    return None
                raw = response.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as exc:
            text = exc.read().decode("utf-8", "replace")
            if exc.code == 409:
                raise LeaseLost(text.strip() or "task lease lost") from exc
            raise ControlPlaneError(f"HTTP {exc.code} {path}: {text.strip()}") from exc
        except urllib.error.URLError as exc:
            raise ControlPlaneError(f"control plane unavailable: {exc}") from exc

    def worker_heartbeat(self, worker_id: str, healthy: bool, *, error: str = "", gpu=None, versions=None, current_task_id=""):
        return self._request("POST", "/internal/worker/heartbeat", {
            "workerId": worker_id,
            "healthy": healthy,
            "error": error,
            "gpu": gpu or {},
            "versions": versions or {},
            "currentTaskId": current_task_id,
        })

    def claim(self, worker_id: str):
        return self._request("POST", "/internal/tasks/claim", {"workerId": worker_id})

    def task_heartbeat(self, task_id: str, worker_id: str, progress: float, message: str):
        return self._request("POST", f"/internal/tasks/{task_id}/heartbeat", {
            "workerId": worker_id,
            "progress": progress,
            "message": message,
        })

    def complete(self, task_id: str, worker_id: str, result: dict[str, str], message: str):
        return self._request("POST", f"/internal/tasks/{task_id}/complete", {
            "workerId": worker_id,
            "result": result,
            "message": message,
        })

    def fail(self, task_id: str, worker_id: str, error: str, message: str):
        return self._request("POST", f"/internal/tasks/{task_id}/fail", {
            "workerId": worker_id,
            "error": error,
            "message": message,
        })

    def cancelled(self, task_id: str, worker_id: str, message: str):
        return self._request("POST", f"/internal/tasks/{task_id}/cancelled", {
            "workerId": worker_id,
            "message": message,
        })
