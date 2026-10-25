// Package releasebuild builds dependency-free AOK release artifacts (Go port of build.py).
package releasebuild

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const Product = "agent-operating-kit"

var (
	KitPaths = []string{
		".codex-plugin",
		"adapters",
		"core",
		"docs",
		"hooks",
		"methodologies",
		"packs",
		"references",
		"schemas",
		"skills",
		"templates",
		"work-items",
		"hooks.json",
		"LICENSE",
		"README.md",
		"scripts/aok",
		"scripts/install.sh",
		"scripts/project-init.sh",
		"scripts/github-sync.sh",
		"scripts/release/build.sh",
		"scripts/release/sign.sh",
		"cmd",
		"internal",
		"kits",
		"integrations",
		"scripts/workkit",
		"scripts/testing/verify-workkit-offline.py",
		"scripts/testing/verify-workkit-read-offline.py",
		"go.sum",
		"go.mod",
		"scripts/validate.sh",
		".github/workflows/release.yml",
		"CHANGELOG.md",
	}

	CodexPluginPaths = []string{
		".codex-plugin",
		"docs/codex-local-environments.md",
		"hooks",
		"methodologies",
		"work-items",
		"scripts/aok",
		"skills",
		"packs/repository-delivery",
		"templates/.codex",
		"hooks.json",
		"LICENSE",
		"README.md",
		"CHANGELOG.md",
	}

	PackPaths = []string{
		"adapters",
		"methodologies",
		"work-items",
		"packs",
		"schemas",
		"core/model.md",
		"LICENSE",
		"README.md",
		"CHANGELOG.md",
	}

	skipDirs     = map[string]bool{".git": true, ".venv": true, "venv": true, "__pycache__": true, ".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true, "dist": true, "build": true, "coverage": true}
	skipSuffixes = map[string]bool{".pyc": true, ".pyo": true, ".DS_Store": true}

	goBinaryTargets = []struct {
		GOOS, GOARCH, NameTmpl string
	}{
		{"darwin", "arm64", "aok-%s-darwin-arm64"},
		{"darwin", "amd64", "aok-%s-darwin-amd64"},
		{"linux", "amd64", "aok-%s-linux-amd64"},
		{"linux", "arm64", "aok-%s-linux-arm64"},
		{"windows", "amd64", "aok-%s-windows-amd64.exe"},
	}
)

// Artifact is one published release file entry for index.json / SHA256SUMS.
type Artifact struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Index is the release index.json document.
type Index struct {
	Product   string     `json:"product"`
	Version   string     `json:"version"`
	CreatedAt string     `json:"createdAt"`
	Artifacts []Artifact `json:"artifacts"`
}

// LoadVersion reads .codex-plugin/plugin.json version.
func LoadVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".codex-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	v := strings.TrimSpace(parsed.Version)
	if v == "" {
		return "", fmt.Errorf("missing version in .codex-plugin/plugin.json")
	}
	return v, nil
}

func isSkipped(name string) bool {
	if skipDirs[name] || skipSuffixes[name] || strings.HasPrefix(name, ".coverage") || strings.HasSuffix(name, ".egg-info") {
		return true
	}
	for suf := range skipSuffixes {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

func safeReleaseInput(root, path string) (os.FileInfo, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes release root")
	}
	current := root
	var info os.FileInfo
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err = os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return nil, fmt.Errorf("not a regular release resource: %s", current)
		}
	}
	return info, nil
}

