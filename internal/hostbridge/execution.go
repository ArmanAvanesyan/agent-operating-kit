package hostbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"os"
	"path/filepath"
	"time"
)

func newAttemptID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "attempt-" + hex.EncodeToString(b[:])
}
func Run(ctx context.Context, p Prepared, policy Policy, runner Runner, previous *Receipt, guard AttemptGuard) (Bundle, error) {
	return runAttempt(ctx, p, policy, runner, previous, guard, newAttemptID())
}

// RunControlled requires a caller-allocated identity and a live guard. Start hooks
// run only at the inference transport boundary, after non-inference preflight.
func RunControlled(ctx context.Context, p Prepared, policy Policy, runner Runner, previous *Receipt, options AttemptOptions) (Bundle, error) {
	if options.AttemptID == "" || options.Guard == nil || options.BeforeStart == nil {
		return Bundle{}, fmt.Errorf("controlled attempt requires identity, guard and start hook")
	}
	return runAttempt(ctx, p, policy, controlledRunner{inner: runner, options: options}, previous, options.Guard, options.AttemptID)
}
func runAttempt(ctx context.Context, p Prepared, policy Policy, runner Runner, previous *Receipt, guard AttemptGuard, attemptID string) (Bundle, error) {
	b := Bundle{Prepared: &p, Files: map[string][]byte{}}
	r := Receipt{Version: p.Version, AttemptID: attemptID, PreparedSHA256: p.Digest, PolicySHA256: PolicyHash(policy), Host: p.Host, HostVersion: p.HostVersion, Workspace: p.Workspace, Model: p.Model, ConfigurationID: p.ConfigurationID, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), HostOutcome: "unknown", Report: BaseReport()}
	r.Report.Version = p.Version
	var transcript Transcript
	fail := func(code string, e error) (Bundle, error) {
		r.Report = blocked(code, "execution", e.Error())
		r.Report.Version = p.Version
		return withAccounting(finishAttempt(b, p, r, transcript), p, policy, transcript), nil
	}
	if p.Digest != preparedDigest(p) || p.PolicySHA256 != PolicyHash(policy) || p.Workspace != policy.Workspace || p.Host != policy.Host || p.HostVersion != policy.HostVersion || !contains(policy.Models, p.Model) || p.Version != policy.Version || (p.Version == VersionV2 && (p.HostDriverSHA256 != policy.HostDriverSHA256 || p.HostProtocol != policy.HostProtocol)) {
		return fail("input-drift", fmt.Errorf("prepared invocation no longer matches operator policy"))
	}
	if p.ConfigurationID != modelcatalog.ConfigurationID(policy.Configuration) || Digest(p.Configuration) != Digest(policy.Configuration) {
		return fail("configuration-drift", fmt.Errorf("prepared configuration differs from operator policy"))
	}
	var snapshot agentresolve.Snapshot
	if json.Unmarshal(p.Snapshot, &snapshot) != nil || snapshot.Digest != p.SnapshotSHA256 || Digest(snapshot.Permissions) != Digest(policy.Permissions) {
		return fail("profile-drift", fmt.Errorf("captured profile or permissions differ from policy"))
	}
	if e := configurationFidelity(snapshot.Profile, policy); e != nil {
		return fail("profile-drift", e)
	}
	var expectedWorkItem *contracts.Reference
	if policy.WorkItemBinding != nil {
		raw, err := assetBytes(policy.root, *policy.WorkItemBinding)
		if err != nil {
			return fail("binding-drift", err)
		}
		var binding contracts.WorkItemBinding
		if _, _, err = contracts.Parse(raw); err != nil {
			return fail("binding-drift", err)
		}
		if err = json.Unmarshal(raw, &binding); err != nil || binding.Packet.SHA256 != snapshot.TaskPacketSHA256 {
			return fail("binding-drift", fmt.Errorf("work item packet differs from assignment"))
		}
		expectedWorkItem = &contracts.Reference{Kind: "WorkItemBinding", ID: binding.Metadata.ID, Version: binding.Metadata.Version}
	}
	if Digest(expectedWorkItem) != Digest(p.WorkItemRef) {
		return fail("binding-drift", fmt.Errorf("prepared work-item reference differs from operator asset"))
	}
	expectedTasks := []TaskStep{}
	if policy.TaskPlan != nil {
		raw, err := assetBytes(policy.root, *policy.TaskPlan)
		if err != nil {
			return fail("task-plan-drift", err)
		}
		var plan struct {
			Steps []struct {
				ID          string   `json:"id"`
				Description string   `json:"description"`
				DependsOn   []string `json:"dependsOn"`
			} `json:"steps"`
		}
		if _, _, err = contracts.Parse(raw); err != nil {
			return fail("task-plan-drift", err)
		}
		if err = json.Unmarshal(raw, &plan); err != nil {
			return fail("task-plan-drift", err)
		}
		for _, step := range plan.Steps {
			expectedTasks = append(expectedTasks, TaskStep{ID: step.ID, Subject: step.Description, Description: step.Description, Dependencies: step.DependsOn, AOKState: "pending"})
		}
	}
	if len(p.NativeTasks) != 0 || len(expectedTasks) != 0 {
		if Digest(p.NativeTasks) != Digest(expectedTasks) || !policy.AllowNativeTasks {
			return fail("task-plan-drift", fmt.Errorf("native steps differ from operator-bound plan"))
		}
	}
	currentCatalog, catalogErr := catalogDigest(policy)
	if catalogErr != nil || currentCatalog != p.CatalogSHA256 {
		return fail("catalog-drift", fmt.Errorf("catalog changed since preparation"))
	}
	if !policy.AllowExecution {
		return fail("not-authorized", fmt.Errorf("operator policy does not allow execution"))
	}
	if e := verifyControls(policy, time.Now().UTC()); e != nil {
		return fail("enforcement-unverified", e)
	}
	preflightRunner := runner
	if controlled, ok := runner.(controlledRunner); ok {
		preflightRunner = controlled.inner
	}
	if _, e := Preflight(ctx, policy, preflightRunner); e != nil {
		return fail("preflight", e)
	}
	if p.Version == VersionV2 && previous != nil {
		return fail("resume-disabled", fmt.Errorf("v2 host resume requires a separately qualified native session protocol"))
	}
	if previous != nil {
		if previous.HostOutcome == "completed" || previous.OutputValidated {
			return fail("already-completed", fmt.Errorf("completed attempts cannot be resubmitted through resume"))
		}
		if previous.HostOutcome != "interrupted" {
			return fail("reconciliation-required", fmt.Errorf("failed or ambiguous attempts require host reconciliation before resubmission"))
		}
		if previous.Digest != receiptDigest(*previous) || previous.Host != p.Host || previous.HostVersion != p.HostVersion || previous.PolicySHA256 != p.PolicySHA256 || previous.ConfigurationID != p.ConfigurationID || previous.Workspace != p.Workspace || previous.PreparedSHA256 != p.Digest {
			return fail("resume-drift", fmt.Errorf("resume requires exact captured configuration/profile/inputs"))
		}
		if previous.SessionID == "" {
			return fail("resume-unknown", fmt.Errorf("native session identity absent"))
		}
		r.PreviousSHA256 = previous.Digest
	}
	if guard != nil {
		if e := guard.Check(ctx, r.AttemptID); e != nil {
			return fail("guard-rejected", e)
		}
	}
	// Single-controller exclusion is scoped to approved workspace/session and is
	// separate from the distributed leases/reservations supplied by P7.
	lockKey := p.ConfigurationID
	if previous != nil {
		lockKey = previous.SessionID
	}
	lock := filepath.Join(policy.Workspace, ".aok-host-"+Digest(lockKey)[:24]+".lock")
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fail("session-owned", fmt.Errorf("another controller or unresolved ownership lock exists"))
	}
	f.Close()
	defer os.Remove(lock)
	timeout := policy.TimeoutSeconds
	if snapshot.Profile.Limits != nil && snapshot.Profile.Limits.TimeoutSeconds != nil {
		n := snapshot.Profile.Limits.TimeoutSeconds
		seconds, err := n.Int64()
		if err != nil || seconds < 1 {
			return fail("timeout-invalid", fmt.Errorf("profile timeout must be positive whole seconds"))
		}
		if seconds < int64(timeout) {
			timeout = int(seconds)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	if p.Version == VersionV2 {
		transcript, e = NativeV2Run(ctx, p, policy, runner, attemptID)
	} else if p.Host == "codex" {
		transcript, e = CodexRun(ctx, p, policy, runner, previous, nil)
	} else {
		transcript, e = ClaudeRun(ctx, p, policy, runner, previous)
	}
	r.SessionID = transcript.SessionID
	r.ThreadID = transcript.ThreadID
	r.TurnID = transcript.TurnID
	r.HostOutcome = transcript.HostOutcome
	if e != nil {
		return fail("host-error", fmt.Errorf("host execution did not complete safely"))
	}
	if ctx.Err() != nil {
		return fail("cancelled", fmt.Errorf("attempt cancelled or timed out"))
	}
	if guard != nil {
		if e = guard.Check(ctx, r.AttemptID); e != nil {
			return fail("guard-rejected", e)
		}
	}
	if e = verifyControls(policy, time.Now().UTC()); e != nil {
		return fail("control-expired", e)
	}
	if transcript.Model != p.Model {
		return fail("model-unverified", fmt.Errorf("reported model differs from exact selected identity"))
	}
	if transcript.HostOutcome != "completed" {
		return fail("host-not-completed", fmt.Errorf("host outcome remains failed/interrupted/unknown"))
	}
	if e = validateOutput(p, transcript.Output); e != nil {
		return fail("output-invalid", e)
	}
	// Existing contracts may require deliverables, sections and resolvable evidence.
	// Check them before publishing a candidate; P8 still owns independent acceptance.
	if e = validateDeliverables(p); e != nil {
		return fail("deliverables-invalid", e)
	}
	r.OutputValidated = true
	r.Report.Diagnostics = append(r.Report.Diagnostics, transcript.Diagnostics...)
	return withAccounting(finishAttempt(b, p, r, transcript), p, policy, transcript), nil
}
func validateOutput(p Prepared, out []byte) error {
	if usage.Decode(out, new(any)) != nil {
		return fmt.Errorf("host output is not complete duplicate-free JSON")
	}
	var schema any
	if usage.Decode(p.OutputSchema, &schema) != nil {
		return fmt.Errorf("invalid output schema")
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(denyLoader{})
	if e := c.AddResource("urn:aok:host-output", schema); e != nil {
		return e
	}
	s, e := c.Compile("urn:aok:host-output")
	if e != nil {
		return fmt.Errorf("output schema cannot compile offline")
	}
	var v any
	_ = usage.Decode(out, &v)
	if e = s.Validate(v); e != nil {
		return fmt.Errorf("host output does not satisfy captured schema")
	}
	return nil
}
func validateDeliverables(p Prepared) error {
	var s agentresolve.Snapshot
	if json.Unmarshal(p.Snapshot, &s) != nil {
		return fmt.Errorf("resolution snapshot required")
	}
	for _, d := range s.Output.Deliverables {
		if !d.Required {
			continue
		}
		if d.Destination == "" {
			return fmt.Errorf("required deliverable path absent")
		}
		b, e := ReadAsset(p.Workspace, d.Destination)
		if e != nil {
			return fmt.Errorf("required deliverable absent, unsafe or unresolved template")
		}
		if d.RequiredSections != nil {
			for _, section := range *d.RequiredSections {
				if !containsText(b, section) {
					return fmt.Errorf("required deliverable section absent")
				}
			}
		}
		if d.Format == "json" {
			var v any
			if usage.Decode(b, &v) != nil {
				return fmt.Errorf("deliverable JSON invalid")
			}
		}
	}
	if len(s.Output.Deliverables) > 0 && s.Output.Validation.EvidenceReferences == "must-resolve" {
		return fmt.Errorf("evidence reference verification requires a supported verifier; result quarantined")
	}
	return nil
}
func containsText(b []byte, s string) bool { return bytesContains(b, []byte(s)) }
func bytesContains(b, sub []byte) bool {
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}
func finishAttempt(b Bundle, p Prepared, r Receipt, t Transcript) Bundle {
	r.ObservedModel = t.Model
	tbytes := encoded(t)
	r.TranscriptSHA256 = Hash(tbytes)
	r.Digest = receiptDigest(r)
	b.Receipt = &r
	b.Transcript = &t
	b.Files["prepared.json"] = encoded(p)
	b.Files["receipt.json"] = encoded(r)
	b.Files["transcript.json"] = tbytes
	header := func(kind, id string) contracts.DocumentHeader {
		return contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: kind, Metadata: contracts.OperationalMetadata{ID: id, Name: id, Version: "1.0.0", Description: "Host observation; no AOK acceptance or workflow authorization"}}
	}
	if r.SessionID != "" && p.WorkItemRef != nil {
		var snap agentresolve.Snapshot
		_ = json.Unmarshal(p.Snapshot, &snap)
		binding := contracts.HostTaskBinding{DocumentHeader: header("HostTaskBinding", r.AttemptID+"-binding"), WorkItemRef: *p.WorkItemRef, AssignmentRef: snap.Assignment.Agent, Host: p.Host, HostVersion: p.HostVersion, SessionID: r.SessionID, ThreadID: r.ThreadID, AttemptID: r.AttemptID, AssignmentSHA256: p.AssignmentSHA256}
		binding.AssignmentRef = contracts.Reference{Kind: "AgentAssignment", ID: snap.Assignment.Metadata.ID, Version: snap.Assignment.Metadata.Version}
		b.Files["host-task-binding.json"] = encoded(binding)
		disposition := "blocked"
		if r.OutputValidated {
			disposition = "completed"
		}
		run := contracts.RunRecord{DocumentHeader: header("RunRecord", r.AttemptID), HostTaskBindingRef: contracts.Reference{Kind: "HostTaskBinding", ID: r.AttemptID + "-binding", Version: "1.0.0"}, Runtime: "aok-host-bridge", RuntimeVersion: p.Version, RuntimeRunID: r.AttemptID, AttemptID: r.AttemptID, InputRevision: p.SnapshotSHA256, ExecutionProfileRef: snap.Assignment.ExecutionProfile, AssignmentSHA256: p.AssignmentSHA256, PolicyRefs: []contracts.Reference{}, CheckpointRefs: []contracts.EvidenceRef{}, OutputRefs: []contracts.EvidenceRef{}, ObservedAt: r.ObservedAt, Disposition: disposition}
		b.Files["run-record.json"] = encoded(run)
	}
	return b
}
