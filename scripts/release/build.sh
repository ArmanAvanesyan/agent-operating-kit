#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DIST_DIR="${1:-${ROOT_DIR}/dist/release}"

python3 "${SCRIPT_DIR}/build.py" --dist "${DIST_DIR}"
