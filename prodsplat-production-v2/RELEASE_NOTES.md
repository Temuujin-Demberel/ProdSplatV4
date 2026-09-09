# ProdSplat 2.0.5 release notes

This release keeps the one-command appliance workflow and fixes long-video camera-registration failures discovered during the first real Ubuntu GPU reconstruction.

## Reconstruction changes

- Reconstruction profiles now treat their historical frame counts (60 / 90 / 140) as **minimum baselines**, not fixed extraction counts.
- The worker inspects video duration before COLMAP and chooses a duration-aware frame target.
- `balanced` keeps the validated 90-frame behavior for a normal ~30 second capture, but scales up to 450 frames for long captures.
- If sequential COLMAP registration is still weak, ProdSplat automatically retries with a denser temporal sample (up to 700 frames for `balanced`).
- If dense sequential matching still cannot form a useful camera model, a bounded exhaustive-matching rescue is attempted (260 frames for `balanced`).
- The quality gate now considers both registration ratio **and absolute registered camera count**. Dense extraction is no longer rejected solely because the ratio is below 35% when a large usable camera set was recovered.
- `quality.json` is written even when adaptive reconstruction ultimately fails, and records the capture statistics, frame plan, every COLMAP registration attempt, and the selected/best model.
- Task logs show the adaptive primary/rescue frame targets and registration result for each pass.

## Existing fixes retained

- One-command launch: `./product-scan.sh` on Ubuntu and `.\product-scan.ps1` on Windows.
- GPU-first COLMAP SIFT extraction and matching.
- Explicit `SiftMatching.max_num_matches=32768` and GPU index.
- Targeted CPU retry only for recognized SiftGPU/CUDA matcher failures.
- Splatfacto and gsplat remain GPU-backed.
- Dashboard cache, Create Job, file-selection persistence, SuperSplat build/SBOM, and durable-task fixes from 2.0.2–2.0.4 remain included.

## Validation performed for this package

- `go test ./...`: passed.
- Python worker tests: passed, including adaptive frame-planning and registration-gate tests.
- Browser JavaScript syntax check: passed.
- Shell-script syntax checks: passed.

A full CUDA reconstruction still depends on the target capture and NVIDIA workstation. The worker now retries common registration topology problems automatically, but no software can recover camera poses from a capture with insufficient visual overlap, severe blur, or a moving object/background configuration that violates static-scene SfM assumptions.
