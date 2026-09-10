# ProdSplat Production Pipeline

## 2.0.6 apu-synth render grid release

The transparent render step now produces 48 views: three elevation rings (−30°, 0°, +30°) of 16 azimuths each, so a product is seen the way a shelf camera sees it and never from directly above or below. Files are named `{assetName}__az{AAA}_el{±EE}.png`, the layout apu-synth's cutout loader reads, so a job's `renders/` folder can be copied straight into `cutouts/render/`. Positive azimuth shows the product's right side and positive elevation looks down from above. The dashboard's render step takes the asset (class) name, the up axis, and a front-azimuth offset, and its preview grid shows all 48 views with their angles so orientation can be corrected and re-rendered. For video attempts an auto-isolate option (on by default) removes the table, floor and surroundings using the reconstruction cameras, so a reconstruction can be rendered straight away and SuperSplat becomes a touch-up tool rather than a required step. Two fixes: the editor loads the splat through `.ply`-suffixed routes (SuperSplat refuses URLs without a recognised extension), and the launch scripts and COLMAP wrapper are committed with their executable bit.

## 2.0.5 adaptive reconstruction release

The normal Ubuntu launch remains exactly `./product-scan.sh` (Windows: `.\product-scan.ps1`). Reconstruction is now duration-aware: profile frame counts are minimum baselines, long videos automatically receive denser frame sampling, weak sequential COLMAP models are retried with a denser sample, and a bounded exhaustive-matching rescue is available as a final automatic recovery path. The quality gate considers both registration ratio and absolute registered-camera count so dense sampling is not rejected by a ratio-only rule.

## 2.0.4 one-command GPU-first release

This build added the GPU-first COLMAP wrapper with explicit SIFT matching limits and a narrow CPU retry only for known SiftGPU/CUDA matcher failures. The browser dashboard also preserves selected video/PLY/background files across SSE and 10-second polling refreshes.

## 2.0.3 dashboard create-job fix

The create-job handler now captures stable DOM references before the asynchronous API call, validates the job name, and the Go server sends `Cache-Control: no-store` for `/app/` assets. This prevents stale dashboard HTML/JavaScript mixes and the Firefox `$(...) is null` error seen after clicking **Create job**.

## 2.0.2 build fix

This package includes two fixes for the SuperSplat build stage:

- CycloneDX generation is produced directly from the pinned `package-lock.json` by `scripts/lockfile_sbom.py`, so upstream ESLint peer-range warnings do not fail the Docker build.
- The editor upload wraps serialized PLY bytes in a `Blob`, eliminating the TypeScript `BodyInit` warning.

See `docs/TROUBLESHOOTING.md`.


**Author:** Demberel Temuujin — National University of Mongolia  
**Purpose:** a production-oriented, single-workstation 3D Gaussian Splatting pipeline for product digitization, interactive cleanup, transparent rendering, and synthetic detector-data generation.

ProdSplat is deliberately more than a reconstruction script. It treats each product as a durable **job**, each re-capture as a versioned **attempt**, and each GPU operation as a leased, recoverable **task**.

## What this build does

### Video path

```text
Create Job
   ↓
Upload Video (fast / balanced / quality)
   ↓
Durable RECONSTRUCT task
   ↓
ns-process-data video → frame extraction + COLMAP
   ↓
Nerfstudio Splatfacto
   ↓
ns-export gaussian-splat
   ↓
REVIEW_READY
   ↓
Pinned SuperSplat editor
   ↓
Select / crop / delete / transform
   ↓
Save & Continue
   ↓
cleaned.ply for that attempt
   ↓
Durable RENDER task
   ↓
gsplat direct-alpha renderer
   ↓
48 straight-alpha RGBA PNGs (3 elevations × 16 azimuths, {assetName}__az{AAA}_el{±EE}.png)
   ↓
Upload shelf/background imagery
   ↓
Durable DATASET task
   ↓
Synthetic composites + YOLO labels + manifest
   ↓
dataset.zip
```

### Existing Gaussian-splat path

```text
Create Job → Upload 3DGS PLY → Review/Edit → Render → Dataset
```

A generic mesh/point-cloud PLY is not accepted as a Gaussian-splat asset. The API validates the required Gaussian fields before accepting it.

## Why this version is production-oriented

The earlier reference build accepted fire-and-forget HTTP jobs. This version uses a **durable file-backed task queue with leases**:

- tasks survive app and worker restarts;
- a worker must heartbeat while owning a task;
- an expired lease can be reclaimed;
- task outputs are written through staging paths before becoming stable outputs;
- worker death does not intentionally destroy job state;
- user cancellation reaches a running GPU subprocess and terminates its process group;
- task logs remain under the persistent workspace;
- previous capture attempts remain available and can be re-activated;
- browser updates use Server-Sent Events with polling as a fallback;
- internal worker endpoints require a random bearer token;
- public application traffic is bound to `127.0.0.1` by default.

This is a **single-node production appliance architecture**. It intentionally avoids PostgreSQL, Redis, Kubernetes, and object storage until multiple app replicas or multiple hosts are actually required. See `docs/ARCHITECTURE.md` for the scale-out boundary.

