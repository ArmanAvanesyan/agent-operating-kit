package nativeadapters_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	agy "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/agy"
	claude "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/claude"
	codex "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/codex"
	cursor "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/cursor"
	gemini "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/gemini"
	hermes "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/hermes"
	openclaw "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/openclaw"
	opencode "github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/opencode"
)

type adapterCase struct {
	name, target, scope, hostStatus string
	adapter                         nativeadapter.Adapter
	files                           []nativeadapter.File
}

func TestAllNativeAdaptersConformToEngineBoundaries(t *testing.T) {
	cases := []adapterCase{
		{name: "agy", target: "agy", scope: "project", hostStatus: "not-run", adapter: agy.New(), files: []nativeadapter.File{readNative(t, "agy/testdata/probe.md", ".agents/agents/aok-probe/agent.md", "markdown")}},
		{name: "claude-code", target: "claude-code", scope: "project", hostStatus: "verified", adapter: claude.New(), files: []nativeadapter.File{{Path: ".claude/agents/reviewer.md", Format: "markdown", Data: []byte("---\nname: aok-reviewer\ndescription: Synthetic review agent\nmodel: sonnet\ncustomField: retained\n---\nReview only the supplied synthetic content.\n")}}},
		{name: "codex", target: "codex", scope: "project", hostStatus: "not-run", adapter: codex.New(), files: []nativeadapter.File{readNative(t, "codex/testdata/probe.toml", ".codex/agents/aok-probe.toml", "toml")}},
		{name: "codex-registered", target: "codex", scope: "project-registered", hostStatus: "verified", adapter: codex.New(), files: []nativeadapter.File{
			{Path: ".codex/config.toml", Format: "toml", Data: []byte("[agents.aok-probe]\ndescription = \"Synthetic read-only discovery fixture\"\nconfig_file = \"agents/aok-probe.toml\"\n")},
			readNative(t, "codex/testdata/probe.toml", ".codex/agents/aok-probe.toml", "toml"),
		}},
		{name: "cursor", target: "cursor", scope: "project", hostStatus: "verified", adapter: cursor.New(), files: []nativeadapter.File{readNative(t, "cursor/testdata/reviewer.md", ".cursor/agents/reviewer.md", "markdown")}},
		{name: "gemini", target: "gemini", scope: "project", hostStatus: "verified", adapter: gemini.New(), files: []nativeadapter.File{readNative(t, "gemini/testdata/security-auditor.md", ".gemini/agents/security-auditor.md", "markdown")}},
		{name: "hermes-agent", target: "hermes-agent", scope: "profile", hostStatus: "verified", adapter: hermes.New(), files: []nativeadapter.File{
			readNative(t, "hermes/testdata/config.yaml", "config.yaml", "yaml"),
			readNative(t, "hermes/testdata/SOUL.md", "SOUL.md", "markdown"),
			readNative(t, "hermes/testdata/profile.yaml", "profile.yaml", "yaml"),
		}},
		{name: "openclaw", target: "openclaw", scope: "isolated-home", hostStatus: "verified", adapter: openclaw.New(), files: []nativeadapter.File{
			{Path: ".openclaw/openclaw.json", Format: "json", Data: []byte(`{"agents":{"entries":{"main":{"identity":{"name":"Main"},"model":"openai/gpt-5.4-mini"}}}}`)},
			{Path: ".openclaw/workspace/AGENTS.md", Format: "markdown", Data: []byte("Synthetic isolated-home instruction content.\n")},
		}},
		{name: "opencode", target: "opencode", scope: "project", hostStatus: "verified", adapter: opencode.New(), files: []nativeadapter.File{readNative(t, "opencode/testdata/probe.md", ".opencode/agents/aok-probe.md", "markdown")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runAdapterConformance(t, tc) })
	}
}

