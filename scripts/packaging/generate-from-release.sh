#!/usr/bin/env bash
# Regenerate Homebrew / Scoop / winget manifests from SHA256SUMS + index.json.
# Writes CI staging under packaging/out/ and same-repo published paths:
#   bucket/aok.json
#   packaging/homebrew/aok.rb
#   packaging/winget/*.yaml (non-.tmpl)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

VERSION=""
DIST_DIR=""
REPO="ArmanAvanesyan/agent-operating-kit"
OUT_DIR="${ROOT_DIR}/packaging/out"
TAG=""
PACKAGE_ID="ArmanAvanesyan.AgentOperatingKit"
PUBLISHER="ArmanAvanesyan"
PUBLISH_SAME_REPO=1

usage() {
  cat <<'EOF'
Usage: generate-from-release.sh [--version X.Y.Z] [--dist DIR] [--repo owner/name] [--out DIR] [--no-publish]

Reads SHA256SUMS (and optional index.json) to fill packaging templates.
With --version only, downloads SHA256SUMS from the GitHub Release tag vX.Y.Z.

By default also syncs Scoop/Homebrew/winget into same-repo published paths
(bucket/, packaging/homebrew/aok.rb, packaging/winget/*.yaml). Pass
--no-publish to write only under --out (default packaging/out).
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --dist) DIST_DIR="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    --no-publish) PUBLISH_SAME_REPO=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

tmpdir=""
cleanup() { [ -n "${tmpdir}" ] && rm -rf "${tmpdir}"; }
trap cleanup EXIT

if [ -n "${DIST_DIR}" ]; then
  SUMS="${DIST_DIR}/SHA256SUMS"
  if [ ! -f "${SUMS}" ]; then
    echo "Missing ${SUMS}" >&2
    exit 1
  fi
  if [ -z "${VERSION}" ] && [ -f "${DIST_DIR}/index.json" ]; then
    VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "${DIST_DIR}/index.json")"
  fi
else
  if [ -z "${VERSION}" ]; then
    VERSION="$(python3 -c 'import json; print(json.load(open(".codex-plugin/plugin.json"))["version"])')"
  fi
  TAG="v${VERSION}"
  tmpdir="$(mktemp -d)"
  SUMS="${tmpdir}/SHA256SUMS"
  echo "Downloading SHA256SUMS for ${TAG} from ${REPO}"
  ok=0
  for attempt in 1 2 3 4 5 6 7 8; do
    if command -v gh >/dev/null 2>&1; then
      if gh release download "${TAG}" --repo "${REPO}" --pattern SHA256SUMS --dir "${tmpdir}" 2>/dev/null; then
        ok=1
        break
      fi
    elif curl -fsSL -o "${SUMS}" "https://github.com/${REPO}/releases/download/${TAG}/SHA256SUMS"; then
      ok=1
      break
    fi
    echo "SHA256SUMS not ready yet (attempt ${attempt}); waiting 15s…"
    sleep 15
  done
  if [ "${ok}" != "1" ] || [ ! -f "${SUMS}" ]; then
    echo "Failed to download SHA256SUMS for ${TAG}" >&2
    exit 1
  fi
fi

if [ -z "${VERSION}" ]; then
  echo "VERSION required" >&2
  exit 1
fi
TAG="v${VERSION}"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"

hash_for() {
  local name="$1"
  awk -v n="$name" '$2 == n { print $1; exit }' "${SUMS}"
}

require_hash() {
  local name="$1"
  local h
  h="$(hash_for "${name}")"
  if [ -z "${h}" ]; then
    echo "Missing SHA256 for ${name} in ${SUMS}" >&2
    exit 1
  fi
  printf '%s' "${h}"
}

DARWIN_ARM64="aok-${VERSION}-darwin-arm64"
DARWIN_AMD64="aok-${VERSION}-darwin-amd64"
WINDOWS_AMD64="aok-${VERSION}-windows-amd64.exe"

DARWIN_ARM64_SHA256="$(require_hash "${DARWIN_ARM64}")"
DARWIN_AMD64_SHA256="$(require_hash "${DARWIN_AMD64}")"
WINDOWS_AMD64_SHA256="$(require_hash "${WINDOWS_AMD64}")"

# winget expects uppercase SHA256.
WINDOWS_AMD64_SHA256_UPPER="$(printf '%s' "${WINDOWS_AMD64_SHA256}" | tr '[:lower:]' '[:upper:]')"

replace_placeholders() {
  local src="$1"
  local dest="$2"
  mkdir -p "$(dirname "${dest}")"
  sed \
    -e "s|{{VERSION}}|${VERSION}|g" \
    -e "s|{{DARWIN_ARM64_URL}}|${BASE_URL}/${DARWIN_ARM64}|g" \
    -e "s|{{DARWIN_ARM64_SHA256}}|${DARWIN_ARM64_SHA256}|g" \
    -e "s|{{DARWIN_AMD64_URL}}|${BASE_URL}/${DARWIN_AMD64}|g" \
    -e "s|{{DARWIN_AMD64_SHA256}}|${DARWIN_AMD64_SHA256}|g" \
    -e "s|{{WINDOWS_AMD64_URL}}|${BASE_URL}/${WINDOWS_AMD64}|g" \
    -e "s|{{WINDOWS_AMD64_SHA256}}|${WINDOWS_AMD64_SHA256_UPPER}|g" \
    -e "s|{{PACKAGE_ID}}|${PACKAGE_ID}|g" \
    -e "s|{{PUBLISHER}}|${PUBLISHER}|g" \
    "${src}" > "${dest}"
}

write_scoop_json() {
  local dest="$1"
  mkdir -p "$(dirname "${dest}")"
  # Scoop hash is typically lowercase sha256 without prefix.
  sed \
    -e "s|{{VERSION}}|${VERSION}|g" \
    -e "s|{{WINDOWS_AMD64_URL}}|${BASE_URL}/${WINDOWS_AMD64}|g" \
    -e "s|{{WINDOWS_AMD64_SHA256}}|${WINDOWS_AMD64_SHA256}|g" \
    packaging/scoop/aok.json.tmpl > "${dest}"
}

write_winget_version_stub() {
  local dest="$1"
  mkdir -p "$(dirname "${dest}")"
  cat > "${dest}" <<EOF
PackageIdentifier: ${PACKAGE_ID}
PackageVersion: ${VERSION}
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.6.0
EOF
}

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}/homebrew" "${OUT_DIR}/scoop" "${OUT_DIR}/winget"

replace_placeholders packaging/homebrew/aok.rb.tmpl "${OUT_DIR}/homebrew/aok.rb"
write_scoop_json "${OUT_DIR}/scoop/aok.json"
replace_placeholders packaging/winget/ArmanAvanesyan.AgentOperatingKit.yaml.tmpl \
  "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.yaml"
replace_placeholders packaging/winget/ArmanAvanesyan.AgentOperatingKit.installer.yaml.tmpl \
  "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.installer.yaml"
replace_placeholders packaging/winget/ArmanAvanesyan.AgentOperatingKit.locale.en-US.yaml.tmpl \
  "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.locale.en-US.yaml"
write_winget_version_stub "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.version.yaml"

if [ "${PUBLISH_SAME_REPO}" = "1" ]; then
  # Scoop: repo-root bucket/ so `scoop bucket add aok <this-repo-url>` works.
  mkdir -p "${ROOT_DIR}/bucket"
  cp "${OUT_DIR}/scoop/aok.json" "${ROOT_DIR}/bucket/aok.json"
  # Homebrew: formula at packaging/homebrew/aok.rb for raw-URL install.
  cp "${OUT_DIR}/homebrew/aok.rb" "${ROOT_DIR}/packaging/homebrew/aok.rb"
  # winget: generated manifests beside templates (submit to winget-pkgs when ready).
  cp "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.yaml" \
    "${ROOT_DIR}/packaging/winget/ArmanAvanesyan.AgentOperatingKit.yaml"
  cp "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.installer.yaml" \
    "${ROOT_DIR}/packaging/winget/ArmanAvanesyan.AgentOperatingKit.installer.yaml"
  cp "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.locale.en-US.yaml" \
    "${ROOT_DIR}/packaging/winget/ArmanAvanesyan.AgentOperatingKit.locale.en-US.yaml"
  cp "${OUT_DIR}/winget/ArmanAvanesyan.AgentOperatingKit.version.yaml" \
    "${ROOT_DIR}/packaging/winget/ArmanAvanesyan.AgentOperatingKit.version.yaml"
fi

echo "Wrote manifests to ${OUT_DIR}"
if [ "${PUBLISH_SAME_REPO}" = "1" ]; then
  echo "Published same-repo paths:"
  echo "  bucket/aok.json"
  echo "  packaging/homebrew/aok.rb"
  echo "  packaging/winget/ArmanAvanesyan.AgentOperatingKit*.yaml"
fi
find "${OUT_DIR}" -type f | sort
