package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

// Options configure install.
type Options struct {
	Root    string
	Home    string
	Replace bool
	// BinarySource is the path of the currently running aok binary to copy onto PATH.
	// If empty, os.Executable() is used.
	BinarySource string
}

// Result summarizes install outcomes.
type Result struct {
	Marketplace string
	PluginPath  string
	PluginMode  string
	CLIPath     string
	CLIMode     string
}

// Run registers marketplace, links the plugin, and installs the Go binary on PATH.
func Run(opts Options, stderr io.Writer) (*Result, error) {
	if opts.Root == "" {
		return nil, kit.NewError("kit root is required")
	}
	home := opts.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}

	if _, err := exec.LookPath("codex"); err != nil {
		fmt.Fprintln(stderr, "Warning: missing optional commands: codex")
		fmt.Fprintln(stderr, "Install continues; `aok doctor` reports optional capabilities separately.")
	}

	marketplace, linkPath := kit.MarketplacePaths(home)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return nil, err
	}
	pluginMode, err := ensurePluginPath(linkPath, opts.Root, opts.Replace)
	if err != nil {
		return nil, err
	}

	data := map[string]any{
		"name":      kit.MarketplaceName,
		"interface": map[string]any{"displayName": kit.MarketplaceDisplay},
		"plugins":   []any{},
	}
	if raw, err := os.ReadFile(marketplace); err == nil {
		var parsed map[string]any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, kit.NewErrorf("Invalid JSON in %s: %v", marketplace, err)
		}
		data = parsed
	}
	if _, ok := data["name"]; !ok {
		data["name"] = kit.MarketplaceName
	}
	iface, _ := data["interface"].(map[string]any)
	if iface == nil {
		iface = map[string]any{}
		data["interface"] = iface
	}
	if _, ok := iface["displayName"]; !ok {
		iface["displayName"] = kit.MarketplaceDisplay
	}
	plugins, _ := data["plugins"].([]any)
	entry := map[string]any{
		"name": kit.PluginName,
		"source": map[string]any{
			"source": "local",
			"path":   "./plugins/" + kit.PluginName,
		},
		"policy": map[string]any{
			"installation":   "AVAILABLE",
			"authentication": "ON_INSTALL",
		},
		"category": "Productivity",
	}
	replaced := false
	for i, raw := range plugins {
		plugin, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if plugin["name"] == kit.PluginName {
			plugins[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		plugins = append(plugins, entry)
	}
	data["plugins"] = plugins
	if err := kit.WriteJSON(marketplace, data); err != nil {
		return nil, err
	}

	cliPath, cliMode, err := ensureCLIBinary(home, opts.BinarySource, opts.Replace)
	if err != nil {
		return nil, err
	}

	return &Result{
		Marketplace: marketplace,
		PluginPath:  linkPath,
		PluginMode:  pluginMode,
		CLIPath:     cliPath,
		CLIMode:     cliMode,
	}, nil
}

func ensurePluginPath(linkPath, root string, replace bool) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	_, lstatErr := os.Lstat(linkPath)
	exists := lstatErr == nil
	if exists {
		currentMatches := false
		existingMode := ""
		if resolved, err := filepath.EvalSymlinks(linkPath); err == nil {
			resolved, _ = filepath.Abs(resolved)
			if resolved == rootAbs {
				currentMatches = true
				info, _ := os.Lstat(linkPath)
				if info != nil && info.Mode()&os.ModeSymlink != 0 {
					return "symlink", nil
				}
				return "junction", nil
			}
		}
		if runtime.GOOS == "windows" && kit.IsKitRoot(linkPath) {
			existingMode = "copy"
			currentMatches = true
		}
		if currentMatches {
			return existingMode, nil
		}
		if !replace {
			return "", kit.NewErrorf("Refusing to replace existing plugin path: %s", linkPath)
		}
		if err := removeExistingPath(linkPath); err != nil {
			return "", err
		}
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink(rootAbs, linkPath); err != nil {
			return "", err
		}
		return "symlink", nil
	}

	// Windows: try junction via cmd, else copy.
	cmd := exec.Command("cmd", "/c", "mklink", "/J", linkPath, rootAbs)
	if err := cmd.Run(); err == nil {
		return "junction", nil
	}
	if err := copyTree(rootAbs, linkPath); err != nil {
		return "", err
	}
	return "copy", nil
}

