package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

const writebackHelp = `aok writeback replay --scenario FILE --as-of RFC3339 --out NEW [--json]
aok tracker authorize-plan --draft BUNDLE --policy FILE --out NEW [--json]
aok tracker enqueue --operation FILE --grant FILE --policy FILE --capability FILE --revocation FILE --draft BUNDLE --snapshot BUNDLE --mapping FILE --outbox FILE [--json]
aok tracker outbox FILE [--json]
aok tracker revoke --outbox FILE --grant ID --revocation FILE --policy FILE [--json]
aok tracker dispatch --outbox FILE --operation ID --once [--json]
aok tracker reconcile --outbox FILE --operation ID [--json]
Approval preimages grant no authority. All four provider live-write gates remain pending.
Production dispatch/reconciliation require separately qualified provider transports.`

func runWriteback(args []string, out, errout io.Writer) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(errout, writebackHelp)
		return 2, nil
	}
	if args[0] == "--help" || args[0] == "help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, writebackHelp)
		return 0, nil
	}
	if args[0] != "replay" {
		return 2, nil
	}
	v, _, e := selectionFlags(args[1:], []string{"scenario", "as-of", "out"})
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2, nil
	}
	for _, k := range []string{"scenario", "as-of", "out"} {
		if v["--"+k] == "" {
			fmt.Fprintln(errout, "missing --"+k)
			return 2, nil
		}
	}
	at, e := phase8AsOf(v["--as-of"])
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2, nil
	}
	var s writeback.ReplayScenario
	if e = phase8Read(v["--scenario"], &s); e != nil {
		return 1, e
	}
	entries, e := writeback.Replay(s, at)
	if e != nil {
		return 1, e
	}
	r := map[string]any{"version": writeback.APIVersion, "scope": "offline-fixture", "entries": entries, "authorizationGranted": false, "executionReady": false, "providerLiveGates": "pending"}
	if e = evidence.WriteJSON(v["--out"], "replay.json", r, v["--scenario"]); e != nil {
		return 1, e
	}
	return phase8Print(out, r)
}
func runTrackerWriteback(args []string, out, errout io.Writer) (int, error) {
	return runTrackerWritebackServices(args, out, errout, Phase9Services{})
}
func runTrackerWritebackServices(args []string, out, errout io.Writer, services Phase9Services) (int, error) {
	op := args[0]
	args = args[1:]
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprintln(out, writebackHelp)
		return 0, nil
	}
	keys := map[string][]string{"authorize-plan": {"draft", "policy", "out"}, "enqueue": {"operation", "grant", "policy", "capability", "revocation", "outbox", "draft", "snapshot", "mapping"}, "outbox": {}, "revoke": {"outbox", "grant", "revocation", "policy"}, "dispatch": {"outbox", "operation"}, "reconcile": {"outbox", "operation"}}[op]
	path := ""
	if op == "outbox" {
		if len(args) == 0 {
			return 2, nil
		}
		path = args[0]
		args = args[1:]
	}
	once := false
	filtered := []string{}
	for _, a := range args {
		if a == "--once" {
			if op != "dispatch" || once {
				return 2, nil
			}
			once = true
		} else {
			filtered = append(filtered, a)
		}
	}
	v, _, e := selectionFlags(filtered, keys)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2, nil
	}
	for _, k := range keys {
		if v["--"+k] == "" {
			fmt.Fprintln(errout, "missing --"+k)
			return 2, nil
		}
	}
	if op == "dispatch" && !once {
		fmt.Fprintln(errout, "dispatch requires --once")
		return 2, nil
	}
	if op == "dispatch" || op == "reconcile" {
		parent := services.Context
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
		defer cancel()
		if op == "dispatch" && services.TrackerDispatch != nil {
			return phase9TrackerDispatch(ctx, v["--outbox"], v["--operation"], services, out)
		}
		if op == "reconcile" && services.TrackerReconciliation != nil {
			return phase9TrackerReconcile(ctx, v["--outbox"], v["--operation"], services, out)
		}
		// No concrete live transport is qualified by a fixture capability declaration.
		_, e = phase8Print(out, map[string]any{"status": "blocked", "reason": "provider operation/auth/scope transport conformance is pending", "authorizationGranted": false, "executionReady": false})
		return 1, e
	}
	if op == "authorize-plan" {
		planPath := filepath.Join(v["--draft"], "update-plan.json")
		var p tracker.TrackerUpdatePlan
		if e = phase8Read(planPath, &p); e != nil {
			return 1, e
		}
		stored := p.Digest
		p.Digest = ""
		if tracker.Digest(p) != stored {
			return 1, fmt.Errorf("update draft digest mismatch")
		}
		p.Digest = stored
		var policy writeback.TrustPolicy
		if e = phase8Read(v["--policy"], &policy); e != nil {
			return 1, e
		}
		if policy.APIVersion != writeback.APIVersion || policy.Kind != "TrustPolicy" {
			return 1, fmt.Errorf("unsupported write policy")
		}
		if p.Version != tracker.Version || p.Report.Status != "valid" {
			return 1, fmt.Errorf("blocked or unsupported draft")
		}
		at, e := time.Parse(time.RFC3339Nano, p.AsOf)
		now := time.Now().UTC()
		if e != nil || now.Before(at) || now.Sub(at) >= 5*time.Minute {
			return 1, fmt.Errorf("refresh-needed: draft observations are stale")
		}
		r := map[string]any{"apiVersion": writeback.APIVersion, "kind": "ApprovalPreimage", "draft": p, "policy": policy, "authorizationGranted": false, "executionReady": false, "next": "fresh capture and narrow externally signed grant required; this preimage is not executable"}
		if e = evidence.WriteJSON(v["--out"], "approval-preimage.json", r, v["--draft"], v["--policy"]); e != nil {
			return 1, e
		}
		return phase8Print(out, r)
	}
	if op == "outbox" {
		box, e := writeback.OpenReadOnly(path)
		if e != nil {
			return 1, e
		}
		defer box.Close()
		r, e := box.Inspect(context.Background())
		if e != nil {
			return 1, e
		}
		return phase8Print(out, r)
	}
	var policy writeback.TrustPolicy
	var rev writeback.RevocationSnapshot
	if e = phase8Read(v["--policy"], &policy); e != nil {
		return 1, e
	}
	if e = phase8Read(v["--revocation"], &rev); e != nil {
		return 1, e
	}
	if op == "revoke" {
		found := false
		for _, id := range rev.RevokedGrantIDs {
			if id == v["--grant"] {
				found = true
			}
		}
		if !found {
			return 1, fmt.Errorf("signed revocation does not revoke requested grant")
		}
		box, e := writeback.Open(v["--outbox"])
		if e != nil {
			return 1, e
		}
		defer box.Close()
		if e = box.ImportRevocation(policy, rev, time.Now().UTC()); e != nil {
			return 1, e
		}
		return phase8Print(out, map[string]string{"status": "revocation-imported"})
	}
	var operation contracts.SyncOperation
	var grant writeback.Grant
	var cap contracts.ConnectorCapability
	if e = phase8Read(v["--operation"], &operation); e != nil {
		return 1, e
	}
	if e = phase8Read(v["--grant"], &grant); e != nil {
		return 1, e
	}
	if e = phase8Read(v["--capability"], &cap); e != nil {
		return 1, e
	}
	if _, ds := connector.ValidateOperation(operation, cap, grant.Scope); len(ds) > 0 {
		return 1, fmt.Errorf("invalid operation: %v", ds)
	}
	now := time.Now().UTC()
	current, e := phase8GrantContext(v["--draft"], v["--snapshot"], v["--mapping"], grant, operation, now)
	if e != nil {
		return 1, e
	}
	// Completion and any evidence-dependent operation remain blocked at the CLI
	// until a qualified external milestone verifier is installed.
	var verifyEvidence writeback.EvidenceVerifier
	if services.WritebackEvidenceVerifier != nil {
		verifyEvidence, e = services.WritebackEvidenceVerifier(operation, grant)
		if e != nil {
			return 1, e
		}
	}
	approved, e := writeback.VerifyGrantBound(grant, policy, rev, operation, cap, grant.Scope, current, verifyEvidence, now)
	if e != nil {
		return 1, e
	}
	box, e := writeback.Open(v["--outbox"])
	if e != nil {
		return 1, e
	}
	defer box.Close()
	if e = box.ImportRevocation(policy, rev, now); e != nil {
		return 1, e
	}
	entry, e := box.Enqueue(operation, approved)
	if e != nil {
		return 1, e
	}
	return phase8Print(out, entry)
}

