package tracker

import "fmt"

// ReadPolicy is supplied separately by the operator; a task/source document
// cannot choose both a credential and an arbitrary destination on its own.
type ReadRule struct {
	Deployment         *Deployment `json:"deployment,omitempty"`
	Provider           string      `json:"provider"`
	Origin             string      `json:"origin"`
	NativeTenantID     string      `json:"nativeTenantId"`
	ContainerID        string      `json:"containerId"`
	ContainerKey       string      `json:"containerKey"`
	Auth               Auth        `json:"auth"`
	PrincipalPseudonym string      `json:"principalPseudonym"`
}
type ReadPolicy struct {
	Version string     `json:"version"`
	Rules   []ReadRule `json:"rules"`
	digest  string
	seal    string
}

func ReadReadPolicy(path string) (*ReadPolicy, error) {
	b, e := ReadFile(path)
	if e != nil {
		return nil, e
	}
	var p ReadPolicy
	if e = Decode(b, "read-policy", &p); e != nil {
		return nil, e
	}
	if (p.Version != Version && p.Version != Version2) || len(p.Rules) == 0 || len(p.Rules) > 100 {
		return nil, fmt.Errorf("invalid operator read policy")
	}
	p.digest = Hash(b)
	p.seal = Digest(p)
	return &p, nil
}
func policyAllows(p *ReadPolicy, s Source) bool {
	if p == nil || p.Version != s.Version || (p.seal != "" && p.seal != Digest(p)) || s.ReadPolicySHA256 != "" && s.ReadPolicySHA256 != ReadPolicyHash(p) {
		return false
	}
	for _, r := range p.Rules {
		if Digest(r.Deployment) == Digest(s.Deployment) && r.Provider == s.Provider && r.Origin == s.Origin && r.NativeTenantID == s.NativeTenantID && r.ContainerID == s.ContainerID && r.ContainerKey == s.ContainerKey && Digest(r.Auth) == Digest(s.Auth) && r.PrincipalPseudonym == s.PrincipalPseudonym {
			return true
		}
	}
	return false
}
func ReadPolicyHash(p *ReadPolicy) string {
	if p == nil {
		return ""
	}
	if p.digest != "" {
		return p.digest
	}
	return Digest(p)
}
