#!/usr/bin/env bash
set -euo pipefail
cd /src/editor
python3 - <<'PY'
from pathlib import Path
p = Path("src/main.ts")
s = p.read_text()
imp = "import { registerProductIntegration } from './product-integration';\n"
anchor = "import { registerPreferences } from './preferences';\n"
if imp not in s:
    if anchor not in s:
        raise SystemExit("SuperSplat integration anchor changed: preferences import not found")
    s = s.replace(anchor, anchor + imp)
call = "    registerProductIntegration(events);\n"
anchor2 = "    initFileHandler(scene, events, editorUI.appContainer.dom);\n"
if call not in s:
    if anchor2 not in s:
        raise SystemExit("SuperSplat integration anchor changed: initFileHandler call not found")
    s = s.replace(anchor2, anchor2 + call)
p.write_text(s)
PY
