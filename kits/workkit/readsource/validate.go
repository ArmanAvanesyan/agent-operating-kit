// Read-side extraction from AOK; legacy formats intentionally retain their identities.
package readsource

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func DefaultLimits() Limits { return Limits{30, 300, 100, 10000, 4 << 20, 64 << 20} }
func EffectiveLimits(s Source) (Limits, error) {
	l := DefaultLimits()
	if s.Limits == nil {
		return l, nil
	}
	x := *s.Limits
	values := []struct{ v, max int }{{x.RequestSeconds, l.RequestSeconds}, {x.CommandSeconds, l.CommandSeconds}, {x.Requests, l.Requests}, {x.Items, l.Items}, {x.ResponseBytes, l.ResponseBytes}, {x.TotalBytes, l.TotalBytes}}
	for _, v := range values {
		if v.v < 1 || v.v > v.max {
			return l, fmt.Errorf("limits may shorten defaults only")
		}
	}
	return x, nil
}
func ValidateSource(s Source) error {
	if s.Version != Version && s.Version != Version2 {
		return fmt.Errorf("unsupported tracker version")
	}
	if s.Version == Version2 {
		if s.Provider != "jira-datacenter" || s.Deployment == nil || s.APIVersion != "2" || s.Deployment.ServerVersion == "" || !hashPattern.MatchString(s.Deployment.TLSTrustSHA256) {
			return fmt.Errorf("Data Center requires API 2 and pinned deployment/version/TLS evidence")
		}
		base := s.Deployment.BasePath
		if base != "" && (base[0] != '/' || strings.HasSuffix(base, "/") || strings.Contains(base, "..") || strings.ContainsAny(base, "%?\\#")) {
			return fmt.Errorf("unsafe Data Center base path")
		}
	} else if s.Deployment != nil || s.Provider == "jira-datacenter" {
		return fmt.Errorf("Data Center requires tracker v2")
	}
	u, e := url.Parse(s.Origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || strings.Contains(u.Host, "\\") {
		return fmt.Errorf("origin must be a fixed HTTPS origin without credentials, path or query")
	}
	if s.NativeTenantID == "" || s.ContainerID == "" || s.PrincipalPseudonym == "" || s.MappingVersion == "" {
		return fmt.Errorf("explicit native tenant/container/principal/mapping identities required")
	}
	if len(s.Scope.ItemIDs) > 10000 {
		return fmt.Errorf("too many explicit IDs")
	}
	for _, id := range s.Scope.ItemIDs {
		if !safeID.MatchString(id) {
			return fmt.Errorf("unsafe item ID")
		}
	}
	modes := map[string][]string{"github": {"gh-session"}, "azure-devops": {"bearer", "pat"}, "linear": {"bearer", "api-key"}, "jira": {"bearer", "basic"}, "jira-datacenter": {"bearer", "basic"}}
	allowed, ok := modes[s.Provider]
	if !ok {
		return fmt.Errorf("unsupported provider")
	}
	valid := false
	for _, m := range allowed {
		if s.Auth.Mode == m {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unsupported provider auth mode")
	}
	if s.Provider == "github" {
		if s.Auth.TokenEnv != "" || s.Auth.UsernameEnv != "" {
			return fmt.Errorf("GitHub credentials remain in gh session")
		}
	} else if !envName.MatchString(s.Auth.TokenEnv) {
		return fmt.Errorf("token environment reference required")
	}
	if s.Auth.Mode == "basic" && !envName.MatchString(s.Auth.UsernameEnv) {
		return fmt.Errorf("username environment reference required")
	}
	if s.Auth.UsernameEnv != "" && s.Auth.Mode != "basic" {
		return fmt.Errorf("username reference only applies to basic auth")
	}
	if s.APIVersion == "" {
		return fmt.Errorf("pinned API version required")
	}
	_, e = EffectiveLimits(s)
	return e
}
