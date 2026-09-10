from __future__ import annotations

import importlib.metadata
import shutil
import subprocess


def _version(package: str) -> str:
    try:
        return importlib.metadata.version(package)
    except Exception:
        return "unknown"


def run_preflight() -> tuple[bool, str, dict[str, str], dict[str, str]]:
    from . import __version__ as worker_version
    versions = {
        "prodsplatWorker": worker_version,
        "python": __import__("platform").python_version(),
        "nerfstudio": _version("nerfstudio"),
        "gsplat": _version("gsplat"),
        "torch": _version("torch"),
    }
    required = ["ns-process-data", "ns-train", "ns-export", "ffmpeg", "colmap"]
    missing = [name for name in required if shutil.which(name) is None]
    if missing:
        return False, "missing commands: " + ", ".join(missing), {}, versions

    try:
        import torch
        if not torch.cuda.is_available():
            return False, "torch.cuda.is_available() is false", {}, versions
        gpu = {
            "name": torch.cuda.get_device_name(0),
            "count": str(torch.cuda.device_count()),
            "cuda": str(torch.version.cuda),
        }
        try:
            raw = subprocess.check_output(["nvidia-smi", "--query-gpu=driver_version,memory.total", "--format=csv,noheader,nounits"], text=True, timeout=5).strip().splitlines()[0]
            driver, memory = [x.strip() for x in raw.split(",", 1)]
            gpu["driver"] = driver
            gpu["memoryMiB"] = memory
        except Exception:
            pass
        return True, "", gpu, versions
    except Exception as exc:
        return False, f"GPU preflight failed: {type(exc).__name__}: {exc}", {}, versions
