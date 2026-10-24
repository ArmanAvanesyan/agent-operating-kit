package hostbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReadPolicy(path string) (Policy, error) {
	var p Policy
	b, e := ReadFile(path)
	if e == nil {
		e = Decode(b, "policy", &p)
	}
	if e != nil {
		return p, e
	}
	p.root = filepath.Dir(path)
	p.digest = Hash(b)
	p.seal = Digest(p)
	return p, ValidatePolicy(p)
}
func PolicyHash(p Policy) string {
	if p.digest != "" {
		return p.digest
	}
	return Digest(p)
}
func ValidatePolicy(p Policy) error {
	legacyHost := p.Host == "codex" || p.Host == "claude-code"
	v2Host := false
	if p.Version == VersionV2 {
		d, ok := hostdriver.Lookup(p.Host)
		v2Host = ok && d.Support == hostdriver.Supported && (p.Host == "opencode" || p.Host == "cursor" || p.Host == "gemini" || p.Host == "openclaw") && p.HostDriverSHA256 == d.Digest && p.HostProtocol == d.Protocol && !p.AllowNativeTasks
	}
	if !((p.Version == Version && legacyHost) || (p.Version == VersionV2 && v2Host)) || p.HostVersion == "" || p.AuthMode != "native-login" || !filepath.IsAbs(p.Executable) || len(p.ExecutableSHA256) != 64 {
		return fmt.Errorf("exact approved host/version/executable and native-login required")
	}
	if p.Version == Version && (p.HostDriverSHA256 != "" || p.HostProtocol != "") {
		return fmt.Errorf("v1 host policy cannot declare v2 driver fields")
	}
	if p.Version == VersionV2 {
		if len(p.Tools) != 0 || len(p.CodexConfig) != 0 || (len(p.Settings) > 0 && string(p.Settings) != "null") {
			return fmt.Errorf("v2 host protocols do not yet enforce tool or settings controls")
		}
		if p.Configuration.Effort != nil || p.Configuration.ReasoningBudgetTokens != nil || p.Configuration.Generation != nil || p.Configuration.ServiceTier != nil || p.Configuration.ModelRevision != nil || p.Configuration.Deployment != nil || p.Configuration.ToolProfile != nil {
			return fmt.Errorf("v2 host protocol cannot enforce requested configuration dimensions")
		}
		if p.Configuration.ReasoningMode != nil && *p.Configuration.ReasoningMode != "provider-default" {
			return fmt.Errorf("v2 host protocol supports provider-default reasoning only")
		}
	}
	if p.seal != "" && p.seal != Digest(p) {
		return fmt.Errorf("operator policy mutated")
	}
	if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 60 || len(p.WorkspaceRoots) == 0 || len(p.Models) == 0 {
		return fmt.Errorf("bounded timeout, workspace roots and model allowlist required")
	}
	if !filepath.IsAbs(p.Workspace) {
		return fmt.Errorf("absolute workspace required")
	}
	if len(p.Settings) > 0 && string(p.Settings) != "null" {
		var settings map[string]json.RawMessage
		if usage.Decode(p.Settings, &settings) != nil {
			return fmt.Errorf("invalid settings")
		}
		for k := range settings {
			if k != "disableAllHooks" && k != "sandbox" {
				return fmt.Errorf("settings may not enable ambient integrations")
			}
		}
		if v, ok := settings["disableAllHooks"]; ok && string(v) != "true" {
			return fmt.Errorf("hooks must be disabled")
		}
	}
	for k, v := range p.CodexConfig {
		if (strings.HasPrefix(k, "mcp_servers.") || strings.HasPrefix(k, "plugins.")) && strings.HasSuffix(k, ".enabled") && string(v) == "false" {
			continue
		}
		if k == "model_reasoning_effort" {
			continue
		}
		return fmt.Errorf("unsupported Codex config override")
	}
	for _, tool := range p.Tools {
		if tool == "Agent" || tool == "Task" || strings.HasPrefix(tool, "mcp__") || strings.ContainsAny(tool, "\r\n") {
			return fmt.Errorf("delegation and connectors not enabled")
		}
	}
	for _, name := range p.Environment {
		switch name {
		case "HOME", "PATH", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SYSTEMROOT", "TEMP", "TMP", "TMPDIR", "CODEX_HOME", "CLAUDE_CONFIG_DIR":
		default:
			return fmt.Errorf("credential/provider environment overrides prohibited")
		}
	}
	return nil
}
func executableHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 512<<20 {
		return "", fmt.Errorf("invalid executable")
	}
	h := sha256.New()
	_, e = io.Copy(h, io.LimitReader(f, (512<<20)+1))
	return hex.EncodeToString(h.Sum(nil)), e
}
func within(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
func verifyWorkspace(p Policy) error {
	real, e := filepath.EvalSymlinks(p.Workspace)
	if e != nil || real != filepath.Clean(p.Workspace) {
		return fmt.Errorf("workspace must be an existing canonical directory")
	}
	st, e := os.Stat(real)
	if e != nil || !st.IsDir() {
		return fmt.Errorf("workspace directory required")
	}
	for _, root := range p.WorkspaceRoots {
		r, e := filepath.EvalSymlinks(root)
		if e == nil && filepath.IsAbs(root) && r == filepath.Clean(root) && within(r, real) {
			return nil
		}
	}
	return fmt.Errorf("workspace outside approved roots")
}
func AllowedEnvironment(p Policy) []string {
	out := []string{}
	for _, n := range p.Environment {
		if v, ok := os.LookupEnv(n); ok {
			out = append(out, n+"="+v)
		}
	}
	return out
}
func Preflight(ctx context.Context, p Policy, runner Runner) (Capabilities, error) {
	c := Capabilities{Version: Version, Host: p.Host, HostVersion: p.HostVersion, ExecutableSHA256: p.ExecutableSHA256, Operations: map[string]string{"execution": "unverified", "cloud": "unsupported", "delegation": "disabled", "native-tasks": "unknown"}, Report: BaseReport()}
	if e := ValidatePolicy(p); e != nil {
		return c, e
	}
	if e := verifyWorkspace(p); e != nil {
		return c, e
	}
	h, e := executableHash(p.Executable)
	if e != nil || h != p.ExecutableSHA256 {
		return c, fmt.Errorf("approved executable changed")
	}
	for _, a := range p.ConfigurationAssets {
		if _, e = assetBytes(p.root, a); e != nil {
			return c, e
		}
	}
	proc, e := runner.Start(ctx, Launch{Executable: p.Executable, Args: []string{"--version"}, Dir: p.Workspace, Env: AllowedEnvironment(p), TimeoutSeconds: 5, MaxFrameBytes: MaxFrameBytes, MaxTotalBytes: MaxFrameBytes})
	if e != nil {
		return c, e
	}
	defer proc.Close()
	line, e := proc.Receive(ctx)
	if e != nil {
		return c, fmt.Errorf("host version discovery failed")
	}
	versionMatches := strings.Contains(string(line), p.HostVersion)
	if p.Version == VersionV2 {
		versionMatches = false
		for _, token := range strings.Fields(strings.TrimSpace(string(line))) {
			if token == p.HostVersion {
				versionMatches = true
			}
		}
	}
	if !versionMatches {
		return c, fmt.Errorf("host version pin mismatch")
	}
	if e = proc.Wait(); e != nil {
		return c, e
	}
	if p.Host == "claude-code" {
		auth, e := runner.Start(ctx, Launch{Executable: p.Executable, Args: []string{"auth", "status", "--json"}, Dir: p.Workspace, Env: AllowedEnvironment(p), TimeoutSeconds: 5, MaxFrameBytes: MaxFrameBytes, MaxTotalBytes: MaxFrameBytes})
		if e != nil {
			return c, e
		}
		defer auth.Close()
		var document []byte
		for {
			line, readErr := auth.Receive(ctx)
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return c, fmt.Errorf("native authentication status unavailable")
			}
			if len(document)+len(line) > MaxFrameBytes {
				return c, fmt.Errorf("authentication status too large")
			}
			document = append(document, line...)
			document = append(document, byte(10))
		}
		line := document
		var status map[string]json.RawMessage
		if usage.Decode(line, &status) != nil {
			return c, fmt.Errorf("native authentication status malformed")
		}
		var logged bool
		_ = json.Unmarshal(status["loggedIn"], &logged)
		method := text(status, "authMethod")
		if !logged || (method != "claude.ai" && method != "oauth_token") {
			return c, fmt.Errorf("existing subscription login required; API billing fallback prohibited")
		}
		if e = auth.Wait(); e != nil {
			return c, e
		}
	}
	c.Operations["execution"] = "fixture-implemented"
	return c, nil
}
