# Troubleshooting

## SuperSplat build fails at `npm sbom` with `ESBOMPROBLEMS`

SuperSplat 2.32.5 currently has ESLint 10 in the root development tree while two transitive lint plugins declare peer ranges ending at ESLint 9. `npm ci` and the Rollup build can still complete, but `npm sbom` without lockfile-only mode asks npm to validate the installed dependency tree and exits with `ESBOMPROBLEMS`.

ProdSplat 2.0.2+ generates the CycloneDX inventory directly from the pinned `package-lock.json` with `scripts/lockfile_sbom.py`. This keeps SBOM generation deterministic and avoids asking npm to validate the incompatible development-only peer tree during the image build.

## TypeScript warning: `Uint8Array<ArrayBufferLike>` is not `BodyInit`

ProdSplat 2.0.2 wraps the serialized PLY bytes in a `Blob` before calling `fetch`. This satisfies the DOM `BodyInit` type and sends the same binary bytes.

## `npm audit` reports high severity vulnerabilities

The pinned SuperSplat package currently declares its npm packages as development/build dependencies. The final ProdSplat app image copies only the compiled `dist/` output from the Node build stage; `node_modules` is not copied into the runtime image. Treat the audit output as a supply-chain/build-environment finding that should still be reviewed, but it is not equivalent to saying those npm packages are present in the Go runtime container.

## Firefox: `can't access property "value", $(...) is null` when creating a job

This was fixed in ProdSplat 2.0.3. The create-job handler keeps references to the input/button before awaiting the POST request, and `/app/` is served with `Cache-Control: no-store` so an older HTML page cannot be paired with a newer `app.js`.


## Selected video briefly appears and then returns to “No file selected”

Fixed in ProdSplat 2.0.4. The dashboard used to rebuild every job card on SSE events and the 10-second polling fallback. Browsers intentionally clear a file input when that DOM node is replaced. ProdSplat now keeps job cards stable while any local file is staged and clears the input only after a successful upload.

## COLMAP `sequential_matcher` aborts with `max_num_matches > 0 (0 vs. 0)`

ProdSplat 2.0.4 keeps COLMAP GPU-first and explicitly supplies `SiftMatching.max_num_matches=32768` plus GPU index 0 by default. If the matcher still exits with a recognized SiftGPU/CUDA runtime error, only that matching stage is retried on CPU. Splatfacto and gsplat stay GPU-backed. Tune this policy with `PRODSPLAT_GPU_INDEX`, `PRODSPLAT_MAX_NUM_MATCHES`, and `PRODSPLAT_COLMAP_CPU_FALLBACK` in `.env`.

## `COLMAP only found poses for ...` / very low camera-registration ratio

ProdSplat 2.0.5 no longer treats the profile frame count as a fixed extraction target. It measures the uploaded video's duration and increases the frame count for long captures so sequential COLMAP matching sees smaller viewpoint jumps.

For the `balanced` profile, a normal ~30 second capture still starts at the previously validated 90-frame baseline. A long capture can scale to 450 primary frames. If that model is still weak, the worker automatically tries a denser sequential pass (up to 700 frames), followed by a bounded exhaustive rescue (up to 260 frames) only when needed.

The worker writes `attempts/<N>/quality.json` even on final registration failure. It contains the capture statistics, adaptive frame plan, every registration attempt, and the best selected model.

A key distinction: `ns-process-data` can exit successfully while registering only a tiny fraction of cameras. Older CLI experiments did not impose ProdSplat's post-processing quality gate, so "All DONE" from Nerfstudio did not necessarily mean the camera model was good enough for production training. ProdSplat checks this explicitly before spending GPU time on Splatfacto.
