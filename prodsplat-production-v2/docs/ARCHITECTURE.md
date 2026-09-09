# Architecture

## Runtime topology

```text
Host browser
    │
    │ http://127.0.0.1:8080
    ▼
┌──────────────────────────────────┐
│ Go control plane                 │
│                                  │
│ HTTP API                         │
│ job state                        │
│ attempt versioning               │
│ durable task queue + leases      │
│ SSE progress stream              │
│ static dashboard                 │
│ compiled SuperSplat              │
└──────────────┬───────────────────┘
               │ private Compose network
               │ bearer-authenticated polling
               ▼
┌──────────────────────────────────┐
│ Python GPU worker                │
│                                  │
│ ns-process-data / COLMAP         │
│ Splatfacto                       │
│ ns-export gaussian-splat         │
│ gsplat renderer                  │
│ synthetic dataset generator      │
└──────────────┬───────────────────┘
               │
               ▼
        /workspace bind mount
```

## Control plane vs GPU worker

The Go process owns business state. The worker never edits `job.json` or task JSON directly; it only communicates through internal HTTP endpoints. This prevents Python subprocess crashes from corrupting application metadata.

The worker owns computational implementation details. The Go layer knows that a task is `RECONSTRUCT`, `RENDER`, or `DATASET`, but it does not import PyTorch, gsplat, or Nerfstudio.

## Durable task leasing

Task lifecycle:

```text
READY
  ↓ claim(worker)
RUNNING + lease owner + lease deadline
  ├── heartbeat → extend lease
  ├── complete  → SUCCEEDED
  ├── fail      → FAILED
  ├── cancel    → CANCELLED
  └── lease expires → reclaimable
```

The delivery model is **at least once**. For that reason, worker tasks use task-specific staging directories and only publish stable outputs after successful validation. A reclaimed reconstruction clears and rebuilds its task scratch directory.

The single Go app process is the only writer to queue/job metadata. This is why a file-backed repository is safe for this deployment topology. Running multiple app replicas against the same files is not supported.

## Attempt semantics

A new video always creates a new attempt. It never overwrites an earlier reconstruction. Attempts can be reactivated after reconstruction, and accepted artifacts such as `cleaned.ply`, renders, and datasets are associated with the active attempt.

## SuperSplat integration

The image build clones a pinned SuperSplat commit, copies `editor-integration/product-integration.ts`, and injects one registration call into the pinned `main.ts`. If the expected upstream anchor changes, the Docker build fails rather than silently shipping an unintegrated editor.

`Save & Continue` uses SuperSplat's own `writeSplatFile` path, captures the PLY in browser memory, then streams it to the same-origin Go API. The browser never attempts to write directly to `/workspace`.

## Scale-out boundary

When one workstation is no longer enough, retain the public API/domain model but replace infrastructure interfaces:

```text
File Job Repository     → PostgreSQL
File Task Queue         → PostgreSQL queue / managed queue
Local /workspace assets → S3/MinIO/object storage
One worker              → worker pool
Loopback UI             → authenticated HTTPS application
```

Kubernetes is useful only after multiple hosts/GPU workers create a real scheduling problem. It is not required to make the single-workstation deployment production-quality.
