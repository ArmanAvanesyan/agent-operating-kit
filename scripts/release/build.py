#!/usr/bin/env python3
"""Build dependency-free AOK release artifacts."""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import stat
import subprocess
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
PLUGIN_MANIFEST = ROOT / ".codex-plugin" / "plugin.json"
DIST_DEFAULT = ROOT / "dist" / "release"
PRODUCT = "agent-operating-kit"

KIT_PATHS = (
    ".codex-plugin",
    "adapters",
    "core",
    "docs",
    "hooks",
    "packs",
    "references",
    "schemas",
    "skills",
    "templates",
    "hooks.json",
    "LICENSE",
    "README.md",
    "scripts/aok",
    "scripts/install.sh",
    "scripts/project-init.sh",
    "scripts/release/build.py",
    "scripts/release/build.sh",
    "scripts/validate.sh",
    "CHANGELOG.md",
)

CODEX_PLUGIN_PATHS = (
    ".codex-plugin",
    "hooks",
    "skills",
    "hooks.json",
    "LICENSE",
    "README.md",
    "CHANGELOG.md",
)

PACK_PATHS = (
    "adapters",
    "packs",
    "schemas",
    "core/model.md",
    "LICENSE",
    "README.md",
    "CHANGELOG.md",
)

SKIP_DIRS = {".git", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache"}
SKIP_SUFFIXES = {".pyc", ".pyo", ".DS_Store"}


def load_version() -> str:
    data = json.loads(PLUGIN_MANIFEST.read_text())
    version = str(data.get("version", "")).strip()
    if not version:
        raise SystemExit(f"Missing version in {PLUGIN_MANIFEST}")
    return version


def is_skipped(path: Path) -> bool:
    return path.name in SKIP_DIRS or path.name in SKIP_SUFFIXES


def iter_files(paths: tuple[str, ...]) -> list[Path]:
    files: list[Path] = []
    for raw in paths:
        path = ROOT / raw
        if not path.exists():
            raise SystemExit(f"Release input does not exist: {raw}")
        if path.is_file():
            files.append(path)
            continue
        for child in sorted(path.rglob("*")):
            if any(part in SKIP_DIRS for part in child.relative_to(ROOT).parts):
                continue
            if child.is_file() and not is_skipped(child):
                files.append(child)
    return sorted(set(files))


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_zip(out: Path, base_dir: str, files: list[Path]) -> None:
    out.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in files:
            rel = path.relative_to(ROOT)
            info = zipfile.ZipInfo(f"{base_dir}/{rel.as_posix()}")
            mode = stat.S_IMODE(path.stat().st_mode)
            info.external_attr = (mode or 0o644) << 16
            archive.writestr(info, path.read_bytes())


def copy_tree(paths: tuple[str, ...], dest: Path) -> None:
    for path in iter_files(paths):
        rel = path.relative_to(ROOT)
        target = dest / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def installer_script(version: str) -> str:
    return f"""#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${{BASH_SOURCE[0]}}")" && pwd)"
SOURCE_DIR="${{SCRIPT_DIR}}/payload/{PRODUCT}"
INSTALL_DIR="${{HOME}}/Library/Application Support/Agent Operating Kit/{PRODUCT}"
REPORT_DIR="${{HOME}}/Library/Application Support/Agent Operating Kit"
REPORT_PATH="${{REPORT_DIR}}/setup-report.md"

if [ ! -d "${{SOURCE_DIR}}" ]; then
  echo "Missing installer payload: ${{SOURCE_DIR}}" >&2
  exit 1
fi

mkdir -p "${{REPORT_DIR}}"
rm -rf "${{INSTALL_DIR}}"
mkdir -p "$(dirname "${{INSTALL_DIR}}")"
cp -R "${{SOURCE_DIR}}" "${{INSTALL_DIR}}"

python3 "${{INSTALL_DIR}}/scripts/aok" install --home "${{HOME}}" --replace
python3 "${{INSTALL_DIR}}/scripts/aok" doctor --home "${{HOME}}" --report "${{REPORT_PATH}}"

echo "Installed {PRODUCT} {version}"
echo "Install path: ${{INSTALL_DIR}}"
echo "Setup report: ${{REPORT_PATH}}"
"""


def build_installer_zip(dist: Path, version: str) -> Path:
    name = f"{PRODUCT}-{version}-macos-installer"
    stage = dist / "_staging" / name
    if stage.exists():
        shutil.rmtree(stage)
    payload = stage / "payload" / PRODUCT
    copy_tree(KIT_PATHS, payload)
    command = stage / "install.command"
    command.write_text(installer_script(version))
    command.chmod(0o755)
    (stage / "README.txt").write_text(
        "\n".join(
            [
                f"{PRODUCT} {version} macOS installer-equivalent",
                "",
                "Double-click install.command in Finder.",
                "The installer copies the kit into ~/Library/Application Support/Agent Operating Kit,",
                "updates ~/.agents/plugins/marketplace.json, links ~/.local/bin/aok,",
                "and writes a setup report next to the installed kit.",
                "",
            ]
        )
    )
    out = dist / f"{name}.zip"
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in sorted(stage.rglob("*")):
            if path.is_file():
                rel = path.relative_to(stage.parent)
                info = zipfile.ZipInfo(rel.as_posix())
                mode = stat.S_IMODE(path.stat().st_mode)
                info.external_attr = (mode or 0o644) << 16
                archive.writestr(info, path.read_bytes())
    shutil.rmtree(stage.parent)
    return out


def build_bundles_zip(dist: Path, version: str) -> Path:
    bundle_root = dist / "_bundles"
    if bundle_root.exists():
        shutil.rmtree(bundle_root)
    subprocess.run(
        [str(ROOT / "scripts" / "aok"), "render", "bundles", "--target", "all", "--out", str(bundle_root)],
        cwd=ROOT,
        check=True,
    )
    out = dist / f"{PRODUCT}-{version}-bundles.zip"
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in sorted(bundle_root.rglob("*")):
            if path.is_file():
                rel = Path("bundles") / path.relative_to(bundle_root)
                info = zipfile.ZipInfo(rel.as_posix())
                mode = stat.S_IMODE(path.stat().st_mode)
                info.external_attr = (mode or 0o644) << 16
                archive.writestr(info, path.read_bytes())
    shutil.rmtree(bundle_root)
    return out


def write_checksums(dist: Path, artifacts: list[dict[str, object]]) -> None:
    lines = [f"{item['sha256']}  {item['name']}" for item in artifacts]
    (dist / "SHA256SUMS").write_text("\n".join(lines) + "\n")


def build(dist: Path, version: str) -> list[dict[str, object]]:
    if dist.exists():
        shutil.rmtree(dist)
    dist.mkdir(parents=True, exist_ok=True)

    base = f"{PRODUCT}-{version}"
    outputs = [
        ("kit", dist / f"{base}-kit.zip", KIT_PATHS),
        ("codex-plugin", dist / f"{base}-codex-plugin.zip", CODEX_PLUGIN_PATHS),
        ("packs", dist / f"{base}-packs.zip", PACK_PATHS),
    ]

    paths: list[tuple[str, Path]] = []
    for kind, out, inputs in outputs:
        write_zip(out, base, iter_files(inputs))
        paths.append((kind, out))
    paths.append(("macos-installer", build_installer_zip(dist, version)))
    paths.append(("bundles", build_bundles_zip(dist, version)))

    artifacts: list[dict[str, object]] = []
    for kind, path in paths:
        artifacts.append(
            {
                "name": path.name,
                "kind": kind,
                "path": path.name,
                "size": path.stat().st_size,
                "sha256": sha256(path),
            }
        )

    write_checksums(dist, artifacts)
    artifacts.append(
        {
            "name": "SHA256SUMS",
            "kind": "checksums",
            "path": "SHA256SUMS",
            "size": (dist / "SHA256SUMS").stat().st_size,
            "sha256": sha256(dist / "SHA256SUMS"),
        }
    )

    index = {
        "product": PRODUCT,
        "version": version,
        "createdAt": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "artifacts": artifacts,
    }
    (dist / "index.json").write_text(json.dumps(index, indent=2) + "\n")
    return artifacts


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", default=str(DIST_DEFAULT), help="release output directory")
    parser.add_argument("--version", default="", help="override manifest version")
    args = parser.parse_args(argv)

    version = args.version or load_version()
    artifacts = build(Path(args.dist).resolve(), version)
    for artifact in artifacts:
        print(f"{artifact['sha256']}  {artifact['name']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
