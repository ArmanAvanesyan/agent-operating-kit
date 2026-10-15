#!/usr/bin/env bash
# Optional release code signing. Never fails the release when secrets/tools are absent.
# Invoked from .github/workflows/release.yml after build, before publish.
set -uo pipefail

DIST_DIR="${1:-dist/release}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if [ ! -d "${DIST_DIR}" ]; then
  echo "sign.sh: dist dir missing (${DIST_DIR}); skipping signing."
  exit 0
fi

VERSION="$(python3 -c 'import json; print(json.load(open(".codex-plugin/plugin.json"))["version"])' 2>/dev/null || true)"
if [ -z "${VERSION}" ]; then
  echo "sign.sh: could not read version; skipping signing."
  exit 0
fi

signed_any=0

sign_macos() {
  if [ -z "${MACOS_CERTIFICATE:-}" ] || [ -z "${MACOS_CERTIFICATE_PWD:-}" ]; then
    echo "sign.sh: macOS secrets not set; skipping codesign."
    return 0
  fi
  if ! command -v codesign >/dev/null 2>&1; then
    echo "sign.sh: codesign not available on this runner; skipping macOS signing."
    return 0
  fi

  local keychain="aok-signing.keychain-db"
  local keychain_pw
  keychain_pw="$(openssl rand -base64 32)"
  local cert_path
  cert_path="$(mktemp /tmp/aok-cert.XXXXXX.p12)"

  cleanup_macos() {
    security delete-keychain "${keychain}" >/dev/null 2>&1 || true
    rm -f "${cert_path}"
  }
  trap cleanup_macos EXIT

  echo "${MACOS_CERTIFICATE}" | base64 --decode > "${cert_path}"
  security create-keychain -p "${keychain_pw}" "${keychain}"
  security set-keychain-settings -lut 21600 "${keychain}"
  security unlock-keychain -p "${keychain_pw}" "${keychain}"
  security import "${cert_path}" -P "${MACOS_CERTIFICATE_PWD}" -A -t cert -f pkcs12 -k "${keychain}"
  security list-keychain -d user -s "${keychain}" $(security list-keychain -d user | sed -e s/\"//g)
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "${keychain_pw}" "${keychain}" >/dev/null

  local identity="${MACOS_SIGNING_IDENTITY:-}"
  if [ -z "${identity}" ]; then
    identity="$(security find-identity -v -p codesigning "${keychain}" | awk -F'"' '/Developer ID Application|Apple Distribution|Mac Developer|Apple Development/{print $2; exit}')"
  fi
  if [ -z "${identity}" ]; then
    echo "sign.sh: no codesigning identity found; skipping macOS signing."
    cleanup_macos
    trap - EXIT
    return 0
  fi

  local targets=(
    "${DIST_DIR}/aok-${VERSION}-darwin-arm64"
    "${DIST_DIR}/aok-${VERSION}-darwin-amd64"
  )
  local f
  for f in "${targets[@]}"; do
    if [ -f "${f}" ]; then
      echo "sign.sh: codesign ${f}"
      codesign --force --options runtime --sign "${identity}" "${f}" || {
        echo "sign.sh: codesign failed for ${f}; continuing unsigned."
        continue
      }
      signed_any=1
    fi
  done

  # Optional notarization (stub): requires Apple ID / team / notary credentials.
  if [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ] && [ -n "${APPLE_APP_SPECIFIC_PASSWORD:-}" ]; then
    if command -v xcrun >/dev/null 2>&1; then
      echo "sign.sh: notarization credentials present; notarytool submit is documented but not required for v1."
      echo "sign.sh: set AOK_ENABLE_NOTARIZE=1 to attempt notarization of darwin installer zips."
      if [ "${AOK_ENABLE_NOTARIZE:-0}" = "1" ]; then
        for zip in "${DIST_DIR}"/agent-operating-kit-"${VERSION}"-macos-*-installer.zip; do
          [ -f "${zip}" ] || continue
          echo "sign.sh: notarytool submit ${zip}"
          xcrun notarytool submit "${zip}" \
            --apple-id "${APPLE_ID}" \
            --team-id "${APPLE_TEAM_ID}" \
            --password "${APPLE_APP_SPECIFIC_PASSWORD}" \
            --wait || echo "sign.sh: notarize failed for ${zip}; continuing."
        done
      fi
    fi
  else
    echo "sign.sh: Apple notarization secrets not set; skipping notarize (codesign-only when cert present)."
  fi

  cleanup_macos
  trap - EXIT
  return 0
}

sign_windows() {
  # Prefer Azure Trusted Signing when configured; else Authenticode PFX via osslsigncode/signtool.
  if [ -n "${AZURE_TRUSTED_SIGNING_CLIENT_ID:-}" ] && [ -n "${AZURE_TRUSTED_SIGNING_TENANT_ID:-}" ] && [ -n "${AZURE_TRUSTED_SIGNING_CLIENT_SECRET:-}" ] && [ -n "${AZURE_TRUSTED_SIGNING_ACCOUNT:-}" ] && [ -n "${AZURE_TRUSTED_SIGNING_PROFILE:-}" ] && [ -n "${AZURE_TRUSTED_SIGNING_ENDPOINT:-}" ]; then
    if command -v trusted-signing-cli >/dev/null 2>&1 || command -v az >/dev/null 2>&1; then
      echo "sign.sh: Azure Trusted Signing credentials present."
      local exe="${DIST_DIR}/aok-${VERSION}-windows-amd64.exe"
      if [ -f "${exe}" ]; then
        if command -v trusted-signing-cli >/dev/null 2>&1; then
          echo "sign.sh: trusted-signing-cli ${exe}"
          trusted-signing-cli sign \
            -e "${AZURE_TRUSTED_SIGNING_ENDPOINT}" \
            -a "${AZURE_TRUSTED_SIGNING_ACCOUNT}" \
            -c "${AZURE_TRUSTED_SIGNING_PROFILE}" \
            -d "Agent Operating Kit" \
            "${exe}" && signed_any=1 || echo "sign.sh: Trusted Signing failed; continuing unsigned."
        else
          echo "sign.sh: install trusted-signing-cli on the runner to sign Windows binaries; skipping."
        fi
      fi
    else
      echo "sign.sh: Trusted Signing secrets set but CLI not installed; skipping Windows signing."
    fi
    return 0
  fi

  if [ -z "${WINDOWS_CERT:-}" ] || [ -z "${WINDOWS_CERT_PWD:-}" ]; then
    echo "sign.sh: Windows signing secrets not set; skipping."
    return 0
  fi

  local exe="${DIST_DIR}/aok-${VERSION}-windows-amd64.exe"
  if [ ! -f "${exe}" ]; then
    echo "sign.sh: Windows binary missing; skipping."
    return 0
  fi

  local pfx
  pfx="$(mktemp /tmp/aok-win.XXXXXX.pfx)"
  echo "${WINDOWS_CERT}" | base64 --decode > "${pfx}"

  if command -v osslsigncode >/dev/null 2>&1; then
    echo "sign.sh: osslsigncode ${exe}"
    local signed="${exe}.signed"
    if osslsigncode sign -pkcs12 "${pfx}" -pass "${WINDOWS_CERT_PWD}" -n "Agent Operating Kit" -in "${exe}" -out "${signed}"; then
      mv "${signed}" "${exe}"
      signed_any=1
    else
      echo "sign.sh: osslsigncode failed; continuing unsigned."
      rm -f "${signed}"
    fi
  elif command -v signtool.exe >/dev/null 2>&1 || command -v signtool >/dev/null 2>&1; then
    local st
    st="$(command -v signtool.exe || command -v signtool)"
    echo "sign.sh: signtool ${exe}"
    "${st}" sign //f "${pfx}" //p "${WINDOWS_CERT_PWD}" //fd SHA256 "${exe}" && signed_any=1 || echo "sign.sh: signtool failed; continuing unsigned."
  else
    echo "sign.sh: no Windows signing tool (osslsigncode/signtool); skipping."
  fi
  rm -f "${pfx}"
  return 0
}

sign_linux_optional() {
  # v1: optional cosign/GPG over checksums only; skip unless explicitly enabled.
  if [ "${AOK_ENABLE_LINUX_SIGN:-0}" != "1" ]; then
    echo "sign.sh: Linux cosign/GPG skipped in v1 (set AOK_ENABLE_LINUX_SIGN=1 + COSIGN_KEY to enable)."
    return 0
  fi
  if [ -z "${COSIGN_KEY:-}" ]; then
    echo "sign.sh: COSIGN_KEY not set; skipping Linux signing."
    return 0
  fi
  if ! command -v cosign >/dev/null 2>&1; then
    echo "sign.sh: cosign not installed; skipping."
    return 0
  fi
  local sums="${DIST_DIR}/SHA256SUMS"
  if [ -f "${sums}" ]; then
    echo "sign.sh: cosign sign-blob SHA256SUMS"
    COSIGN_PASSWORD="${COSIGN_PASSWORD:-}" cosign sign-blob --key env://COSIGN_KEY "${sums}" --output-signature "${sums}.sig" \
      && signed_any=1 \
      || echo "sign.sh: cosign failed; continuing."
  fi
  return 0
}

refresh_checksums() {
  if [ "${signed_any}" != "1" ]; then
    return 0
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "sign.sh: go missing; cannot refresh SHA256SUMS after signing."
    return 0
  fi
  echo "sign.sh: refreshing SHA256SUMS / index.json after signing"
  # Rebuild checksums for all regular files in dist (except index.json itself first).
  python3 - <<'PY' "${DIST_DIR}" || true
import hashlib, json, os, sys, time
from pathlib import Path
dist = Path(sys.argv[1])
artifacts = []
for path in sorted(dist.iterdir()):
    if not path.is_file():
        continue
    if path.name in ("index.json", "SHA256SUMS"):
        continue
    if path.name.endswith(".sig"):
        continue
    h = hashlib.sha256(path.read_bytes()).hexdigest()
    artifacts.append({"name": path.name, "kind": "artifact", "path": path.name, "size": path.stat().st_size, "sha256": h})
lines = [f"{a['sha256']}  {a['name']}" for a in artifacts]
(dist / "SHA256SUMS").write_text("\n".join(lines) + "\n")
sums = dist / "SHA256SUMS"
artifacts.append({
    "name": "SHA256SUMS",
    "kind": "checksums",
    "path": "SHA256SUMS",
    "size": sums.stat().st_size,
    "sha256": hashlib.sha256(sums.read_bytes()).hexdigest(),
})
# Preserve product/version from prior index when present.
product, version = "agent-operating-kit", ""
prev = dist / "index.json"
if prev.exists():
    try:
        data = json.loads(prev.read_text())
        product = data.get("product", product)
        version = data.get("version", version)
    except Exception:
        pass
if not version:
    try:
        version = json.loads(Path(".codex-plugin/plugin.json").read_text())["version"]
    except Exception:
        version = "unknown"
index = {
    "product": product,
    "version": version,
    "createdAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "artifacts": artifacts,
}
prev.write_text(json.dumps(index, indent=2) + "\n")
print(f"refreshed {len(artifacts)} artifact checksums")
PY
}

sign_macos
sign_windows
sign_linux_optional
refresh_checksums

echo "sign.sh: done (unsigned releases remain valid when secrets are absent)."
exit 0
