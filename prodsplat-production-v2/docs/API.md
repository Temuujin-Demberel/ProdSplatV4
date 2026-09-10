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
- `POST /api/jobs/{id}/render` — optional JSON `{assetName, upAxis, frontAzimuthDegrees}`; no body reuses the job's last options or the defaults (sanitized job name, `+z`, `0`)
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
