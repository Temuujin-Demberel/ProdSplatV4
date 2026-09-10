# Operations

## Startup

Windows:

```powershell
.\product-scan.ps1
```

Linux:

```bash
./product-scan.sh
```

## Service status

```powershell
docker compose ps
```

Logs:

```powershell
docker compose logs -f app
docker compose logs -f worker
```

Individual reconstruction/render/dataset logs are exposed from the dashboard and persisted in `workspace/jobs/<job>/logs/`.

## Restart behavior

```powershell
docker compose restart app
docker compose restart worker
```

Job/task metadata remains on the bind-mounted workspace. If the worker dies during a task, its lease eventually expires and the same durable task can be delivered again.

## Cancellation

Use **Cancel active task** in the dashboard. The next worker lease heartbeat returns `cancelRequested=true`; the worker terminates the long-running Linux process group with SIGTERM and then SIGKILL if required, and acknowledges cancellation.

## Backup

For the single-node edition, back up the complete `workspace/` directory while no task is mutating it. For a consistent live backup, stop the services first:

```powershell
docker compose stop
```

copy `workspace/`, then:

```powershell
docker compose start
```

## Updating SuperSplat

Do not change `SUPER_SPLAT_COMMIT` casually. First build the editor integration against the new commit, run upstream lint/build, test PLY loading/edit/export, and inspect the updated license/dependency tree.

## Updating Nerfstudio

The worker image is pinned to `1.1.5`. Before changing it, revalidate:

- `ns-process-data video` flags;
- Splatfacto config flags;
- `ns-export gaussian-splat` flags;
- exported PLY property semantics;
- gsplat rasterization API;
- GPU memory usage on the supported cards.
