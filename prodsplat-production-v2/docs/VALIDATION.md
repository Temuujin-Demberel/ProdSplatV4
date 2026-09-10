# Validation protocol

## Static checks included in this repository

- Go formatting/build/unit tests
- Go HTTP integration test for job → PLY → cleaned PLY → durable render task → worker completion
- durable task lease/reclaim tests
- atomic local-storage/path traversal tests
- Gaussian PLY validation tests
- Python bytecode compilation
- Python PLY/SH parser tests
- synthetic YOLO dataset smoke test
- dashboard JavaScript syntax check
- shell syntax checks
- SuperSplat integration TypeScript syntax/type smoke test through stubs

## Required NVIDIA host validation

1. Start ProdSplat and confirm `/ready` returns 200.
2. Confirm the UI reports the expected GPU, CUDA, Nerfstudio, gsplat, and PyTorch versions.
3. Upload a good product video with `balanced`.
4. Confirm COLMAP/Nerfstudio logs are persisted.
5. Confirm the attempt reaches `REVIEW_READY` and `splat.ply` exists.
6. Open the splat in SuperSplat and perform selection/delete/transform operations.
7. Save and confirm attempt-local `cleaned.ply` exists.
8. Render and verify exactly 48 PNGs named `{assetName}__az{AAA}_el{±EE}.png` plus `render_manifest.json`. Confirm `az000_el+00` shows the product front and `az090_el+00` its right side; if not, set the front azimuth to the label of the frontal view and re-render.
9. Inspect PNG alpha in an image editor and composite against both light and dark backgrounds to detect dark-edge errors.
10. Upload shelf backgrounds and create the dataset.
11. Validate YOLO labels against generated images.
12. Kill the worker container during a reconstruction, wait for lease expiration, restart the worker, and confirm the task is reclaimed.
13. Cancel a running reconstruction and verify the subprocess terminates and the job becomes `CANCELLED`.
14. Create a second attempt, then reactivate attempt 1 and confirm the correct splat/cleaned/render pointers return.
15. Restart the app container and verify all job/task state remains available.

## Research measurements

For the academic paper, record real values rather than estimates:

- registered-frame ratio;
- end-to-end reconstruction time;
- peak GPU memory;
- Gaussian count;
- first-attempt acceptance rate;
- attempts per accepted product;
- manual edit time;
- alpha silhouette IoU against hand masks;
- downstream detector performance on a separate real test set.
