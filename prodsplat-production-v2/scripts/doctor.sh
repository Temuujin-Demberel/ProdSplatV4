#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"; cd "$ROOT"
command -v docker >/dev/null || { echo "Docker missing"; exit 1; }
docker info >/dev/null
docker compose version
if [[ ! -f .env ]]; then cp .env.example .env; token="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"; sed -i "s/replace-with-random-token/${token}/" .env; fi

echo "Building worker image and validating GPU..."
docker compose build worker
docker compose run --rm --no-deps worker python - <<'PY'
from prodsplat.preflight import run_preflight
ok,error,gpu,versions = run_preflight()
print("healthy:", ok)
print("gpu:", gpu)
print("versions:", versions)
if error: print("error:", error)
raise SystemExit(0 if ok else 1)
PY