func iterFiles(root string, paths []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	for _, raw := range paths {
		path := filepath.Join(root, raw)
		info, err := safeReleaseInput(root, path)
		if err != nil {
			return nil, fmt.Errorf("unsafe or missing release input %s: %w", raw, err)
		}
		if info.IsDir() {
			err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, relErr := filepath.Rel(root, p)
				if relErr != nil {
					return relErr
				}
				parts := strings.Split(rel, string(os.PathSeparator))
				for _, part := range parts {
					if skipDirs[part] {
						if d.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("refusing symlink release input: %s", p)
				}
				if !d.IsDir() && !d.Type().IsRegular() {
					return fmt.Errorf("refusing special release input: %s", p)
				}
				if d.IsDir() || isSkipped(d.Name()) {
					return nil
				}
				if !seen[p] {
					seen[p] = true
					files = append(files, p)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeZip(root, out, baseDir string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		hdr := &zip.FileHeader{
			Name:   filepath.ToSlash(filepath.Join(baseDir, rel)),
			Method: zip.Deflate,
		}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(root string, paths []string, dest string) error {
	files, err := iterFiles(root, paths)
	if err != nil {
		return err
	}
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeStageZip(stageRoot, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	parent := filepath.Dir(stageRoot)
	return filepath.WalkDir(stageRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		hdr := &zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
}

func writeStageTarGz(stageRoot, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()
	return filepath.WalkDir(stageRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(stageRoot), path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
}

func stageBundledBinary(payload, binary string, windows bool) error {
	binDir := filepath.Join(payload, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	name := "aok"
	if windows {
		name = "aok.exe"
	}
	dest := filepath.Join(binDir, name)
	if err := copyFile(binary, dest); err != nil {
		return err
	}
	if !windows {
		return os.Chmod(dest, 0o755)
	}
	return nil
}

func binaryPathFor(dist, version, goos, goarch string) string {
	suffix := ""
	if goos == "windows" {
		suffix = ".exe"
	}
	return filepath.Join(dist, fmt.Sprintf("aok-%s-%s-%s%s", version, goos, goarch, suffix))
}

func buildGoBinaries(root, dist, version string) ([]namedPath, error) {
	outDir := filepath.Join(dist, "_binaries")
	_ = os.RemoveAll(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	var results []namedPath
	for _, t := range goBinaryTargets {
		name := fmt.Sprintf(t.NameTmpl, version)
		out := filepath.Join(outDir, name)
		args := []string{"build", "-o", out}
		if t.GOOS == "darwin" {
			// Backported in Go 1.22: modern dyld requires LC_UUID.
			args = append(args, "-ldflags=-B=gobuildid")
		}
		cmd := exec.Command("go", append(args, "./cmd/aok")...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GOOS="+t.GOOS,
			"GOARCH="+t.GOARCH,
			"CGO_ENABLED=0",
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("go build %s/%s: %w", t.GOOS, t.GOARCH, err)
		}
		if err := os.Chmod(out, 0o755); err != nil {
			return nil, err
		}
		published := filepath.Join(dist, name)
		if err := copyFile(out, published); err != nil {
			return nil, err
		}
		results = append(results, namedPath{kind: fmt.Sprintf("cli-%s-%s", t.GOOS, t.GOARCH), path: published})
	}
	return results, nil
}

type namedPath struct {
	kind string
	path string
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func macosInstallerScript(version, targetArch string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/payload/%s"
INSTALL_DIR="${HOME}/Library/Application Support/Agent Operating Kit/%s"
REPORT_DIR="${HOME}/Library/Application Support/Agent Operating Kit"
REPORT_PATH="${REPORT_DIR}/setup-report.md"
ARCH="$(uname -m)"
AOK_BIN="${SOURCE_DIR}/bin/aok"

case "%s" in
  apple-silicon) EXPECTED_ARCH="arm64" ;;
  intel) EXPECTED_ARCH="x86_64" ;;
  *) EXPECTED_ARCH="" ;;
esac

if [ -n "$EXPECTED_ARCH" ] && [ "$ARCH" != "$EXPECTED_ARCH" ] && [ "${AOK_IGNORE_ARCH:-0}" != "1" ]; then
  echo "This installer targets %s macOS ($EXPECTED_ARCH), detected $ARCH." >&2
  echo "Set AOK_IGNORE_ARCH=1 to force installation." >&2
  exit 2
fi

if [ ! -d "${SOURCE_DIR}" ]; then
  echo "Missing installer payload: ${SOURCE_DIR}" >&2
  exit 1
fi
if [ ! -x "${AOK_BIN}" ]; then
  echo "Missing bundled aok binary: ${AOK_BIN}" >&2
  exit 1
fi

mkdir -p "${REPORT_DIR}"
rm -rf "${INSTALL_DIR}"
mkdir -p "$(dirname "${INSTALL_DIR}")"
cp -R "${SOURCE_DIR}" "${INSTALL_DIR}"

export AOK_ROOT="${INSTALL_DIR}"
"${INSTALL_DIR}/bin/aok" install --home "${HOME}" --replace || true
"${INSTALL_DIR}/bin/aok" doctor --home "${HOME}" --report "${REPORT_PATH}" || true

echo "Installed %s %s (%s)"
echo "Install path: ${INSTALL_DIR}"
echo "Setup report: ${REPORT_PATH}"
`, Product, Product, targetArch, targetArch, Product, version, targetArch)
}

func windowsInstallerPS1(version string) string {
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$SourceDir = Join-Path $ScriptDir "payload\%s"
$InstallRoot = Join-Path $env:LOCALAPPDATA "Agent Operating Kit"
$InstallDir = Join-Path $InstallRoot "%s"
$ReportPath = Join-Path $InstallRoot "setup-report.txt"
$AokBin = Join-Path $SourceDir "bin\aok.exe"

if (-not (Test-Path $SourceDir)) {
  Write-Error "Missing installer payload: $SourceDir"
}
if (-not (Test-Path $AokBin)) {
  Write-Error "Missing bundled aok binary: $AokBin"
}

New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
if (Test-Path $InstallDir) {
  Remove-Item -Recurse -Force $InstallDir
}
Copy-Item -Recurse -Force $SourceDir $InstallDir

$InstalledBin = Join-Path $InstallDir "bin\aok.exe"
$env:AOK_ROOT = $InstallDir
& $InstalledBin install --home $env:USERPROFILE --replace
& $InstalledBin doctor --home $env:USERPROFILE --report $ReportPath

Write-Host "Installed %s %s"
Write-Host "Install path: $InstallDir"
Write-Host "Setup report: $ReportPath"
`, Product, Product, Product, version)
}

func linuxUserInstallerSH(version string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/payload/%s"
INSTALL_ROOT="${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit"
INSTALL_DIR="${INSTALL_ROOT}/%s"
REPORT_PATH="${INSTALL_ROOT}/setup-report.md"
AOK_BIN="${SOURCE_DIR}/bin/aok"

if [ ! -d "${SOURCE_DIR}" ]; then
  echo "Missing installer payload: ${SOURCE_DIR}" >&2
  exit 1
fi
if [ ! -x "${AOK_BIN}" ]; then
  echo "Missing bundled aok binary: ${AOK_BIN}" >&2
  exit 1
fi

mkdir -p "${INSTALL_ROOT}"
rm -rf "${INSTALL_DIR}"
cp -R "${SOURCE_DIR}" "${INSTALL_DIR}"

export AOK_ROOT="${INSTALL_DIR}"
"${INSTALL_DIR}/bin/aok" install --home "${HOME}" --replace || true
"${INSTALL_DIR}/bin/aok" doctor --home "${HOME}" --report "${REPORT_PATH}" || true

echo "Installed %s %s (linux-user)"
echo "Install path: ${INSTALL_DIR}"
echo "Setup report: ${REPORT_PATH}"
`, Product, Product, Product, version)
}

func linuxSystemInstallerSH(version string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/payload/%s"
INSTALL_ROOT="/opt/agent-operating-kit"
INSTALL_DIR="${INSTALL_ROOT}/%s"
REPORT_PATH="${INSTALL_ROOT}/setup-report.md"
AOK_BIN="${SOURCE_DIR}/bin/aok"

if [ "$(id -u)" -ne 0 ]; then
  echo "System installer requires root. Re-run with sudo." >&2
  exit 1
fi
if [ ! -x "${AOK_BIN}" ]; then
  echo "Missing bundled aok binary: ${AOK_BIN}" >&2
  exit 1
fi

mkdir -p "${INSTALL_ROOT}"
rm -rf "${INSTALL_DIR}"
cp -R "${SOURCE_DIR}" "${INSTALL_DIR}"

TARGET_USER="${SUDO_USER:-}"
TARGET_HOME="${HOME}"
if [ -n "${TARGET_USER}" ]; then
  TARGET_HOME="$(getent passwd "${TARGET_USER}" | cut -d: -f6 || true)"
  if [ -z "${TARGET_HOME}" ]; then
    TARGET_HOME="/home/${TARGET_USER}"
  fi
fi

if [ -n "${TARGET_USER}" ] && [ -d "${TARGET_HOME}" ]; then
  su - "${TARGET_USER}" -c "AOK_ROOT='${INSTALL_DIR}' '${INSTALL_DIR}/bin/aok' install --home '${TARGET_HOME}' --replace" || true
  su - "${TARGET_USER}" -c "AOK_ROOT='${INSTALL_DIR}' '${INSTALL_DIR}/bin/aok' doctor --home '${TARGET_HOME}' --report '${REPORT_PATH}'" || true
fi

echo "Installed %s %s (linux-system)"
echo "Install path: ${INSTALL_DIR}"
echo "Setup report: ${REPORT_PATH}"
`, Product, Product, Product, version)
}

func buildMacOSInstallerZip(root, dist, version, targetArch string) (string, error) {
	name := fmt.Sprintf("%s-%s-macos-%s-installer", Product, version, targetArch)
	stage := filepath.Join(dist, "_staging", name)
	_ = os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload", Product)
	if err := copyTree(root, KitPaths, payload); err != nil {
		return "", err
	}
	goarch := "amd64"
	if targetArch == "apple-silicon" {
		goarch = "arm64"
	}
	if err := stageBundledBinary(payload, binaryPathFor(dist, version, "darwin", goarch), false); err != nil {
		return "", err
	}
	cmdPath := filepath.Join(stage, "install.command")
	if err := writeFile(cmdPath, macosInstallerScript(version, targetArch), 0o755); err != nil {
		return "", err
	}
	readme := strings.Join([]string{
		fmt.Sprintf("%s %s macOS %s installer-equivalent", Product, version, targetArch),
		"",
		"Double-click install.command in Finder.",
		"The installer copies the kit into ~/Library/Application Support/Agent Operating Kit,",
		"installs the Go aok binary onto ~/.local/bin,",
		"updates ~/.agents/plugins/marketplace.json,",
		"and writes a setup report next to the installed kit.",
		"No Python runtime is required.",
		"",
	}, "\n")
	if err := writeFile(filepath.Join(stage, "README.txt"), readme, 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dist, name+".zip")
	if err := writeStageZip(stage, out); err != nil {
		return "", err
	}
	_ = os.RemoveAll(filepath.Dir(stage))
	return out, nil
}

func buildWindowsInstallerZip(root, dist, version string) (string, error) {
	name := fmt.Sprintf("%s-%s-windows-installer", Product, version)
	stage := filepath.Join(dist, "_staging", name)
	_ = os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload", Product)
	if err := copyTree(root, KitPaths, payload); err != nil {
		return "", err
	}
	if err := stageBundledBinary(payload, binaryPathFor(dist, version, "windows", "amd64"), true); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(stage, "install.ps1"), windowsInstallerPS1(version), 0o644); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(stage, "install.cmd"), "@echo off\r\npowershell -ExecutionPolicy Bypass -File \"%~dp0install.ps1\"\r\n", 0o644); err != nil {
		return "", err
	}
	readme := strings.Join([]string{
		fmt.Sprintf("%s %s Windows installer", Product, version),
		"",
		"Run install.cmd (or install.ps1).",
		"The installer copies the kit into %LOCALAPPDATA%\\Agent Operating Kit\\agent-operating-kit,",
		"then runs the bundled aok.exe for install/doctor (no Python required).",
		"",
	}, "\n")
	if err := writeFile(filepath.Join(stage, "README.txt"), readme, 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dist, name+".zip")
	if err := writeStageZip(stage, out); err != nil {
		return "", err
	}
	_ = os.RemoveAll(filepath.Dir(stage))
	return out, nil
}

func buildLinuxUserInstallerTar(root, dist, version, goarch string) (string, error) {
	name := fmt.Sprintf("%s-%s-linux-user-installer", Product, version)
	stage := filepath.Join(dist, "_staging", name)
	_ = os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload", Product)
	if err := copyTree(root, KitPaths, payload); err != nil {
		return "", err
	}
	if err := stageBundledBinary(payload, binaryPathFor(dist, version, "linux", goarch), false); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(stage, "install.sh"), linuxUserInstallerSH(version), 0o755); err != nil {
		return "", err
	}
	readme := strings.Join([]string{
		fmt.Sprintf("%s %s Linux user installer", Product, version),
		"",
		"Run: ./install.sh",
		"Installs to ${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit.",
		"Bundles the Go aok binary (no Python required on the device).",
		"",
	}, "\n")
	if err := writeFile(filepath.Join(stage, "README.txt"), readme, 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dist, name+".tar.gz")
	if err := writeStageTarGz(stage, out); err != nil {
		return "", err
	}
	_ = os.RemoveAll(filepath.Dir(stage))
	return out, nil
}

func buildLinuxSystemInstallerTar(root, dist, version, goarch string) (string, error) {
	name := fmt.Sprintf("%s-%s-linux-system-installer", Product, version)
	stage := filepath.Join(dist, "_staging", name)
	_ = os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload", Product)
	if err := copyTree(root, KitPaths, payload); err != nil {
		return "", err
	}
	if err := stageBundledBinary(payload, binaryPathFor(dist, version, "linux", goarch), false); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(stage, "install.sh"), linuxSystemInstallerSH(version), 0o755); err != nil {
		return "", err
	}
	readme := strings.Join([]string{
		fmt.Sprintf("%s %s Linux system installer", Product, version),
		"",
		"Run as root: sudo ./install.sh",
		"Installs to /opt/agent-operating-kit.",
		"Bundles the Go aok binary (no Python required on the device).",
		"",
	}, "\n")
	if err := writeFile(filepath.Join(stage, "README.txt"), readme, 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dist, name+".tar.gz")
	if err := writeStageTarGz(stage, out); err != nil {
		return "", err
	}
	_ = os.RemoveAll(filepath.Dir(stage))
	return out, nil
}

// RenderBundlesCmd is overridden in tests; default runs Go aok render bundles.
var RenderBundlesCmd = func(root, outDir string) error {
	cmd := exec.Command("go", "run", "./cmd/aok", "render", "bundles", "--target", "all", "--out", outDir)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildBundlesZip(root, dist, version string) (string, error) {
	bundleRoot := filepath.Join(dist, "_bundles")
	_ = os.RemoveAll(bundleRoot)
	if err := RenderBundlesCmd(root, bundleRoot); err != nil {
		return "", fmt.Errorf("aok render bundles: %w", err)
	}
	out := filepath.Join(dist, fmt.Sprintf("%s-%s-bundles.zip", Product, version))
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	err = filepath.WalkDir(bundleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(bundleRoot, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		hdr := &zip.FileHeader{
			Name:   filepath.ToSlash(filepath.Join("bundles", rel)),
			Method: zip.Deflate,
		}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		return "", err
	}
	_ = os.RemoveAll(bundleRoot)
	return out, nil
}

func writeChecksums(dist string, artifacts []Artifact) error {
	var lines []string
	for _, a := range artifacts {
		lines = append(lines, fmt.Sprintf("%s  %s", a.SHA256, a.Name))
	}
	return os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// Build produces a full dist/release tree. Returns artifact metadata (including SHA256SUMS entry).
func Build(root, dist, version string) ([]Artifact, error) {
	_ = os.RemoveAll(dist)
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return nil, err
	}
	base := fmt.Sprintf("%s-%s", Product, version)
	var paths []namedPath

	type zipSpec struct {
		kind   string
		out    string
		inputs []string
	}
	for _, spec := range []zipSpec{
		{"kit", filepath.Join(dist, base+"-kit.zip"), KitPaths},
		{"codex-plugin", filepath.Join(dist, base+"-codex-plugin.zip"), CodexPluginPaths},
		{"packs", filepath.Join(dist, base+"-packs.zip"), PackPaths},
	} {
		files, err := iterFiles(root, spec.inputs)
		if err != nil {
			return nil, err
		}
		if err := writeZip(root, spec.out, base, files); err != nil {
			return nil, err
		}
		paths = append(paths, namedPath{kind: spec.kind, path: spec.out})
	}

	bins, err := buildGoBinaries(root, dist, version)
	if err != nil {
		return nil, err
	}
	paths = append(paths, bins...)

	macAS, err := buildMacOSInstallerZip(root, dist, version, "apple-silicon")
	if err != nil {
		return nil, err
	}
	paths = append(paths, namedPath{kind: "macos-apple-silicon-installer", path: macAS})
	macIntel, err := buildMacOSInstallerZip(root, dist, version, "intel")
	if err != nil {
		return nil, err
	}
	paths = append(paths, namedPath{kind: "macos-intel-installer", path: macIntel})
	win, err := buildWindowsInstallerZip(root, dist, version)
	if err != nil {
		return nil, err
	}
	paths = append(paths, namedPath{kind: "windows-installer", path: win})
	linuxUser, err := buildLinuxUserInstallerTar(root, dist, version, "amd64")
	if err != nil {
		return nil, err
	}
	paths = append(paths, namedPath{kind: "linux-user-installer", path: linuxUser})
	linuxSys, err := buildLinuxSystemInstallerTar(root, dist, version, "amd64")
	if err != nil {
		return nil, err
	}
	paths = append(paths, namedPath{kind: "linux-system-installer", path: linuxSys})
	bundles, err := buildBundlesZip(root, dist, version)
	if err != nil {
		return nil, err
	}
	paths = append(paths, namedPath{kind: "bundles", path: bundles})

	_ = os.RemoveAll(filepath.Join(dist, "_binaries"))

	var artifacts []Artifact
	for _, p := range paths {
		sum, err := fileSHA256(p.path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(p.path)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(p.path)
		artifacts = append(artifacts, Artifact{
			Name: name, Kind: p.kind, Path: name, Size: info.Size(), SHA256: sum,
		})
	}
	if err := writeChecksums(dist, artifacts); err != nil {
		return nil, err
	}
	sumPath := filepath.Join(dist, "SHA256SUMS")
	sumHash, err := fileSHA256(sumPath)
	if err != nil {
		return nil, err
	}
	sumInfo, err := os.Stat(sumPath)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, Artifact{
		Name: "SHA256SUMS", Kind: "checksums", Path: "SHA256SUMS", Size: sumInfo.Size(), SHA256: sumHash,
	})

	index := Index{
		Product:   Product,
		Version:   version,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Artifacts: artifacts,
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dist, "index.json"), append(raw, '\n'), 0o644); err != nil {
		return nil, err
	}
	return artifacts, nil
}
