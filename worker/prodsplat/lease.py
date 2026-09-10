from __future__ import annotations

import logging
import threading
import time

from .client import ControlPlane, LeaseLost

LOG = logging.getLogger(__name__)


class Cancelled(RuntimeError):
    pass


class LeaseGuard:
    """Keeps a durable task lease alive while a long GPU command runs.

    The worker aborts if the user asks for cancellation, the lease is lost, or
    the control plane is unreachable for long enough that a second worker may
    legitimately reclaim the task.
    """

    def __init__(self, client: ControlPlane, task_id: str, worker_id: str, interval: float = 5.0, network_grace: float = 30.0):
        self.client = client
        self.task_id = task_id
        self.worker_id = worker_id
        self.interval = interval
        self.network_grace = network_grace
        self._progress = 0.0
        self._message = "starting"
        self._lock = threading.Lock()
        self.stop_event = threading.Event()
        self.cancel_event = threading.Event()
        self.lease_lost_event = threading.Event()
        self._thread: threading.Thread | None = None
        self._last_success = time.monotonic()

    def start(self):
        self._thread = threading.Thread(target=self._loop, daemon=True, name=f"lease-{self.task_id}")
        self._thread.start()
        return self

    def update(self, progress: float, message: str):
        with self._lock:
            self._progress = min(max(float(progress), 0.0), 1.0)
            self._message = message

    def check(self):
        if self.cancel_event.is_set():
            raise Cancelled("cancelled by user")
        if self.lease_lost_event.is_set():
            raise LeaseLost("task lease lost")

    def close(self):
        self.stop_event.set()
        if self._thread:
            self._thread.join(timeout=self.interval + 2)

    def _loop(self):
        while not self.stop_event.wait(self.interval):
            with self._lock:
                progress = self._progress
                message = self._message
            try:
                response = self.client.task_heartbeat(self.task_id, self.worker_id, progress, message)
                self._last_success = time.monotonic()
                if response and response.get("cancelRequested"):
                    LOG.info("task %s cancellation requested", self.task_id)
                    self.cancel_event.set()
                    return
            except LeaseLost:
                LOG.error("task %s lease was lost", self.task_id)
                self.lease_lost_event.set()
                return
            except Exception as exc:
                LOG.warning("task heartbeat failed: %s", exc)
                if time.monotonic() - self._last_success > self.network_grace:
                    LOG.error("control plane unreachable beyond grace period; stopping task")
                    self.lease_lost_event.set()
                    return
