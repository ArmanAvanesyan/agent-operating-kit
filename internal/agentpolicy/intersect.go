// Package agentpolicy computes declared permission intersections. It does not
// enforce permissions at runtime or establish that a host can enforce them.
package agentpolicy

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

var scopeSymbol = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type actionKey struct{ action, resource string }

// Intersect intersects agent permissions with each non-nil restriction.
// A nil restriction inherits the current policy. Filesystem scopes and network
// hosts use exact symbolic set intersection. Action permissions default to
// deny; every policy must explicitly contain an action/resource key for that
// key to remain usable. Results and diagnostics are returned in stable order.
// The input values and their slices are never modified.
func Intersect(agent contracts.Permissions, restrictions ...*contracts.Permissions) (contracts.Permissions, []contracts.Diagnostic) {
	policies := []contracts.Permissions{agent}
	for _, restriction := range restrictions {
		if restriction != nil {
			policies = append(policies, *restriction)
		}
	}
	var diagnostics []contracts.Diagnostic
	for i, p := range policies {
		source := "agent"
		if i > 0 {
			source = fmt.Sprintf("restriction[%d]", i-1)
		}
		validatePolicy(p, source, &diagnostics)
	}
	if hasErrors(diagnostics) {
		return contracts.Permissions{}, sortedDiagnostics(diagnostics)
	}

	result := contracts.Permissions{
		Filesystem: contracts.FilesystemPermissions{
			Read:  intersectStringSets(fieldSet(policies, func(p contracts.Permissions) []string { return p.Filesystem.Read })),
			Write: intersectStringSets(fieldSet(policies, func(p contracts.Permissions) []string { return p.Filesystem.Write })),
		},
		Network:           intersectNetwork(policies),
		Secrets:           intersectSecrets(policies),
		ExternalMutations: intersectExternalMutations(policies),
	}
	result.Actions, diagnostics = intersectActions(policies, diagnostics)
	if hasErrors(diagnostics) {
		return contracts.Permissions{}, sortedDiagnostics(diagnostics)
	}
	return result, sortedDiagnostics(diagnostics)
}

func validatePolicy(p contracts.Permissions, source string, diagnostics *[]contracts.Diagnostic) {
	for field, values := range map[string][]string{"filesystem.read": p.Filesystem.Read, "filesystem.write": p.Filesystem.Write} {
		for _, value := range values {
			if !validScope(value) {
				addError(diagnostics, "policy.filesystem.symbol", source, field, fmt.Sprintf("filesystem scope %q is not an exact safe symbol", value))
			}
		}
	}
	switch p.Network.Mode {
	case "denied", "allowlist":
	default:
		addError(diagnostics, "policy.network.mode", source, "network.mode", fmt.Sprintf("unsupported network mode %q", p.Network.Mode))
	}
	for _, host := range derefStrings(p.Network.Hosts) {
		if !validHost(host) {
			addError(diagnostics, "policy.network.host", source, "network.hosts", fmt.Sprintf("host %q is not a canonical exact host", host))
		}
	}
	if p.Secrets.Mode != "references-only" {
		addError(diagnostics, "policy.secrets.mode", source, "secrets.mode", fmt.Sprintf("unsupported secret mode %q", p.Secrets.Mode))
	}
	if p.ExternalMutations.Mode != "denied" && p.ExternalMutations.Mode != "explicit-authorization" {
		addError(diagnostics, "policy.external-mutations.mode", source, "externalMutations.mode", fmt.Sprintf("unsupported external-mutation mode %q", p.ExternalMutations.Mode))
	}
	if p.Actions.Default != "deny" {
		addError(diagnostics, "policy.actions.default", source, "actions.default", "action policy default must be deny")
	}
	for _, rule := range p.Actions.Rules {
		if rule.Action == "" || rule.Resource == "" {
			addError(diagnostics, "policy.actions.key", source, "actions.rules", "action and resource must be non-empty")
		}
		if rule.Decision != "allow" && rule.Decision != "require-authorization" && rule.Decision != "deny" {
			addError(diagnostics, "policy.actions.decision", source, "actions.rules.decision", fmt.Sprintf("unsupported action decision %q", rule.Decision))
		}
		if rule.EnforcementOwner != "native" && rule.EnforcementOwner != "executor" {
			addError(diagnostics, "policy.actions.owner", source, "actions.rules.enforcementOwner", fmt.Sprintf("unsupported enforcement owner %q", rule.EnforcementOwner))
		}
	}
}

func validScope(value string) bool {
	// Paths, globs, dot segments and separators are deliberately outside this
	// symbolic contract. workspace and reports are conventional reserved symbols.
	return value == "workspace" || value == "reports" || scopeSymbol.MatchString(value)
}

