# syntax=docker/dockerfile:1.7

ARG SUPER_SPLAT_COMMIT=e060989b202548848eb440a5005cd41a8b26f7db

# -----------------------------------------------------------------------------
# Build the pinned SuperSplat fork with the ProdSplat integration injected.
# -----------------------------------------------------------------------------
FROM node:22-bookworm AS editor-build
ARG SUPER_SPLAT_COMMIT
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates python3 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git clone https://github.com/playcanvas/supersplat.git editor \
    && cd editor \
    && git checkout "${SUPER_SPLAT_COMMIT}"
WORKDIR /src/editor
COPY editor-integration/product-integration.ts src/product-integration.ts
COPY editor-integration/inject.sh /tmp/inject.sh
COPY scripts/lockfile_sbom.py /tmp/lockfile_sbom.py
RUN chmod +x /tmp/inject.sh && /tmp/inject.sh
RUN npm ci && npm run build \
    && cp LICENSE /tmp/SuperSplat-LICENSE.txt \
    && python3 /tmp/lockfile_sbom.py package-lock.json /tmp/supersplat-sbom.json

# -----------------------------------------------------------------------------
# Compile the Go control plane. It intentionally has zero third-party Go deps.
# -----------------------------------------------------------------------------
FROM golang:1.23-bookworm AS go-build
ARG APP_VERSION=2.0.5
ARG APP_COMMIT=local
WORKDIR /src/app
COPY app/go.mod app/go.sum ./
COPY app/ ./
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/demberel-temuujin/prodsplat-production/app/internal/buildinfo.Version=${APP_VERSION} -X github.com/demberel-temuujin/prodsplat-production/app/internal/buildinfo.Commit=${APP_COMMIT}" \
    -o /out/prodsplat ./cmd/server

# -----------------------------------------------------------------------------
# App runtime: Go binary + dashboard + compiled SuperSplat.
# -----------------------------------------------------------------------------
FROM debian:bookworm-slim AS app-runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/prodsplat
COPY --from=go-build /out/prodsplat /usr/local/bin/prodsplat
COPY web /opt/prodsplat/web
COPY --from=editor-build /src/editor/dist /opt/prodsplat/editor
COPY licenses /opt/prodsplat/licenses
COPY --from=editor-build /tmp/SuperSplat-LICENSE.txt /opt/prodsplat/licenses/SuperSplat-MIT.txt
COPY --from=editor-build /tmp/supersplat-sbom.json /opt/prodsplat/licenses/supersplat-sbom.json
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/prodsplat"]

# -----------------------------------------------------------------------------
# GPU worker. Nerfstudio v1.1.5 pins the compatible CUDA/PyTorch/gsplat stack.
# -----------------------------------------------------------------------------
FROM ghcr.io/nerfstudio-project/nerfstudio:1.1.5 AS worker-runtime
WORKDIR /opt/prodsplat-worker
COPY worker /opt/prodsplat-worker
RUN chmod +x /opt/prodsplat-worker/bin/colmap
ENV PYTHONPATH=/opt/prodsplat-worker
# Put the ProdSplat COLMAP compatibility wrapper before upstream COLMAP.
ENV PATH="/opt/prodsplat-worker/bin:${PATH}"
ENV PRODSPLAT_GPU_INDEX=0
ENV PRODSPLAT_MAX_NUM_MATCHES=32768
ENV PRODSPLAT_COLMAP_CPU_FALLBACK=1
CMD ["python", "-m", "prodsplat.worker_main"]
