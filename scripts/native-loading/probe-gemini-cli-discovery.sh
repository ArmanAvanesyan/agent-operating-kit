#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ $# -ne 3 ]]; then
  echo "usage: $0 /path/to/AOK-exported-agent.md expected-name expected-description" >&2
  exit 2
fi
exec python3 "$script_dir/verify_gemini_cli.py" "$1" --name "$2" --description "$3"
