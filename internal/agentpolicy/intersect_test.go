package agentpolicy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func TestIntersectRestrictionsAreMonotoneAndDeterministic(t *testing.T) {
	base := policy(
		[]string{"reports", "workspace", "cache"}, []string{"reports", "cache"},
		"allowlist", []string{"github.com", "api.example.com"}, "explicit-authorization",
		[]contracts.ActionRule{
			rule("repository.read", "workspace", "allow", "executor"),
			rule("artifact.write", "reports", "require-authorization", "executor"),
			rule("agent.only", "workspace", "allow", "native"),
		},
	)
	restriction := policy(
		[]string{"workspace", "reports", "elsewhere"}, []string{"workspace"},
		"allowlist", []string{"unrelated.example", "api.example.com"}, "denied",
		[]contracts.ActionRule{
			rule("repository.read", "workspace", "require-authorization", "executor"),
			rule("restriction.only", "reports", "allow", "executor"), // absent from base: still denied
		},
	)
	baseBefore := marshal(t, base)
	restrictionBefore := marshal(t, restriction)

	got, diagnostics := Intersect(base, &restriction)
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if !reflect.DeepEqual(got.Filesystem.Read, []string{"reports", "workspace"}) {
		t.Fatalf("read intersection = %#v", got.Filesystem.Read)
	}
	if len(got.Filesystem.Write) != 0 {
		t.Fatalf("write intersection widened: %#v", got.Filesystem.Write)
	}
	if got.Network.Mode != "allowlist" || got.Network.Hosts == nil || !reflect.DeepEqual(*got.Network.Hosts, []string{"api.example.com"}) {
		t.Fatalf("network intersection = %#v", got.Network)
	}
	if got.Secrets.Mode != "references-only" || got.ExternalMutations.Mode != "denied" {
		t.Fatalf("mode intersection = %#v / %#v", got.Secrets, got.ExternalMutations)
	}
	if got.Actions.Default != "deny" {
		t.Fatalf("action default widened: %q", got.Actions.Default)
	}
	wantRules := []contracts.ActionRule{
		rule("agent.only", "workspace", "deny", "native"),
		rule("artifact.write", "reports", "deny", "executor"),
		rule("repository.read", "workspace", "require-authorization", "executor"),
		rule("restriction.only", "reports", "deny", "executor"),
	}
	if !reflect.DeepEqual(got.Actions.Rules, wantRules) {
		t.Fatalf("actions = %#v, want %#v", got.Actions.Rules, wantRules)
	}
	if !reflect.DeepEqual(baseBefore, marshal(t, base)) || !reflect.DeepEqual(restrictionBefore, marshal(t, restriction)) {
		t.Fatal("Intersect mutated an input policy")
	}

	// Set and rule ordering in source documents cannot change the effective result.
	base.Filesystem.Read = []string{"cache", "workspace", "reports"}
	base.Filesystem.Write = []string{"cache", "reports"}
	base.Network.Hosts = ptrStrings([]string{"api.example.com", "github.com"})
	base.Actions.Rules[0], base.Actions.Rules[2] = base.Actions.Rules[2], base.Actions.Rules[0]
	restriction.Filesystem.Read = []string{"elsewhere", "reports", "workspace"}
	restriction.Network.Hosts = ptrStrings([]string{"api.example.com", "unrelated.example"})
	reordered, diagnostics := Intersect(base, &restriction)
	if len(diagnostics) != 0 || !reflect.DeepEqual(got, reordered) {
		t.Fatalf("intersection depends on input order: %#v %#v", got, reordered)
	}
}

func TestIntersectNilRestrictionInheritsButEmptyRestrictionDenies(t *testing.T) {
	base := policy([]string{"workspace"}, []string{"reports"}, "allowlist", []string{"example.com"}, "explicit-authorization", []contracts.ActionRule{rule("read", "workspace", "allow", "native")})
	inherited, diagnostics := Intersect(base, nil)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if !reflect.DeepEqual(inherited.Filesystem.Read, []string{"workspace"}) || inherited.Network.Mode != "allowlist" || len(inherited.Actions.Rules) != 1 || inherited.Actions.Rules[0].Decision != "allow" {
		t.Fatalf("nil did not inherit: %#v", inherited)
	}

	empty := policy(nil, nil, "denied", nil, "denied", nil)
	denied, diagnostics := Intersect(base, &empty)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(denied.Filesystem.Read) != 0 || len(denied.Filesystem.Write) != 0 || denied.Network.Mode != "denied" {
		t.Fatalf("empty restriction did not deny: %#v", denied)
	}
	if len(denied.Actions.Rules) != 1 || denied.Actions.Rules[0].Decision != "deny" {
		t.Fatalf("missing restriction action did not deny: %#v", denied.Actions)
	}
}

