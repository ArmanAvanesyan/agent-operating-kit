#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
REPORT="${AOK_SETUP_REPORT:-$ROOT/.codex/aok-setup-report.md}"

if [ -x "$ROOT/scripts/aok" ]; then
  AOK="$ROOT/scripts/aok"
elif command -v aok >/dev/null 2>&1; then
  AOK="$(command -v aok)"
else
  mkdir -p "$(dirname "$REPORT")"
  {
    echo "# AOK Codex Setup Report"
    echo
    echo "Status: needs-attention"
    echo "AOK CLI was not found in this worktree or on PATH."
    echo "Install Agent Operating Kit before relying on Codex local-environment actions."
  } > "$REPORT"
  echo "AOK CLI not found. Wrote setup report: $REPORT" >&2
  exit 0
fi

mkdir -p "$(dirname "$REPORT")"
"$AOK" doctor --report "$REPORT" || true
"$AOK" project validate "$ROOT" >/dev/null 2>&1 || true

echo "AOK Codex setup completed. Report: $REPORT"
