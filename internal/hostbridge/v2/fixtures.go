package v2

import (
	"errors"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
)

const FixtureQualificationVersion = "aok-host-fixture-qualification/v1"

// FixtureQualification reports parser compatibility only. It conveys no
// executable, account, model, runtime, readiness, or acceptance qualification.
type FixtureQualification struct {
	Version              string `json:"version"`
	Host                 string `json:"host"`
	DriverSHA256         string `json:"driverSha256"`
	Protocol             string `json:"protocol"`
	Status               string `json:"status"`
	Frames               int    `json:"frames"`
	Events               int    `json:"events"`
	TerminalObserved     bool   `json:"terminalObserved"`
	LiveQualification    string `json:"liveQualification"`
	AuthorizationGranted bool   `json:"authorizationGranted"`
}

// QualifyV2Fixtures replays bounded protocol fixtures for any registered
// protocol without invoking a host, model, credential, or subprocess.
func QualifyV2Fixtures(host string, frames [][]byte) (FixtureQualification, error) {
	out := FixtureQualification{Version: FixtureQualificationVersion, Host: host, Status: "blocked", LiveQualification: "pending", AuthorizationGranted: false}
	d, ok := hostdriver.Lookup(host)
	if !ok || d.Support != hostdriver.Supported || d.Replay != "fixture" || len(frames) == 0 {
		return out, errors.New("registered fixture-supported driver and nonempty fixtures required")
	}
	o, err := ReplayScoped(host, "fixture-run", "fixture-attempt", frames)
	if err != nil {
		return out, err
	}
	out.DriverSHA256, out.Protocol, out.Frames, out.Events = d.Digest, d.Protocol, len(frames), len(o.Events)
	out.TerminalObserved = o.Outcome.Status == "observed"
	if out.Events == 0 {
		return out, errors.New("fixture set yielded no protocol events")
	}
	out.Status = "fixtures-parsed"
	return out, nil
}

// DecodeFixtureQualification is strict for persisted local fixture reports.
func DecodeFixtureQualification(raw []byte) (FixtureQualification, error) {
	var out FixtureQualification
	if err := DecodeStrict(raw, &out); err != nil {
		return FixtureQualification{}, err
	}
	if out.Version != FixtureQualificationVersion || out.Status != "fixtures-parsed" || out.LiveQualification != "pending" || out.AuthorizationGranted || out.Host == "" || out.Events < 1 || out.Frames < 1 || out.DriverSHA256 == "" || out.Protocol == "" {
		return FixtureQualification{}, errors.New("invalid non-authorizing fixture qualification")
	}
	return out, nil
}
