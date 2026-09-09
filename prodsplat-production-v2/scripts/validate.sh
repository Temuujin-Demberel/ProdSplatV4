#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "== Go =="
(cd "$ROOT/app" && gofmt -w . && go test -timeout 30s ./...)

echo "== Python =="
python3 -m compileall -q "$ROOT/worker/prodsplat"
python3 -m pytest -q "$ROOT/worker/tests"

echo "== Browser / shell =="
node --check "$ROOT/web/app.js"
bash -n "$ROOT/product-scan.sh" "$ROOT/stop.sh" "$ROOT/editor-integration/inject.sh"

if command -v docker >/dev/null 2>&1; then
  echo "== Compose config =="
  (cd "$ROOT" && INTERNAL_TOKEN=validation-token docker compose config >/dev/null)
fi

echo "All static validation checks passed."
