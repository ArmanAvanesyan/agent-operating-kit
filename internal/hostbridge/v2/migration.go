package v2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// MigrationEnvelope carries an untouched v1 record alongside v2 evidence.
// Migration never activates execution or drops an unresolved obligation.
type MigrationEnvelope struct {
	Version               string          `json:"version"`
	SourceVersion         string          `json:"sourceVersion"`
	CapturedAt            string          `json:"capturedAt"`
	LegacySHA256          string          `json:"legacySha256"`
	LegacyRaw             json.RawMessage `json:"legacyRaw"`
	UnresolvedObligations []string        `json:"unresolvedObligations"`
	Activation            string          `json:"activation"`
}

func PreserveV1(raw []byte, capturedAt time.Time, obligations []string) (MigrationEnvelope, error) {
	var zero MigrationEnvelope
	if len(raw) == 0 || len(raw) > MaxFrameBytes || !json.Valid(raw) || capturedAt.IsZero() {
		return zero, errors.New("invalid v1 migration input")
	}
	if _, err := time.Parse(time.RFC3339Nano, capturedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return zero, err
	}
	copyObligations := append([]string(nil), obligations...)
	for _, o := range copyObligations {
		if o == "" {
			return zero, errors.New("empty unresolved obligation")
		}
	}
	sum := sha256.Sum256(raw)
	return MigrationEnvelope{Version: "aok-host-migration/v2", SourceVersion: "v1", CapturedAt: capturedAt.UTC().Format(time.RFC3339Nano), LegacySHA256: hex.EncodeToString(sum[:]), LegacyRaw: append(json.RawMessage(nil), raw...), UnresolvedObligations: copyObligations, Activation: "disabled"}, nil
}

func ValidateMigration(m MigrationEnvelope) error {
	if m.Version != "aok-host-migration/v2" || m.SourceVersion != "v1" || m.Activation != "disabled" || !json.Valid(m.LegacyRaw) || m.LegacySHA256 == "" || len(m.UnresolvedObligations) > 1000 {
		return errors.New("invalid migration envelope or activation state")
	}
	sum := sha256.Sum256(m.LegacyRaw)
	if hex.EncodeToString(sum[:]) != m.LegacySHA256 {
		return errors.New("legacy bytes changed")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.CapturedAt); err != nil {
		return errors.New("invalid migration capture time")
	}
	for _, o := range m.UnresolvedObligations {
		if o == "" {
			return errors.New("empty unresolved obligation")
		}
	}
	return nil
}
