package enrollment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/authority"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/bootstrap"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type draft struct {
	Version      string `json:"version"`
	Executable   bool   `json:"executable"`
	Kind         string `json:"kind"`
	Instructions string `json:"instructions"`
	Value        any    `json:"value"`
}

const draftInstruction = "DRAFT ONLY. Replace unknown values through the external operator process, then independently validate and sign the exact resulting payload. This file grants no authority and is invalid for execution."

func encode(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func addDraft(files map[string][]byte, path, kind string, value any) error {
	b, err := encode(draft{Version: Version, Executable: false, Kind: kind, Instructions: draftInstruction, Value: value})
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	files[path] = b
	return nil
}

// Files returns deterministic offline enrollment material. Empty fields are
// intentional: the generator never invents identities, evidence, credentials,
// timestamps, signatures, hashes, or key material.
func Files() (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, n := range Requirements() {
		if n.Check == "bootstrap" {
			c := bootstrap.Config{Version: bootstrap.ConfigVersion, Service: serviceFor(n.ID), SecretEnv: map[string]string{}}
			if n.ID == "anchor-config" {
				c.Anchor = &bootstrap.AnchorServiceConfig{}
			}
			if n.ID == "verifier-config" {
				c.Verifier = &bootstrap.VerifierConfig{}
			}
			if n.ID == "authority-config" {
				c.Authority = &bootstrap.AuthorityConfig{}
			}
			if n.ID == "qualify-github-read" {
				c.Qualification = &bootstrap.QualificationRecords{Version: bootstrap.QualificationVersion, Purpose: "synthetic-conformance-only", Action: "github-read", GitHubRead: &bootstrap.GitHubReadConfig{}}
			}
			if n.ID == "qualify-codex-quota" {
				c.Qualification = &bootstrap.QualificationRecords{Version: bootstrap.QualificationVersion, Purpose: "synthetic-conformance-only", Action: "codex-quota", CodexQuota: &bootstrap.CodexQuotaConfig{}}
			}
			if n.ID == "qualify-host" {
				c.Qualification = &bootstrap.QualificationRecords{Version: bootstrap.QualificationVersion, Purpose: "synthetic-conformance-only", Action: "host-conformance", Host: &bootstrap.HostConformance{}}
			}
			if n.ID == "qualify-verifier" {
				c.Qualification = &bootstrap.QualificationRecords{Version: bootstrap.QualificationVersion, Purpose: "synthetic-conformance-only", Action: "verifier-conformance", Verifier: &bootstrap.VerifierConformance{}}
			}
			if n.ID == "qualify-comment" {
				c.Qualification = &bootstrap.QualificationRecords{Version: bootstrap.QualificationVersion, Purpose: "synthetic-conformance-only", Action: "github-comment", GitHubWrite: &bootstrap.GitHubWriteRecords{}}
			}
			if n.ID == "calibration-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "calibration", Calibration: &bootstrap.CalibrationRecords{}}
			}
			if n.ID == "experiment-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "experiment", Experiment: &bootstrap.ExperimentRecords{}}
			}
			if n.ID == "verify-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "verify-candidate", PostRun: &bootstrap.PostRunRecords{Stage: "verify-candidate"}}
			}
			if n.ID == "accept-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "accept-candidate", PostRun: &bootstrap.PostRunRecords{Stage: "accept-candidate"}}
			}
			if n.ID == "handoff-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "handoff-candidate", PostRun: &bootstrap.PostRunRecords{Stage: "handoff-candidate"}}
			}
			if n.ID == "dispatch-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "github-dispatch", GitHubWrite: &bootstrap.GitHubWriteRecords{}}
			}
			if n.ID == "reconcile-worker" {
				c.Worker = &bootstrap.WorkerConfig{Mode: "github-reconcile", GitHubWrite: &bootstrap.GitHubWriteRecords{}}
			}
			if err := addDraft(files, "drafts/"+n.ID+".json", n.Contract, c); err != nil {
				return nil, err
			}
		}
	}
	additional := []struct {
		path, kind string
		value      any
	}{
		{"drafts/anchor-service.json", "anchor service bootstrap config", bootstrap.Config{Version: bootstrap.ConfigVersion, Service: "anchor", Anchor: &bootstrap.AnchorServiceConfig{}}},
		{"drafts/verifier-service.json", "verifier service bootstrap config", bootstrap.Config{Version: bootstrap.ConfigVersion, Service: "verifier", Verifier: &bootstrap.VerifierConfig{}}},
		{"drafts/authority-service.json", "authority service bootstrap config", bootstrap.Config{Version: bootstrap.ConfigVersion, Service: "authority", Authority: &bootstrap.AuthorityConfig{}}},
		{"drafts/host-policy.json", "typed host qualification policy", hostbridge.Policy{}},
		{"drafts/host-conformance-record.json", "typed host qualification record", bootstrap.HostConformance{}},
		{"drafts/usage-binding.json", "typed usage binding", usage.Binding{}},
		{"drafts/usage-policy.json", "typed usage policy", usage.Policy{}},
		{"drafts/usage-demand.json", "typed full-matrix usage demand", usage.Demand{}},
		{"drafts/capacity-reservation.json", "typed capacity reservation request", store.ReserveRequest{}},
		{"drafts/pilot-manifest.json", "typed pilot manifest", pilot.Manifest{}},
		{"drafts/calibration-grant.json", "typed calibration grant", pilot.CalibrationGrant{}},
		{"drafts/calibration-request.json", "typed calibration request", pilot.CalibrationRequest{}},
		{"drafts/calibration-bounds.json", "typed explicit operator upper bound", selection.ExplicitUpperBound{}},
		{"drafts/verifier-policy.json", "typed verifier policy", verification.VerificationPolicy{}},
		{"drafts/authority-enrollment.json", "typed authority enrollment", authority.Enrollment{}},
		{"drafts/bootstrap-envelope.json", "typed signed bootstrap envelope", bootstrap.Envelope{}},
		{"drafts/postrun-execution-grant.json", "typed post-run execution grant", bootstrap.PostRunExecutionGrant{}},
		{"drafts/host-control-proof.json", "typed host control proof", hostbridge.ControlProof{}},
		{"drafts/verifier-control-proof.json", "typed verifier control proof", verification.ControlProof{}},
		{"drafts/acceptance-projection.json", "typed acceptance authority projection", records.AcceptanceAuthorityState{}},
		{"drafts/acceptance-receipt.json", "typed acceptance receipt", verification.AcceptanceReceipt{}},
		{"drafts/writeback-readback.json", "typed provider readback report", writeback.CreateReadbackReport{}},
		{"drafts/workflow-sync-receipt.json", "typed workflow sync receipt", contracts.SyncReceipt{}},
		{"drafts/p8-sync-operation.json", "typed P8 sync operation", contracts.SyncOperation{}},
		{"drafts/p8-writeback-grant.json", "typed P8 writeback grant", writeback.Grant{}},
	}
	for _, x := range additional {
		if err := addDraft(files, x.path, x.kind, x.value); err != nil {
			return nil, err
		}
	}
	readme := []byte("# Enrolled service offline preparation\n\nThese files are deterministic, closed drafts for the `codex-github-first-live-run` enrollment purpose. Every JSON draft wrapper sets `executable` to false. No draft is a signed input, and this generator does not read environment variables, credentials, native identity, network services, or host files. All unknown values are empty.\n\nThe operator must obtain independent evidence and have the exact payloads reviewed and signed through the external trust process before creating any files under `signed/` or `inputs/`. Never use these drafts as bootstrap inputs. The public root is provisioned separately as raw 32-byte Ed25519 public-key material.\n\nEach qualification operation requires its own derived anchor stream and independently administered anchor state. The 13 live gate proofs are external and must be independently issued; compilation and offline checks do not satisfy live gates. Calibration and experiment grants require external validation, bounded requests, and independent evidence.\n\nPost-run candidate artifacts exist only after a host run. Create verification, acceptance, and handoff records against the resulting immutable candidate and proof after that run; do not pre-bind a future output. P8 writeback requires its own operation-scoped grant and independent reconciliation/readback.\n\n`manifest.json` hashes every immutable draft and this README. `operator-inputs.json` is an initially blank inventory for later human completion and is intentionally excluded from the immutable hashes. `manifest.json` and the drafts are not authorization. The compiled enrollment `Requirements()` remains authoritative, and every listed input and signed bootstrap file is intentionally absent here.\n")
	files["README.md"] = readme
	assets := make([]Asset, 0, len(files))
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		sum := sha256.Sum256(files[p])
		assets = append(assets, Asset{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	manifest, err := encode(Manifest{Version: Version, Purpose: "codex-github-first-live-run", Drafts: assets, Required: Requirements()})
	if err != nil {
		return nil, err
	}
	files["manifest.json"] = manifest
	refs := make(map[string]string, len(SecretReferenceIDs()))
	for _, id := range SecretReferenceIDs() {
		refs[id] = ""
	}
	inputs, err := encode(Inventory{Version: Version, SecretReferences: refs})
	if err != nil {
		return nil, err
	}
	files["operator-inputs.json"] = inputs
	return files, nil
}

func serviceFor(id string) string {
	switch id {
	case "anchor-config":
		return "anchor"
	case "verifier-config":
		return "verifier"
	case "authority-config":
		return "authority"
	case "qualify-github-read", "qualify-codex-quota", "qualify-host", "qualify-verifier", "qualify-comment":
		return "qualification"
	default:
		return "worker"
	}
}

// Prepare writes the deterministic draft set into a new private directory.
func Prepare(out string) error {
	files, err := Files()
	if err != nil {
		return err
	}
	return evidence.WriteFiles(out, files)
}
