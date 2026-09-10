#!/usr/bin/env python3
"""Generate a minimal CycloneDX SBOM directly from an npm package-lock.json.

Why this exists:
`npm sbom` currently aborts when npm considers the installed tree invalid due
solely to upstream peer-dependency range mismatches. SuperSplat 2.32.5 pins
ESLint 10 while two lint-only plugins still declare peer support through ESLint
9. The application build itself succeeds. Reading the lockfile directly keeps
SBOM generation deterministic and independent of npm's peer validation.
"""
from __future__ import annotations

import json
import sys
import uuid
from pathlib import Path
from urllib.parse import quote


def purl(name: str, version: str) -> str:
    # npm package-url: scoped package names are percent-encoded as one namespace/name path.
    return f"pkg:npm/{quote(name, safe='/')}@{quote(str(version), safe='')}"


def main() -> int:
    if len(sys.argv) != 3:
        print(f"usage: {Path(sys.argv[0]).name} package-lock.json output.json", file=sys.stderr)
        return 2

    lock_path = Path(sys.argv[1])
    output_path = Path(sys.argv[2])
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    packages = lock.get("packages", {})

    root = packages.get("", {})
    root_name = root.get("name") or lock.get("name") or "npm-application"
    root_version = root.get("version") or lock.get("version") or "0.0.0"
    root_ref = purl(root_name, root_version)

    components = []
    seen = set()
    for package_path, meta in sorted(packages.items()):
        if package_path == "":
            continue
        name = meta.get("name")
        version = meta.get("version")
        if not name:
            marker = "node_modules/"
            if marker in package_path:
                name = package_path.rsplit(marker, 1)[-1]
        if not name or not version:
            continue
        ref = purl(name, version)
        if ref in seen:
            continue
        seen.add(ref)
        component = {
            "type": "library",
            "bom-ref": ref,
            "name": name,
            "version": str(version),
            "purl": ref,
        }
        resolved = meta.get("resolved")
        if resolved:
            component["externalReferences"] = [{"type": "distribution", "url": resolved}]
        components.append(component)

    bom = {
        "bomFormat": "CycloneDX",
        "specVersion": "1.5",
        "serialNumber": f"urn:uuid:{uuid.uuid4()}",
        "version": 1,
        "metadata": {
            "component": {
                "type": "application",
                "bom-ref": root_ref,
                "name": root_name,
                "version": str(root_version),
                "purl": root_ref,
            },
            "properties": [
                {"name": "prodsplat:sbom-source", "value": "npm-package-lock"},
                {"name": "prodsplat:package-lock-version", "value": str(lock.get("lockfileVersion", "unknown"))},
            ],
        },
        "components": components,
    }

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(bom, indent=2) + "\n", encoding="utf-8")
    print(f"wrote CycloneDX SBOM with {len(components)} components to {output_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