func TestIntersectReportsEnforcementOwnerConflict(t *testing.T) {
	base := policy(nil, nil, "denied", nil, "denied", []contracts.ActionRule{rule("read", "workspace", "allow", "native")})
	restriction := policy(nil, nil, "denied", nil, "denied", []contracts.ActionRule{rule("read", "workspace", "deny", "executor")})
	got, diagnostics := Intersect(base, &restriction)
	if len(got.Actions.Rules) != 0 {
		t.Fatalf("conflicting policy returned a guessed result: %#v", got)
	}
	if !hasDiagnostic(diagnostics, "policy.actions.owner-conflict") {
		t.Fatalf("missing owner conflict diagnostic: %#v", diagnostics)
	}
}

func TestIntersectRejectsAmbiguousFilesystemAndHostSymbols(t *testing.T) {
	base := policy([]string{"*", "workspace/**", "repo/src"}, nil, "allowlist", []string{"*.example.com", "https://example.com", "example.com:443", "EXAMPLE.com"}, "denied", nil)
	got, diagnostics := Intersect(base)
	if len(got.Filesystem.Read) != 0 {
		t.Fatal("returned policy for ambiguous symbols")
	}
	if !hasDiagnostic(diagnostics, "policy.filesystem.symbol") || !hasDiagnostic(diagnostics, "policy.network.host") {
		t.Fatalf("missing ambiguity diagnostics: %#v", diagnostics)
	}
}

func TestIntersectAcceptsCanonicalIPHosts(t *testing.T) {
	base := policy(nil, nil, "allowlist", []string{"192.0.2.1", "2001:db8::1"}, "denied", nil)
	got, diagnostics := Intersect(base)
	if len(diagnostics) != 0 {
		t.Fatalf("canonical IPs rejected: %#v", diagnostics)
	}
	if got.Network.Hosts == nil || !reflect.DeepEqual(*got.Network.Hosts, []string{"192.0.2.1", "2001:db8::1"}) {
		t.Fatalf("canonical IPs changed: %#v", got.Network.Hosts)
	}
}

func TestExecutionProfileAcceptsSamePermissionShapeAsAssignment(t *testing.T) {
	data, err := osReadProfileFixture()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	permissionJSON := marshal(t, policy([]string{"workspace"}, []string{"reports"}, "denied", nil, "denied", nil))
	doc["permissionRestrictions"] = permissionJSON
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := contracts.Parse(encoded)
	if err != nil {
		t.Fatalf("profile with permissionRestrictions rejected: %v", err)
	}
	typed, err := contracts.DecodeTyped(parsed.(contracts.Resource))
	if err != nil {
		t.Fatal(err)
	}
	profile := typed.(contracts.ExecutionProfile)
	if profile.PermissionRestrictions == nil || !reflect.DeepEqual(profile.PermissionRestrictions.Filesystem.Read, []string{"workspace"}) {
		t.Fatalf("restriction not retained: %#v", profile.PermissionRestrictions)
	}

	bad := map[string]any{"filesystem": map[string]any{"read": []string{"workspace"}}}
	badJSON, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	doc["permissionRestrictions"] = badJSON
	encoded, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := contracts.Parse(encoded); err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("same closed permission schema was not applied: %v", err)
	}
}

func policy(read, write []string, networkMode string, hosts []string, externalMode string, rules []contracts.ActionRule) contracts.Permissions {
	if read == nil {
		read = []string{}
	}
	if write == nil {
		write = []string{}
	}
	if rules == nil {
		rules = []contracts.ActionRule{}
	}
	var hostPtr *[]string
	if hosts != nil {
		hostPtr = &hosts
	}
	return contracts.Permissions{
		Filesystem:        contracts.FilesystemPermissions{Read: read, Write: write},
		Network:           contracts.NetworkPermissions{Mode: networkMode, Hosts: hostPtr},
		Secrets:           contracts.ModePolicy{Mode: "references-only"},
		ExternalMutations: contracts.ModePolicy{Mode: externalMode},
		Actions:           contracts.ActionPermissions{Default: "deny", Rules: rules},
	}
}

func rule(action, resource, decision, owner string) contracts.ActionRule {
	return contracts.ActionRule{Action: action, Resource: resource, Decision: decision, EnforcementOwner: owner}
}
func ptrStrings(values []string) *[]string { return &values }
func marshal(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func hasDiagnostic(diagnostics []contracts.Diagnostic, code string) bool {
	for _, d := range diagnostics {
		if d.Code == code && d.Severity == "error" {
			return true
		}
	}
	return false
}
