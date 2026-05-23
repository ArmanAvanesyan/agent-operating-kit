#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

resolve_aok() {
  if [ -x "$ROOT/scripts/aok" ]; then
    printf '%s\n' "$ROOT/scripts/aok"
    return 0
  fi
  if command -v aok >/dev/null 2>&1; then
    command -v aok
    return 0
  fi
  return 1
}

AOK="$(resolve_aok || true)"
if [ -z "${AOK:-}" ]; then
  echo "AOK CLI not found. Install Agent Operating Kit or run this inside the AOK checkout." >&2
  exit 127
fi

cmd="${1:-}"
case "$cmd" in
  doctor)
    "$AOK" doctor --report "$ROOT/.codex/aok-doctor-report.md"
    ;;
  validate)
    "$AOK" project validate "$ROOT"
    if [ -f "$ROOT/.codex-plugin/plugin.json" ]; then
      "$AOK" validate
    fi
    ;;
  render-bundles)
    "$AOK" render bundles --target all --out "$ROOT/.codex/aok-bundles"
    ;;
  project-init)
    "$AOK" project init "$ROOT"
    ;;
  claude-setup)
    "$AOK" claude setup --enable all
    ;;
  release-build)
    if [ -x "$ROOT/scripts/release/build.sh" ]; then
      "$ROOT/scripts/release/build.sh" "$ROOT/dist/release"
    else
      echo "AOK release build is only available from the AOK checkout." >&2
      exit 2
    fi
    ;;
  *)
    echo "Usage: $0 {doctor|validate|render-bundles|project-init|claude-setup|release-build}" >&2
    exit 2
    ;;
esac
