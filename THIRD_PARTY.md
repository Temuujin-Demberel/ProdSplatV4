# Third-party software inventory

This project deliberately keeps third-party components replaceable. Before any
commercial distribution, run a full dependency/license audit for the exact image
and npm lockfile you ship.

Pinned/direct components in this repository:

| Component | Version / revision | License | Role |
|---|---|---|---|
| PlayCanvas Engine / SuperSplat ecosystem | versions pinned by SuperSplat lockfile | MIT (core engine/editor) | WebGL runtime and editor dependencies |
| SuperSplat | commit `e060989b202548848eb440a5005cd41a8b26f7db` (2.32.5-era source) | MIT | Browser Gaussian editor |
| Nerfstudio | Docker image `1.1.5` | Apache-2.0 | Video processing, COLMAP integration, Splatfacto training/export |
| gsplat | version bundled/pinned by Nerfstudio 1.1.5 | Apache-2.0 | CUDA Gaussian rasterization |
| COLMAP | bundled by Nerfstudio image | BSD-3-Clause | Camera pose / SfM processing |
| CUDA runtime | bundled through Nerfstudio/NVIDIA image | NVIDIA terms | GPU execution |

The original Graphdeco/Inria `gaussian-splatting` source repository is **not**
used by this application code. Do not silently replace gsplat/Nerfstudio code
with the original reference implementation in a proprietary distribution
without reviewing its license.

The exact transitive npm/Python/container dependencies must be audited at release
time. This file is engineering documentation, not legal advice.