func runAdapterConformance(t *testing.T, tc adapterCase) {
	t.Helper()
	descriptor := tc.adapter.Descriptor()
	scopeSupported := false
	for _, scope := range descriptor.Scopes {
		if scope == tc.scope {
			scopeSupported = true
		}
	}
	if !scopeSupported {
		t.Fatalf("descriptor does not declare fixture scope %q: %+v", tc.scope, descriptor.Scopes)
	}
	hostStatus := descriptor.HostLoading.Status
	if hostStatus == "verified" {
		scopeVerified := false
		for _, scope := range descriptor.HostLoading.Scopes {
			scopeVerified = scopeVerified || scope == tc.scope
		}
		if !scopeVerified {
			hostStatus = "not-run"
		}
	}
	if got := hostStatus; got != tc.hostStatus {
		t.Fatalf("descriptor host status=%q want=%q", got, tc.hostStatus)
	}
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(tc.adapter); err != nil {
		t.Fatalf("register: %v", err)
	}
	engine := nativeadapter.NewEngine(registry)
	controls := []nativeadapter.Control{{Path: "agent.permissions.network.mode", Required: true}}

	// Import and export remain incomplete even when syntax and projection are sound.
	bundle, imported := engine.Import(tc.target, tc.scope, tc.files, nil, nil)
	if imported.Status != "blocked" || bundle.Completeness != "draft-incomplete" || bundle.Digest == "" {
		t.Fatalf("import must remain an incomplete draft: report=%+v bundle=%+v", imported, bundle)
	}
	assertEngineBoundary(t, imported, tc.hostStatus)

	// Every adapter must surface an unsupported required control through the shared engine.
	blocked := engine.Inspect(tc.target, tc.scope, tc.files, controls)
	if blocked.Status != "blocked" {
		t.Fatalf("required network control did not block: %+v", blocked)
	}
	assertEngineBoundary(t, blocked, tc.hostStatus)
	if !hasDiagnostic(blocked, "native.control.required-unsupported") {
		t.Fatalf("missing required-control diagnostic: %+v", blocked.Diagnostics)
	}
	optional := engine.Inspect(tc.target, tc.scope, tc.files, []nativeadapter.Control{{Path: "agent.permissions.network.mode", Required: false}})
	if optional.Status != "valid" || !hasDiagnostic(optional, "native.control.optional-unsupported") {
		t.Fatalf("optional unsupported control should warn without claiming enforcement: %+v", optional)
	}
	assertEngineBoundary(t, optional, tc.hostStatus)

	outputs, rendered, exported := engine.Export(tc.target, tc.scope, tc.files, bundle, nil)
	if exported.Status != "blocked" || rendered.Digest == "" || len(outputs) == 0 {
		t.Fatalf("export must produce only an incomplete candidate: outputs=%d report=%+v", len(outputs), exported)
	}
	assertEngineBoundary(t, exported, tc.hostStatus)

	// Compare proves semantic compatibility separately from host loading.
	comparison := engine.Compare(tc.target, tc.scope, outputs, rendered, nil)
	if comparison.Status != "valid" || comparison.MappingStatus != "compatible" || len(comparison.Differences) != 0 {
		t.Fatalf("roundtrip changed the native projection: %+v", comparison)
	}
	assertEngineBoundary(t, comparison, tc.hostStatus)

	// Source drift and sidecar tampering must be detected for every file shape.
	tamperedFiles := cloneNativeFiles(outputs)
	tamperedFiles[0].Data = append(append([]byte(nil), tamperedFiles[0].Data...), '\n')
	drift := engine.Compare(tc.target, tc.scope, tamperedFiles, rendered, nil)
	if drift.Status != "invalid" || !hasDiagnostic(drift, "native.bundle.conflict") {
		t.Fatalf("source drift was not detected: %+v", drift)
	}

	corruptBundle := rendered
	corruptBundle.Digest = strings.Repeat("0", 64)
	tamper := engine.Compare(tc.target, tc.scope, outputs, corruptBundle, nil)
	if tamper.Status != "invalid" || !hasDiagnostic(tamper, "native.bundle.integrity") {
		t.Fatalf("bundle tampering was not detected: %+v", tamper)
	}
}

func assertEngineBoundary(t *testing.T, r nativeadapter.Report, host string) {
	t.Helper()
	if r.HostLoading.Status != host {
		t.Errorf("operation %s host status=%q want=%q", r.Operation, r.HostLoading.Status, host)
	}
	if r.RuntimeEnforcement != "unverified" {
		t.Errorf("operation %s overstated runtime enforcement: %q", r.Operation, r.RuntimeEnforcement)
	}
}

func readNative(t *testing.T, source, path, format string) nativeadapter.File {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read fixture %s: %v", source, err)
	}
	return nativeadapter.File{Path: path, Format: format, Data: data}
}

func hasDiagnostic(r nativeadapter.Report, code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}
func cloneNativeFiles(in []nativeadapter.File) []nativeadapter.File {
	out := make([]nativeadapter.File, len(in))
	for i, f := range in {
		out[i] = f
		out[i].Data = bytes.Clone(f.Data)
	}
	return out
}