## Pinned upstream components

- Nerfstudio Docker image: `ghcr.io/nerfstudio-project/nerfstudio:1.1.5`
- gsplat: version bundled by Nerfstudio 1.1.5
- SuperSplat: commit `e060989b202548848eb440a5005cd41a8b26f7db`

The Docker build fails rather than silently integrating against an incompatible SuperSplat source layout.

## Requirements

### Windows

- Windows 11 or supported Windows 10
- Docker Desktop
- Docker Desktop **WSL2 backend**
- NVIDIA Windows driver with WSL2 GPU support
- NVIDIA GPU usable from Docker

You do **not** run `chmod` in PowerShell.

### Ubuntu/Linux

- Docker Engine
- Docker Compose plugin
- NVIDIA driver
- NVIDIA Container Toolkit
- CUDA-capable NVIDIA GPU

The host does not need Go, Node.js, Python, npm, pip, Nerfstudio, COLMAP, or a Python virtual environment.

## Run on Windows PowerShell

Extract the ZIP, open PowerShell in the project folder, then:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\product-scan.ps1
```

The script creates `.env` on first run and generates a cryptographically random `INTERNAL_TOKEN` automatically.

After readiness succeeds, the browser opens:

```text
http://127.0.0.1:8080/app/
```

Stop the containers later with:

```powershell
.\stop.ps1
```

## Run on Ubuntu/Linux

```bash
./product-scan.sh
```

Stop:

```bash
./stop.sh
```

## First run

The first Docker build requires internet access because it must pull the pinned Nerfstudio image and clone/build the pinned SuperSplat source. Later runs reuse Docker layers unless the build inputs change.

## Reconstruction profiles

| Profile | Base/min frames | Iterations | Data downscale | Model downscales | Intended use |
|---|---:|---:|---:|---:|---|
| `fast` | 60 | 4,000 | 2 | 2 | quickly validate capture quality |
| `balanced` | 90 | 8,000 | 2 | 1 | default production preset |
| `quality` | 140 | 15,000 | 1 | 1 | stronger GPU / higher-quality attempt |

`balanced` preserves the validated 90-frame/8,000-iteration baseline for normal short captures. In 2.0.5 the frame count is adaptive: long videos are sampled more densely before COLMAP, and weak camera registration can trigger automatic denser sequential and bounded exhaustive rescue passes. Hardware-dependent out-of-memory behavior is still possible; no preset can guarantee that arbitrary capture resolution fits every GPU.

## Workspace

```text
workspace/
├── jobs/
│   └── job_.../
│       ├── job.json
│       ├── backgrounds/
│       ├── logs/
│       └── attempts/
│           ├── 001/
│           │   ├── input.mp4
│           │   ├── dataset/
│           │   ├── training/
│           │   ├── splat.ply
│           │   ├── cleaned.ply
│           │   ├── renders/
│           │   │   ├── orgiluun_lemon_lime_Pet__az000_el-30.png
│           │   │   ├── orgiluun_lemon_lime_Pet__az000_el+00.png
│           │   │   ├── orgiluun_lemon_lime_Pet__az023_el+30.png
│           │   │   ├── … (48 views)
│           │   │   └── render_manifest.json
│           │   └── dataset/
│           └── 002/
├── tasks/
│   └── task_....json
└── runtime/
```

The bind-mounted `workspace` is the durable source of truth for the single-node deployment. Containers are disposable.

## Validation

Development/static validation:

```bash
./scripts/validate.sh
```

Windows PowerShell:

```powershell
.\scripts\validate.ps1
```

GPU/Docker diagnostic:

```powershell
.\scripts\doctor.ps1
```

or:

```bash
./scripts/doctor.sh
```

The final meaningful validation is a real end-to-end run on the target NVIDIA machine: video → COLMAP → Splatfacto → PLY → SuperSplat → cleaned PLY → gsplat RGBA → synthetic dataset.

## Important deployment boundary

Do **not** expose this build directly to the public internet. The browser API is intentionally unauthenticated because the application is bound to loopback and designed as a local workstation appliance. A remote/multi-user edition needs user authentication, authorization, TLS/reverse proxying, database-backed metadata, object storage, quotas, and tenant-aware paths.

## Commercial note

The application's original code is marked proprietary in `LICENSE`. Third-party software retains its own license. See `THIRD_PARTY.md` and `licenses/`. Always audit the exact dependency/container versions before a commercial release.

## SuperSplat SBOM build note

ProdSplat 2.0.2 no longer invokes `npm sbom` during the editor image build.
SuperSplat 2.32.5 builds successfully with ESLint 10, but two upstream lint-only
plugins still advertise peer ranges ending at ESLint 9. npm treats that as an
invalid installed tree when running `npm sbom`, even though `npm ci` and the
Rollup production build succeed. ProdSplat now generates the CycloneDX inventory
deterministically from the pinned `package-lock.json` instead, so compliance
metadata cannot break the application build.
