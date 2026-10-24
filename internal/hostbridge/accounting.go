package hostbridge

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"time"
)

func withAccounting(b Bundle, p Prepared, policy Policy, t Transcript) Bundle {
	at := time.Now().UTC()
	ex, report, e := ExportUsage(p, policy, t, at)
	if e != nil || t.Model != "" && t.Model != p.Model {
		ex = usage.EmptyExport()
		report = usage.BaseReport()
		report.Status = "unknown"
	}
	b.Files["usage.json"] = encoded(ex)
	b.Files["usage-report.json"] = encoded(report)
	if b.Receipt != nil {
		b.Receipt.UsageSHA256 = Hash(b.Files["usage.json"])
		b.Receipt.Digest = receiptDigest(*b.Receipt)
		b.Files["receipt.json"] = encoded(*b.Receipt)
	}
	return b
}

// InvocationProbe is deliberately separate from task-quality evidence. It is
// emitted only for the bounded synthetic test profile by the qualifier.
func InvocationProbe(p Prepared, r Receipt, t Transcript, accountScope string) modelcatalog.ProbeEvidence {
	observations := []modelcatalog.ProbeObservation{}
	outcome := "malformed-output"
	if r.OutputValidated && t.HostOutcome == "completed" && t.Model == p.Model {
		outcome = "success"
		for _, pair := range []struct {
			k string
			v any
		}{{"selector", p.Model}, {"host", p.Host}, {"hostVersion", p.HostVersion}, {"structuredOutput", true}} {
			raw, _ := json.Marshal(pair.v)
			observations = append(observations, modelcatalog.ProbeObservation{Dimension: pair.k, Value: raw})
		}
	}
	return modelcatalog.ProbeEvidence{Version: modelcatalog.ProbeVersion, ID: r.AttemptID, Configuration: p.Configuration, ProfileSHA256: p.ProfileSHA256, ObservedAt: r.ObservedAt, AccountScope: accountScope, Outcome: outcome, Observations: observations, EvidenceSHA256: r.TranscriptSHA256}
}
