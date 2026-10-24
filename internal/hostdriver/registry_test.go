package hostdriver

import (
	"context"
	"strings"
	"testing"
)

func TestRegistryIsStableAndUnsupportedTargetsFailClosed(t *testing.T) {
	a, b := List(), List()
	if len(a) != 9 || len(a) != len(b) {
		t.Fatalf("registry size: %d", len(a))
	}
	for i := range a {
		if a[i] != b[i] || a[i].Digest == "" {
			t.Fatalf("unstable descriptor %q", a[i].ID)
		}
	}
	for _, id := range []string{"agy", "hermes-agent", "codex-cloud"} {
		d, ok := Lookup(id)
		if !ok || d.Support != Unsupported {
			t.Fatalf("%s not explicitly unsupported", id)
		}
		if _, err := Prepare(Assignment{Host: id}, Policy{Host: id}); err == nil {
			t.Fatalf("%s prepared", id)
		}
	}
}

type probe struct{ called bool }

func (p *probe) Version(context.Context, string) (string, error) {
	p.called = true
	return "1.2.3", nil
}
func TestDiscoverIsInjectedAndBounded(t *testing.T) {
	p := &probe{}
	d, e := Discover(context.Background(), "cursor", "/fake/cursor-agent", p)
	if e != nil || !p.called || d.Status != "discovered" || d.Version != "1.2.3" {
		t.Fatalf("%+v %v", d, e)
	}
}
func TestPrepareCapsExecutionAndDisablesDelegation(t *testing.T) {
	h := strings.Repeat("a", 64)
	a := Assignment{ID: "x", Host: "cursor", Workspace: "/repo", Model: "m", Input: []byte(`{"task":"x"}`)}
	p := Policy{Host: "cursor", HostVersion: "1", Executable: "/tmp/cursor", ExecutableSHA256: h, AuthMode: "native-login", AccountScope: "pool", Workspace: "/repo", Model: "m", ProfileSHA256: h, ConfigurationSHA256: h, MaxSeconds: 60, MaxBytes: 1024}
	r, e := Prepare(a, p)
	if e != nil || r.AllowDelegation || r.MaxSeconds != 60 || r.Digest == "" {
		t.Fatalf("%+v %v", r, e)
	}
	if !VerifyPrepared(r) {
		t.Fatal("valid prepared digest did not verify")
	}
	r.Model = "tampered"
	if VerifyPrepared(r) {
		t.Fatal("tampered prepared artifact verified")
	}
	r.Model = "m"
	p.MaxSeconds = 61
	if _, e = Prepare(a, p); e == nil {
		t.Fatal("unbounded duration accepted")
	}
}

func TestBoundPolicyExposesImmutableDriverIdentity(t *testing.T) {
	h := strings.Repeat("b", 64)
	p := Policy{Host: "gemini", HostVersion: "v", Executable: "/bin/gemini", ExecutableSHA256: h, AuthMode: "native-login", AccountScope: "pool", Workspace: "/w", Model: "m", ProfileSHA256: h, ConfigurationSHA256: h, MaxSeconds: 5, MaxBytes: 5}
	b, err := BindPolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := Lookup("gemini")
	if b.ImmutableDriverSHA() != d.Digest || b.CurrentPolicy() != p {
		t.Fatal("policy binding lost exact identity")
	}
}
func TestArgumentsRefuseRawOptionShapedPrompts(t *testing.T) {
	for _, host := range []string{"opencode", "cursor", "gemini", "openclaw"} {
		for _, prompt := range []string{"--auto", "--force", "--model other", "--config-override {}"} {
			if _, e := Arguments(host, "/workspace", "exact-model", prompt); e == nil {
				t.Fatal("raw option prompt accepted", host, prompt)
			}
		}
		args, e := Arguments(host, "/workspace", "exact-model", `{"instructions":"--force --model other","task":{"body":"--auto"}}`)
		if e != nil || len(args) == 0 {
			t.Fatal(host, e)
		}
	}
}
