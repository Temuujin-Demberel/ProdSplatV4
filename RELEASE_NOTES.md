# ProdSplat 2.0.6 release notes

This release changes the transparent render step so its output drops straight into apu-synth's `cutouts/render` folder, and fixes two defects that blocked the documented workflow on a fresh clone.

## Transparent render grid

- 48 views per render: three elevation rings at −30°, 0° and +30° with 16 azimuths (22.5° steps) each. No top-down or bottom views; the product is seen the way a shelf camera sees it.
- Filenames follow apu-synth's cutout grammar: `{assetName}__az{AAA}_el{±EE}.png` (for example `orgiluun_lemon_lime_Pet__az045_el+30.png`). Azimuths are rounded to whole degrees in the name; the exact value is kept in `render_manifest.json`.
- Sign conventions match apu-synth: positive azimuth shows the product's own right side, positive elevation looks down from above.
- Render options: `assetName` (defaults to the job name sanitized to letters, digits and single underscores), `upAxis` (`+z` default; `±x`, `±y`, `±z`), and `frontAzimuthDegrees` (multiple of 22.5) that tells the renderer which of the current views is the product's front. The last-used options are stored on the job and attempt and prefilled in the dashboard.
- Default render size is 1024 px; alpha is straight (un-premultiplied) as before.
- `render_manifest.json` now records `assetName`, `upAxis`, `frontAzimuthDegrees`, `elevations`, `viewsPerRing`, `size`, `fovDegrees` and, per view, `azimuthDegrees`, `azimuthLabel`, `elevationDegrees`, camera, target and view matrix.
- The dashboard preview grid shows all 48 views with their angle captions, loaded from the manifest, so orientation mistakes are visible before a re-render.
- Auto-isolate (`isolate` render option, on by default for video attempts): the worker locates the product from the reconstruction cameras, keeps a cylinder around it, cuts at the densest horizontal layer below it (the table or floor) plus a margin, trims everything outside the product's footprint, writes `attempts/NNN/isolated.ply`, and renders that. A video attempt can therefore be rendered without opening SuperSplat; the editor loads the isolated asset for touch-ups. Thresholds are fractions of the camera orbit radius and can be tuned with the `PRODSPLAT_ISOLATE_*` variables.

## Fixes

- SuperSplat only accepts load URLs whose path ends in a known extension, so the editor is now opened through `GET /api/jobs/{id}/splat.ply` and `GET /api/jobs/{id}/cleaned.ply` (the old routes remain).
- `product-scan.sh`, `stop.sh`, `scripts/*.sh`, `editor-integration/inject.sh` and `worker/bin/colmap` are committed with the executable bit, so `./product-scan.sh` and the COLMAP wrapper tests work on a fresh clone.
- The worker's PLY reader drops Gaussians whose required properties are non-finite (some third-party exporters write `inf` opacity) instead of rejecting the whole file; the dropped count is kept on the parsed asset.

## Validation performed for this package

- `go test ./...`: passed, including render-option validation, default and invalid render bodies, `.ply` routes and manifest serving.
- Python worker tests: passed, including the 48-view rig geometry, apu-synth filename grammar, orientation frames and asset-name sanitizing. `render.py` imports without torch so these run in CI.
- Browser JavaScript syntax check: passed.
- Shell-script syntax checks: passed.

A GPU render on the target workstation is still required to confirm that `az000_el+00` shows the product front for a given capture; the front-azimuth option exists for exactly that correction.

## 2.0.5 (previous)

This release keeps the one-command appliance workflow and fixes long-video camera-registration failures discovered during the first real Ubuntu GPU reconstruction.

### Reconstruction changes

- Reconstruction profiles now treat their historical frame counts (60 / 90 / 140) as **minimum baselines**, not fixed extraction counts.
- The worker inspects video duration before COLMAP and chooses a duration-aware frame target.
- `balanced` keeps the validated 90-frame behavior for a normal ~30 second capture, but scales up to 450 frames for long captures.
- If sequential COLMAP registration is still weak, ProdSplat automatically retries with a denser temporal sample (up to 700 frames for `balanced`).
- If dense sequential matching still cannot form a useful camera model, a bounded exhaustive-matching rescue is attempted (260 frames for `balanced`).
- The quality gate now considers both registration ratio **and absolute registered camera count**. Dense extraction is no longer rejected solely because the ratio is below 35% when a large usable camera set was recovered.
- `quality.json` is written even when adaptive reconstruction ultimately fails, and records the capture statistics, frame plan, every COLMAP registration attempt, and the selected/best model.
- Task logs show the adaptive primary/rescue frame targets and registration result for each pass.

### Existing fixes retained

- One-command launch: `./product-scan.sh` on Ubuntu and `.\product-scan.ps1` on Windows.
- GPU-first COLMAP SIFT extraction and matching.
- Explicit `SiftMatching.max_num_matches=32768` and GPU index.
- Targeted CPU retry only for recognized SiftGPU/CUDA matcher failures.
- Splatfacto and gsplat remain GPU-backed.
- Dashboard cache, Create Job, file-selection persistence, SuperSplat build/SBOM, and durable-task fixes from 2.0.2–2.0.4 remain included.

A full CUDA reconstruction still depends on the target capture and NVIDIA workstation. The worker retries common registration topology problems automatically, but no software can recover camera poses from a capture with insufficient visual overlap, severe blur, or a moving object/background configuration that violates static-scene SfM assumptions.