func phase8GrantContext(draftPath, snapshotPath, mappingPath string, g writeback.Grant, op contracts.SyncOperation, now time.Time) (writeback.GrantContext, error) {
	var p tracker.TrackerUpdatePlan
	if e := phase8Read(filepath.Join(draftPath, "update-plan.json"), &p); e != nil {
		return writeback.GrantContext{}, e
	}
	stored := p.Digest
	p.Digest = ""
	if tracker.Digest(p) != stored {
		return writeback.GrantContext{}, fmt.Errorf("draft hash changed")
	}
	p.Digest = stored
	if !phase8PlanCovers(p, op) {
		return writeback.GrantContext{}, fmt.Errorf("operation is outside reviewed draft changes")
	}
	c, e := tracker.Load(snapshotPath)
	if e != nil {
		return writeback.GrantContext{}, e
	}
	m, e := tracker.ReadMapping(mappingPath)
	if e != nil {
		return writeback.GrantContext{}, e
	}
	fresh, e := tracker.PlanUpdate(c, m, p.Changes, now)
	if e != nil {
		return writeback.GrantContext{}, e
	}
	if p.Version != tracker.Version || p.Report.Status != "valid" || fresh.Report.Status != "valid" || p.SnapshotSHA256 != c.Snapshot.Digest || p.MappingSHA256 != tracker.Digest(m) || p.DesiredSHA256 != fresh.DesiredSHA256 || p.Identity != g.Scope || p.ExpectedRevision != g.ExpectedRevision {
		return writeback.GrantContext{}, fmt.Errorf("fresh draft scope, mapping, ownership or revision check failed")
	}
	at, e := time.Parse(time.RFC3339Nano, c.Snapshot.ObservedAt)
	if e != nil || now.Before(at) || now.Sub(at) >= 5*time.Minute {
		return writeback.GrantContext{}, fmt.Errorf("refresh-needed")
	}
	return writeback.GrantContext{DraftDigest: stored, SnapshotHash: c.Snapshot.Digest, MappingVersion: m.MappingVersion, ObservedAt: at}, nil
}

func phase8PlanCovers(p tracker.TrackerUpdatePlan, op contracts.SyncOperation) bool {
	switch op.OperationKind {
	case "comment":
		return p.Changes.Comment != nil && op.Change.Comment != nil && *p.Changes.Comment == *op.Change.Comment
	case "transition":
		return p.Changes.TransitionID != "" && p.Changes.TransitionID == op.Change.TransitionID
	case "update-fields":
		if len(op.Change.Fields) == 0 {
			return false
		}
		for _, field := range op.Change.Fields {
			desired, ok := p.Changes.Fields[field.ProviderFieldID]
			if !ok || !samePhase8JSON(desired, field.Value) {
				return false
			}
		}
		return true
	case "link":
		if op.Change.RelationIdentity == nil {
			return false
		}
		for _, r := range p.Changes.Relations {
			if r.Supported && r.Type == op.Change.RelationType && r.TargetID == op.Change.RelationIdentity.RemoteID && op.Change.RelationIdentity.Provider == p.Identity.Provider && op.Change.RelationIdentity.TenantID == p.Identity.TenantID && op.Change.RelationIdentity.ContainerID == p.Identity.ContainerID {
				return true
			}
		}
	}
	return false
}
func samePhase8JSON(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}
