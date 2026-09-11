# HTTP API

## Public loopback API

- `GET /health` — app process health
- `GET /ready` — app + healthy GPU worker readiness
- `GET /api/system` — worker/GPU/version status and reconstruction profiles
- `GET /api/events` — SSE job-change notifications
- `POST /api/jobs`
- `GET /api/jobs`
- `GET /api/jobs/{id}`
- `GET /api/jobs/{id}/tasks`
- `POST /api/jobs/{id}/video` — multipart `file`, optional `profile`
- `POST /api/jobs/{id}/ply` — multipart `file`
- `POST /api/jobs/{id}/attempts/{number}/activate`
- `GET /api/jobs/{id}/splat` (alias `GET /api/jobs/{id}/splat.ply` for URL loaders that require the extension)
- `POST /api/jobs/{id}/cleaned` — raw Gaussian PLY body
- `GET /api/jobs/{id}/cleaned` (alias `GET /api/jobs/{id}/cleaned.ply`)
- `GET /api/jobs/{id}/isolated.ply` — the auto-isolated asset produced by the last render with `isolate`
- `POST /api/jobs/{id}/render` — optional JSON `{assetName, upAxis, frontAzimuthDegrees, isolate, supportColor}`; no body reuses the job's last options or the defaults (sanitized job name, `+z`, `0`, `false`, `""`). With `isolate: true` a video attempt can be rendered without a cleaned PLY: the worker cuts the table, floor, surroundings and any narrower stand under the product away using the reconstruction cameras and writes `attempts/NNN/isolated.ply`. `supportColor` (`#rrggbb`, optional) additionally removes Gaussians of that colour below the product's bottom edge. The isolation report is stored under `isolation` in `render_manifest.json`
- `GET /api/jobs/{id}/renders/{name}` — `*.png` or `render_manifest.json`
- `POST /api/jobs/{id}/backgrounds` — multipart repeated `files`
- `POST /api/jobs/{id}/dataset`
- `GET /api/jobs/{id}/dataset.zip`
- `POST /api/jobs/{id}/cancel`
- `GET /api/tasks/{taskID}/log`

## Internal worker API

Every request requires:

```text
Authorization: Bearer <INTERNAL_TOKEN>
```

- `POST /internal/worker/heartbeat`
- `POST /internal/tasks/claim`
- `POST /internal/tasks/{taskID}/heartbeat`
- `POST /internal/tasks/{taskID}/complete`
- `POST /internal/tasks/{taskID}/fail`
- `POST /internal/tasks/{taskID}/cancelled`

The internal API is accessible only on the Compose network unless the deployment configuration is intentionally changed.
