package sdk

import (
	"context"
	"encoding/json"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"path/filepath"
	"sort"
)

func setupOutputOptional(out string, roots ...string) error {
	if out == "" {
		return nil
	}
	return setupOutput(out, roots...)
}

type operationalBundle struct {
	Version string      `json:"version"`
	Kind    string      `json:"kind"`
	Assets  []api.Asset `json:"assets"`
}

func operationalReportFiles(version string, v any, inputs map[string][]byte) map[string][]byte {
	files := cloneBytes(inputs)
	files["report.json"] = engine.Encode(v)
	names := []string{}
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	assets := []api.Asset{}
	for _, n := range names {
		assets = append(assets, api.Asset{Path: n, Sha256: api.Sha(SHA256(files[n]))})
	}
	files["bundle.json"] = engine.Encode(operationalBundle{version, "OperationalReportBundle", assets})
	return files
}
func writeOperationalReport(ctx context.Context, out, version string, v any, files map[string][]byte) error {
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return e
	}
	if e = w.Write(ctx, operationalReportFiles(version, v, files)); e != nil {
		return e
	}
	return w.Sync(ctx)
}
func loadOperationalReport(ctx context.Context, root, version string) (map[string][]byte, error) {
	raw, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return nil, e
	}
	var b operationalBundle
	if DecodeRead(raw, &b) != nil || b.Version != version || b.Kind != "OperationalReportBundle" || len(b.Assets) > 1023 {
		return nil, errors.New("invalid operational report")
	}
	files := map[string][]byte{}
	total := len(raw)
	for _, a := range b.Assets {
		if a.Path == "bundle.json" {
			return nil, errors.New("circular report")
		}
		if _, ok := files[a.Path]; ok {
			return nil, errors.New("duplicate report asset")
		}
		p, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return nil, e
		}
		v, e := ReadLocal(ctx, p)
		if e != nil || SHA256(v) != string(a.Sha256) {
			return nil, errors.New("changed operational asset")
		}
		total += len(v)
		if total > engine.MaxTotalBytes {
			return nil, errors.New("report bound")
		}
		files[a.Path] = v
	}
	if files["report.json"] == nil {
		return nil, errors.New("missing report")
	}
	return files, nil
}

// ProjectOperationReceipt preserves the full journal observation in an extension
// while reusing the original domain receipt. It grants no send/acceptance authority.
func ProjectOperationReceipt(observation o.Observation, planSHA256 string) (api.OperationReceipt, error) {
	status, effect := "unknown", "possible"
	switch observation.State {
	case "prepared", "authorized", "planned-not-admitted":
		status, effect = "queued", "none"
	case "blocked", "cancelled":
		status, effect = "blocked", "none"
	case "dispatch-intent":
		status = "dispatch-intent"
	case "response-observed", "readback-observed":
		status = "observed"
	case "readback-verified":
		status, effect = "confirmed", "confirmed"
	case "rejected":
		status, effect = "rejected", "none"
	}
	assets := append([]api.Asset{}, observation.Evidence...)
	readback := []api.Asset{}
	if observation.State == "readback-verified" || observation.State == "readback-observed" {
		readback = append(readback, assets...)
	}
	receipt := api.OperationReceipt{APIVersion: api.Version, Kind: "OperationReceipt", Metadata: api.Metadata{Id: "receipt-" + SHA256(engine.Encode(struct{ ID, Plan, Observation string }{observation.OperationID, planSHA256, SHA256(engine.Encode(observation))}))[:40], Revision: 1}, Spec: api.OperationReceiptSpec{OperationId: api.Id(observation.OperationID), PlanSha256: api.Sha(planSHA256), Status: status, Effects: effect, ObservedAt: api.Time(observation.ObservedAt), DispatchEvidence: assets, ReadbackEvidence: readback}, Extensions: map[string]json.RawMessage{"workkit/observation": engine.Encode(observation)}}
	if _, err := Parse(engine.Encode(receipt), "json"); err != nil {
		return receipt, err
	}
	return receipt, nil
}