func validHost(host string) bool {
	if host == "" || strings.TrimSpace(host) != host {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String() == host
	}
	if strings.ContainsAny(host, "*/\\:@?#%") {
		return false
	}
	if len(host) > 253 || strings.ToLower(host) != host || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func fieldSet(policies []contracts.Permissions, pick func(contracts.Permissions) []string) [][]string {
	sets := make([][]string, len(policies))
	for i, p := range policies {
		sets[i] = pick(p)
	}
	return sets
}

func intersectStringSets(sets [][]string) []string {
	if len(sets) == 0 {
		return []string{}
	}
	current := stringSet(sets[0])
	for _, values := range sets[1:] {
		next := stringSet(values)
		for value := range current {
			if _, ok := next[value]; !ok {
				delete(current, value)
			}
		}
	}
	result := make([]string, 0, len(current))
	for value := range current {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}
func derefStrings(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

func intersectNetwork(policies []contracts.Permissions) contracts.NetworkPermissions {
	var sets [][]string
	denied := false
	for _, p := range policies {
		if p.Network.Mode == "denied" {
			denied = true
		}
		sets = append(sets, derefStrings(p.Network.Hosts))
	}
	if denied {
		return contracts.NetworkPermissions{Mode: "denied"}
	}
	hosts := intersectStringSets(sets)
	return contracts.NetworkPermissions{Mode: "allowlist", Hosts: &hosts}
}

func intersectSecrets(policies []contracts.Permissions) contracts.ModePolicy {
	// references-only is the only supported secret mode and therefore the
	// strongest common representable policy.
	return contracts.ModePolicy{Mode: "references-only"}
}

func intersectExternalMutations(policies []contracts.Permissions) contracts.ModePolicy {
	for _, p := range policies {
		if p.ExternalMutations.Mode == "denied" {
			return contracts.ModePolicy{Mode: "denied"}
		}
	}
	return contracts.ModePolicy{Mode: "explicit-authorization"}
}

func intersectActions(policies []contracts.Permissions, diagnostics []contracts.Diagnostic) (contracts.ActionPermissions, []contracts.Diagnostic) {
	perPolicy := make([]map[actionKey]contracts.ActionRule, len(policies))
	allKeys := map[actionKey]struct{}{}
	for i, p := range policies {
		perPolicy[i] = make(map[actionKey]contracts.ActionRule)
		for _, rule := range p.Actions.Rules {
			key := actionKey{rule.Action, rule.Resource}
			if previous, exists := perPolicy[i][key]; exists {
				if previous.EnforcementOwner != rule.EnforcementOwner {
					addError(&diagnostics, "policy.actions.owner-conflict", policySource(i), "actions.rules.enforcementOwner", fmt.Sprintf("conflicting enforcement owners for %q on %q", rule.Action, rule.Resource))
				}
				if decisionRank(rule.Decision) > decisionRank(previous.Decision) {
					previous.Decision = rule.Decision
				}
				perPolicy[i][key] = previous
			} else {
				perPolicy[i][key] = rule
			}
			allKeys[key] = struct{}{}
		}
	}
	keys := make([]actionKey, 0, len(allKeys))
	for key := range allKeys {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].action == keys[j].action {
			return keys[i].resource < keys[j].resource
		}
		return keys[i].action < keys[j].action
	})
	result := contracts.ActionPermissions{Default: "deny", Rules: make([]contracts.ActionRule, 0, len(keys))}
	for _, key := range keys {
		decision := "allow"
		owner := ""
		missing := false
		for i, rules := range perPolicy {
			rule, ok := rules[key]
			if !ok {
				missing = true
				continue
			}
			if owner == "" {
				owner = rule.EnforcementOwner
			} else if owner != rule.EnforcementOwner {
				addError(&diagnostics, "policy.actions.owner-conflict", policySource(i), "actions.rules.enforcementOwner", fmt.Sprintf("conflicting enforcement owners for %q on %q", key.action, key.resource))
			}
			if decisionRank(rule.Decision) > decisionRank(decision) {
				decision = rule.Decision
			}
		}
		if missing {
			decision = "deny"
		}
		result.Rules = append(result.Rules, contracts.ActionRule{Action: key.action, Resource: key.resource, Decision: decision, EnforcementOwner: owner})
	}
	return result, diagnostics
}

func policySource(index int) string {
	if index == 0 {
		return "agent"
	}
	return fmt.Sprintf("restriction[%d]", index-1)
}
func decisionRank(value string) int {
	switch value {
	case "allow":
		return 0
	case "require-authorization":
		return 1
	case "deny":
		return 2
	default:
		return 3
	}
}
func addError(ds *[]contracts.Diagnostic, code, source, field, message string) {
	*ds = append(*ds, contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: "error", Message: message})
}
func hasErrors(ds []contracts.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}
func sortedDiagnostics(ds []contracts.Diagnostic) []contracts.Diagnostic {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	return ds
}
