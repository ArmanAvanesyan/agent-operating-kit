package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"regexp"
)

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func DefaultLimits() Limits                    { return readsource.DefaultLimits() }
func EffectiveLimits(s Source) (Limits, error) { return readsource.EffectiveLimits(s) }
func ValidateSource(s Source) error            { return readsource.ValidateSource(s) }