// ensureCLIBinary installs the Go binary to ~/.local/bin (not a symlink to scripts/aok).
func ensureCLIBinary(home, binarySource string, replace bool) (path, mode string, err error) {
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", err
	}

	src := binarySource
	if src == "" {
		src, err = os.Executable()
		if err != nil {
			return "", "", err
		}
		if resolved, err := filepath.EvalSymlinks(src); err == nil {
			src = resolved
		}
	}

	if runtime.GOOS == "windows" {
		return ensureWindowsCLI(binDir, src, replace)
	}
	return ensureUnixCLI(binDir, src, replace)
}

func ensureUnixCLI(binDir, src string, replace bool) (string, string, error) {
	dest := filepath.Join(binDir, "aok")
	if _, err := os.Lstat(dest); err == nil {
		if isDesiredGoBinary(dest, src) {
			return dest, "binary", nil
		}
		if !replace {
			return "", "", kit.NewErrorf("Refusing to replace existing executable: %s", dest)
		}
		if err := removeExistingPath(dest); err != nil {
			return "", "", err
		}
	}
	if err := copyFile(src, dest, 0o755); err != nil {
		return "", "", err
	}
	return dest, "binary", nil
}

func ensureWindowsCLI(binDir, src string, replace bool) (string, string, error) {
	exeDest := filepath.Join(binDir, "aok.exe")
	cmdDest := filepath.Join(binDir, "aok.cmd")
	shimText := windowsCLIShimText(exeDest)

	if _, err := os.Lstat(exeDest); err == nil {
		if !isDesiredGoBinary(exeDest, src) {
			if !replace {
				return "", "", kit.NewErrorf("Refusing to replace existing executable: %s", exeDest)
			}
			if err := removeExistingPath(exeDest); err != nil {
				return "", "", err
			}
		}
	}
	if _, err := os.Stat(exeDest); err != nil {
		if err := copyFile(src, exeDest, 0o755); err != nil {
			return "", "", err
		}
	} else if replace && !sameFileContent(src, exeDest) {
		if err := copyFile(src, exeDest, 0o755); err != nil {
			return "", "", err
		}
	}

	if raw, err := os.ReadFile(cmdDest); err == nil {
		normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
		if normalized != shimText {
			if !replace {
				return "", "", kit.NewErrorf("Refusing to replace existing executable: %s", cmdDest)
			}
			if err := os.WriteFile(cmdDest, []byte(strings.ReplaceAll(shimText, "\n", "\r\n")), 0o644); err != nil {
				return "", "", err
			}
		}
	} else {
		if err := os.WriteFile(cmdDest, []byte(strings.ReplaceAll(shimText, "\n", "\r\n")), 0o644); err != nil {
			return "", "", err
		}
	}

	// Upgrade old Python Unix-named shim on Windows when --replace.
	legacy := filepath.Join(binDir, "aok")
	if _, err := os.Lstat(legacy); err == nil && replace {
		_ = removeExistingPath(legacy)
	}
	return cmdDest, "cmd", nil
}

func windowsCLIShimText(exePath string) string {
	// Logical newlines; writers normalize to CRLF on disk.
	return "@echo off\nsetlocal\n\"" + exePath + "\" %*\n"
}

func isDesiredGoBinary(dest, src string) bool {
	info, err := os.Lstat(dest)
	if err != nil {
		return false
	}
	// Old Python shim was a symlink to scripts/aok — never treat as desired.
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	// Heuristic: regular file that looks like our binary (same size as source, or executable non-script).
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false
	}
	if info.Size() == srcInfo.Size() && sameFileContent(src, dest) {
		return true
	}
	// Treat existing non-symlink binary as upgrade candidate via --replace only.
	return false
}

func sameFileContent(a, b string) bool {
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	if len(da) != len(db) {
		return false
	}
	for i := range da {
		if da[i] != db[i] {
			return false
		}
	}
	return true
}

func removeExistingPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func copyFile(src, dest string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, info.Mode())
	})
}
