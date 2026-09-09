#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

fail() { echo "[ProdSplat] $*" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || fail "Docker is not installed."
docker compose version >/dev/null 2>&1 || fail "Docker Compose plugin is unavailable."
docker info >/dev/null 2>&1 || fail "Docker daemon is not running."

if [[ ! -f .env ]]; then
  cp .env.example .env
  token="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  sed -i "s/replace-with-random-token/${token}/" .env
  echo "[ProdSplat] Created .env with a random internal token."
fi

if grep -q 'replace-with-random-token' .env; then
  fail "Replace INTERNAL_TOKEN in .env."
fi

mkdir -p workspace/jobs workspace/tasks workspace/runtime

echo "[ProdSplat] Building and starting services..."
docker compose up -d --build

port="$(awk -F= '/^APP_PORT=/{v=$2} END{print v}' .env)"
port="${port:-8080}"
health="http://127.0.0.1:${port}/ready"
url="http://127.0.0.1:${port}/app/"

echo "[ProdSplat] Waiting for app + GPU worker readiness..."
for _ in $(seq 1 180); do
  if command -v curl >/dev/null 2>&1 && curl -fsS "$health" >/dev/null 2>&1; then
    echo "[ProdSplat] Ready: $url"
    command -v xdg-open >/dev/null 2>&1 && xdg-open "$url" >/dev/null 2>&1 || true
    echo "[ProdSplat] Ctrl+C stops log following; containers remain running."
    docker compose logs -f
    exit 0
  fi
  sleep 1
done

echo "[ProdSplat] Timed out waiting for readiness."
docker compose ps
docker compose logs --tail=250
exit 1
