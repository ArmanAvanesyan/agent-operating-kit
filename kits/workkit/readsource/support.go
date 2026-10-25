package readsource

type TransportError struct{ Disposition, Message string }

func (e *TransportError) Error() string { return e.Message }
func has(v []string, s string) bool {
	for _, a := range v {
		if a == s {
			return true
		}
	}
	return false
}

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
}
