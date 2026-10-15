package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

type cliNativeFixture struct{}

func (cliNativeFixture) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: "opencode", AdapterVersion: "0.1.0", Scopes: []string{"project"},
		Fields:      []nativeadapter.FieldCapability{{NeutralPath: "metadata.name", NativePath: "name", Support: "native", EnforcementOwner: "native"}},
		HostLoading: nativeadapter.HostLoadingEvidence{Status: "not-run"},
	}
}
func (cliNativeFixture) Decode(_ string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	value, err := nativeparse.Decode("json", files[0].Data)
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	doc := nativeadapter.Document{Fields: map[string]json.RawMessage{}, Unknown: map[string]json.RawMessage{}}
	for k, v := range value {
		b, _ := json.Marshal(v)
		if k == "name" {
			doc.Fields["metadata.name"] = b
		} else {
			doc.Unknown[k] = b
		}
	}
	return doc, nil, nil
}
func (cliNativeFixture) Encode(_ string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	value := map[string]json.RawMessage{}
	for k, v := range doc.Unknown {
		value[k] = v
	}
	value["name"] = doc.Fields["metadata.name"]
	b, err := json.Marshal(value)
	return []nativeadapter.File{{Path: "agent.json", Format: "json", Data: b}}, nil, err
}
func TestNativeCLIMisuseAndMissingAdapter(t *testing.T) {
	for _, args := range [][]string{
		{}, {"inspect"}, {"import", "--target", "opencode", "--scope", "project", "--input", "x"},
		{"export", "--target", "opencode", "--scope", "project", "--input", "x", "--out", "out"},
		{"inspect", "--target", "made-up", "--scope", "project", "--input", "x"},
	} {
		var out, errout bytes.Buffer
		code, err := runNative(args, &out, &errout)
		if err != nil || code != 2 {
			t.Fatalf("%v: %d %v", args, code, err)
		}
	}
	root := t.TempDir()
	input := filepath.Join(root, "agent.json")
	_ = os.WriteFile(input, []byte("{}"), 0600)
	var out, errout bytes.Buffer
	code, err := runNative([]string{"inspect", "--target", "opencode", "--scope", "project", "--input", input}, &out, &errout)
	if err != nil || code != 1 || !bytes.Contains(out.Bytes(), []byte("blocked")) {
		t.Fatalf("%d %v %s", code, err, out.String())
	}
}
func TestNativeCLIExplicitDraftRoundTripAndNoOverwrite(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "agent.json")
	_ = os.WriteFile(source, []byte(`{"name":"fixture","experimental":{"preserve":true}}`), 0600)
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(cliNativeFixture{}); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errout bytes.Buffer
		code, err := runNativeWithEngine(args, &out, &errout, engine)
		if err != nil {
			t.Fatal(err)
		}
		return code, out.String()
	}
	base := []string{"--target", "opencode", "--scope", "project"}
	args := func(op string, extra ...string) []string { return append(append([]string{op}, base...), extra...) }
	out := filepath.Join(root, "draft")
	code, report := run(args("import", "--input", source, "--out", out)...)
	if code != 1 {
		t.Fatalf("incomplete draft should block: %d %s", code, report)
	}
	bundle := filepath.Join(out, "aok-native.json")
	if _, err := os.Stat(bundle); err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(root, "exported")
	code, report = run(args("export", "--input", bundle, "--source", source, "--out", exported)...)
	if code != 1 {
		t.Fatalf("export draft should block: %d %s", code, report)
	}
	if _, err := os.Stat(filepath.Join(exported, "agent.json")); err != nil {
		t.Fatal(err)
	}
	code, report = run(args("compare", "--input", filepath.Join(exported, "agent.json"), "--against", filepath.Join(exported, "aok-native.json"))...)
	if code != 0 {
		t.Fatalf("semantic roundtrip failed: %d %s", code, report)
	}
	before, _ := os.ReadFile(bundle)
	code, _ = run(args("import", "--input", source, "--out", out)...)
	after, _ := os.ReadFile(bundle)
	if code != 1 || !bytes.Equal(before, after) {
		t.Fatal("existing output changed")
	}
}
