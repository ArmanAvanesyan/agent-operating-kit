// Package kit provides kit-root discovery, version reading, and shared path helpers.
package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	PluginName             = "agent-operating-kit"
	MarketplaceName        = "private-local"
	MarketplaceDisplay     = "Private Local"
	ClaudeMarketplaceName  = "aok-claude-code"
	ClaudeRootPlugin       = "agent-operating-kit"
	PluginManifestRelPath  = ".codex-plugin/plugin.json"
	EnvRoot                = "AOK_ROOT"
)

var SupportedTargets = []string{
	"codex",
	"claude-code",
	"cursor",
	"opencode",
	"gemini",
	"openclaw",
	"hermes-agent",
}

// Error is a user-facing CLI error (printed as "Error: …", exit 1).
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func NewError(msg string) error { return &Error{Msg: msg} }

func NewErrorf(format string, args ...any) error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// IsKitRoot reports whether dir looks like an Agent Operating Kit root.
func IsKitRoot(dir string) bool {
	manifest := filepath.Join(dir, PluginManifestRelPath)
	data, err := os.ReadFile(manifest)
	if err != nil {
		return false
	}
	var parsed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false
	}
	return parsed.Name == PluginName
}

// FindRoot resolves the kit root using, in order:
//  1. AOK_ROOT (must be a valid kit root)
//  2. Walk up from startDir (default: cwd)
//  3. Platform install roots under home
//  4. Directory adjacent to the running executable (install layout)
func FindRoot(startDir string) (string, error) {
	if env := strings.TrimSpace(os.Getenv(EnvRoot)); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", NewErrorf("%s is not a valid path: %v", EnvRoot, err)
		}
		if !IsKitRoot(abs) {
			return "", NewErrorf("%s=%s is not a valid kit root", EnvRoot, abs)
		}
		return abs, nil
	}

	if startDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		startDir = cwd
	}
	absStart, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	if root, ok := walkAncestors(absStart); ok {
		return root, nil
	}

	home, err := os.UserHomeDir()
	if err == nil {
		for _, candidate := range InstallRoots(home) {
			if IsKitRoot(candidate) {
				return candidate, nil
			}
		}
	}

	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		// Common layouts: <kit>/dist/aok, <kit>/bin/aok, <install>/aok next to tree
		for _, candidate := range []string{
			filepath.Join(dir, ".."),
			filepath.Join(dir, "..", ".."),
			dir,
		} {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				continue
			}
			if IsKitRoot(abs) {
				return abs, nil
			}
		}
	}

	return "", NewError("could not locate kit root; set AOK_ROOT or run from a kit checkout")
}

func walkAncestors(start string) (string, bool) {
	dir := start
	for {
		if IsKitRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// InstallRoots returns platform kit install directories to probe (docs/cli-distribution.md).
func InstallRoots(home string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "Agent Operating Kit", PluginName),
		}
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return []string{
			filepath.Join(local, "Agent Operating Kit", PluginName),
		}
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".local", "share")
		}
		return []string{
			filepath.Join(xdg, "agent-operating-kit", PluginName),
			filepath.Join("/opt", "agent-operating-kit", PluginName),
		}
	}
}

// Version reads the kit version from .codex-plugin/plugin.json.
func Version(root string) (string, error) {
	path := filepath.Join(root, PluginManifestRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.Version) == "" {
		return "unknown", nil
	}
	return parsed.Version, nil
}

// MustVersion returns the version or "unknown".
func MustVersion(root string) string {
	v, err := Version(root)
	if err != nil {
		return "unknown"
	}
	return v
}

// UserHome resolves --home or the process home directory.
func UserHome(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return os.UserHomeDir()
	}
	expanded := expandHome(raw)
	return filepath.Abs(expanded)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// MarketplacePaths returns marketplace.json and the plugin link path under home.
func MarketplacePaths(home string) (marketplace string, pluginPath string) {
	root := filepath.Join(home, ".agents", "plugins")
	return filepath.Join(root, "marketplace.json"), filepath.Join(root, "plugins", PluginName)
}

// CLIShimPath is the PATH entry for the release CLI under home.
func CLIShimPath(home string) string {
	bin := filepath.Join(home, ".local", "bin")
	if runtime.GOOS == "windows" {
		return filepath.Join(bin, "aok.cmd")
	}
	return filepath.Join(bin, "aok")
}

// CLIBinaryPath is the installed Go binary path (Windows places aok.exe beside aok.cmd).
func CLIBinaryPath(home string) string {
	bin := filepath.Join(home, ".local", "bin")
	if runtime.GOOS == "windows" {
		return filepath.Join(bin, "aok.exe")
	}
	return filepath.Join(bin, "aok")
}

// Rel returns a slash-separated path relative to root when possible.
func Rel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// LoadJSON reads JSON object into dest; on missing file returns defaultData copy via dest.
func LoadJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return err
	}
	return json.Unmarshal(data, dest)
}

// WriteJSON writes pretty JSON with trailing newline (Python CLI style).
func WriteJSON(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}
