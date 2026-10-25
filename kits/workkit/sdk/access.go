package sdk

import (
	"context"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/access"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/accessjournal"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/auth"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/platformfs"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/toolchain"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AccessBackend is a trusted embedding port, not selectable executable code.
// check must be called immediately before every external action. Backends must
// bound IO, never log raw responses, and return only allowlisted metadata.
type AccessBackend interface {
	Authenticate(context.Context, string, a.Configuration, func() error) (a.Binding, error)
}

func (s *Service) WithAccessBackend(b AccessBackend) *Service { s.access = b; return s }
func AccessRoutes() any                                       { return auth.Routes() }
func AccessSigningBytes(g a.Grant) []byte                     { return access.SigningBytes(g) }
func AccessImplementationSHA256() string                      { return engine.ImplementationSHA256 }
func ToolPlatform() string                                    { return toolchain.Platform() }

// InspectTool checks bytes without invoking the executable. Version text is a
// declared pin until an authorized tools verify operation probes it.
func InspectTool(ctx context.Context, raw []byte, at time.Time) (a.Observation, error) {
	var pin a.ToolPin
	var o a.Observation
	if e := DecodeRead(raw, &pin); e != nil {
		return o, failure("tool.invalid", "invalid tool pin", 2, e)
	}
	if at.IsZero() {
		return o, failure("time.required", "explicit as-of required", 2, nil)
	}
	if e := toolchain.Verify(ctx, pin); e != nil {
		return o, failure("tool.mismatch", "tool bytes or platform do not match", 4, e)
	}
	return a.Observation{Version: a.Version, Kind: "ToolObservation", Tool: pin, ObservedAt: at.UTC().Format(time.RFC3339Nano), Verified: false, LiveQualification: "pending"}, nil
}
func ValidateAuthConfiguration(raw []byte) (a.Configuration, error) {
	var c a.Configuration
	if e := DecodeRead(raw, &c); e != nil {
		return c, e
	}
	if e := auth.Validate(c); e != nil {
		return c, e
	}
	d, e := Parse(c.Profile, "json")
	if e != nil || d.Kind() != "ConnectionProfile" {
		return c, errors.New("exact connection profile required")
	}
	var p map[string]any
	if e = DecodeRead(d.CanonicalBytes(), &p); e != nil {
		return c, e
	}
	spec := p["spec"].(map[string]any)
	meta := p["metadata"].(map[string]any)
	if meta["id"] != c.ConnectionID || spec["provider"] != c.Provider || spec["origin"] != c.Origin || spec["basePath"] != c.BasePath {
		return c, errors.New("auth profile identity mismatch")
	}
	scope := spec["scope"].(map[string]any)
	if scope["tenantId"] != c.TenantID {
		return c, errors.New("auth tenant mismatch")
	}
	ids := scope["containerIds"].([]any)
	if len(ids) != len(c.Containers) {
		return c, errors.New("auth scope mismatch")
	}
	for i, v := range ids {
		if v != c.Containers[i] {
			return c, errors.New("auth scope mismatch")
		}
	}
	if c.Provider == "jira-datacenter" {
		dep, ok := spec["deployment"].(map[string]any)
		if !ok || c.TLS == nil {
			return c, errors.New("DC deployment binding required")
		}
		trust, ok := dep["tlsTrust"].(map[string]any)
		if !ok || dep["serverVersion"] != c.TLS.ServerVersion || trust["sha256"] != c.TLS.CASHA256 {
			return c, errors.New("DC deployment/TLS mismatch")
		}
	}
	return c, nil
}
func PlanTool(rawPolicy, rawArtifact []byte, destination, action, previousPath, previous string, at time.Time) (a.InstallPlan, error) {
	var p a.Policy
	var artifact a.Artifact
	var plan a.InstallPlan
	if e := DecodeRead(rawPolicy, &p); e != nil {
		return plan, e
	}
	if p.Version != a.Version || p.Kind != "AccessPolicy" || at.IsZero() {
		return plan, errors.New("policy/version/time required")
	}
	if e := DecodeRead(rawArtifact, &artifact); e != nil {
		return plan, e
	}
	if e := toolchain.ValidateArtifact(artifact); e != nil {
		return plan, e
	}
	approved := false
	for _, x := range p.Artifacts {
		if sameJSON(x, artifact) {
			approved = true
		}
	}
	if !approved {
		return plan, errors.New("artifact not approved")
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || action != "install" && action != "update" || action == "install" && previous != "" || action == "update" && len(previous) != 64 {
		return plan, errors.New("invalid install target or baseline")
	}
	plan = a.InstallPlan{Version: a.Version, Kind: "ToolInstallPlan", PolicySHA256: SHA256(rawPolicy), Artifact: artifact, Destination: destination, Action: action, PreviousSHA256: previous, PreviousPath: previousPath, ObservedAt: at.UTC().Format(time.RFC3339Nano)}
	return plan, nil
}
func sameJSON(x, y any) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}

// RunAccess consumes a protected operation ID and exact output before any IO.
// If interrupted, both stay consumed. It cannot activate runtime or senders.
func (s *Service) RunAccess(ctx context.Context, action string, request, policyRaw, grantRaw []byte, out string) (a.Receipt, error) {

	var p a.Policy
	var g a.Grant
	var receipt a.Receipt
	if e := DecodeRead(policyRaw, &p); e != nil {
		return receipt, failure("access.policy", "invalid access policy", 2, e)
	}
	if e := DecodeRead(grantRaw, &g); e != nil {
		return receipt, failure("access.grant", "invalid grant", 2, e)
	}
	if !access.Contains([]string{"tools install", "tools update", "tools verify", "auth login", "auth refresh", "auth logout", "connections test"}, action) {
		return receipt, failure("access.action", "unsupported access action", 3, nil)
	}
	recheck := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return access.Validate(g, p, policyRaw, request, action, out, engine.ImplementationSHA256, time.Now().UTC())
	}
	if e := recheck(); e != nil {
		return receipt, failure("access.denied", "grant is invalid, expired or revoked", 3, e)
	}
	var c a.Configuration
	var plan a.InstallPlan
	var pin a.ToolPin
	switch action {
	case "tools install", "tools update":
		if e := DecodeRead(request, &plan); e != nil {
			return receipt, failure("plan.invalid", "invalid install plan", 2, e)
		}
		if plan.Version != a.Version || plan.Kind != "ToolInstallPlan" || plan.AuthorizationGranted || plan.ExecutionReady || plan.PolicySHA256 != SHA256(policyRaw) || "tools "+plan.Action != action {
			return receipt, failure("plan.binding", "install plan mismatch", 4, nil)
		}
		approved := false
		for _, x := range p.Artifacts {
			if sameJSON(x, plan.Artifact) {
				approved = true
			}
		}
		if !approved || toolchain.ValidateArtifact(plan.Artifact) != nil {
			return receipt, failure("artifact.unapproved", "unapproved artifact", 3, nil)
		}
		if pathsOverlap(out, plan.Destination) || pathsOverlap(p.LedgerRoot, plan.Destination) {
			return receipt, failure("path.overlap", "installation overlaps authority/evidence", 2, nil)
		}
	case "tools verify":
		if e := DecodeRead(request, &pin); e != nil {
			return receipt, failure("tool.invalid", "invalid tool pin", 2, e)
		}
	default:
		var e error
		c, e = ValidateAuthConfiguration(request)
		if e != nil {
			return receipt, failure("auth.invalid", "invalid exact auth configuration", 2, e)
		}
		if action == "auth refresh" && c.Mode != "oauth2" && c.Broker == nil {
			return receipt, failure("auth.unsupported", "external-reference credentials require refresh by their owner", 3, nil)
		}
		if !access.Contains(p.AllowedConnections, c.ConnectionID) {
			return receipt, failure("connection.denied", "connection not allowed", 3, nil)
		}
		if c.Mode == "gh-native" || c.Mode == "az-native" || c.Mode == "acli-native" {
			nativeCtx := context.WithValue(ctx, nativeAuthorityKey{}, nativeAuthority{p, g.OperationID})
			if e := VerifyNativeProfile(nativeCtx, c); e != nil {
				return receipt, failure("native.controls", "fresh inherited native controls required", 3, e)
			}
		}
		if c.Read != nil {
			if action != "connections test" || pathsOverlap(out, c.Read.Output) || pathsOverlap(p.LedgerRoot, c.Read.Output) {
				return receipt, failure("read.binding", "invalid separately enrolled read output", 2, nil)
			}
		} else if action == "connections test" {
			return receipt, failure("read.required", "exact read inputs required", 2, nil)
		}
		if s.access == nil {
			return receipt, failure("auth.backend", "trusted auth backend unavailable", 3, nil)
		}
	}
	if pathsOverlap(out, p.LedgerRoot) {
		return receipt, failure("path.overlap", "evidence overlaps protected ledger", 2, nil)
	}
	root, e := engine.SafePath(p.LedgerRoot)
	if e != nil || !filepath.IsAbs(p.LedgerRoot) {
		return receipt, failure("ledger.invalid", "external protected ledger unavailable", 3, e)
	}
	st, e := os.Stat(root)
	if e != nil || !st.IsDir() || platformfs.PrivateDirectory(root) != nil {
		return receipt, failure("ledger.private", "protected private ledger required", 3, e)
	}
	reservation, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return receipt, failure("output.conflict", "fresh private output required", 4, e)
	}
	// Operation namespace is independent of output and grant issuer. A new grant
	// cannot reuse an ID; deletion/rollback is outside the protected embedding.
	if runtime.GOOS == "windows" {
		if e = accessjournal.Consume(ctx, root, g.OperationID); e != nil {
			return receipt, failure("operation.consumed", "durable operation already consumed or unavailable", 4, e)
		}
	}
	consumed := filepath.Join(root, "operation-"+SHA256([]byte(g.OperationID)))
	f, e := os.OpenFile(consumed, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return receipt, failure("operation.consumed", "operation already consumed or ledger unavailable", 4, e)
	}
	_, e = f.Write([]byte(SHA256(grantRaw) + "\n"))
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		return receipt, failure("ledger.uncertain", "consumption durability uncertain", 5, e)
	}
	if runtime.GOOS != "windows" {
		if e = syncDir(root); e != nil {
			return receipt, failure("ledger.uncertain", "consumption durability uncertain", 5, e)
		}
	}
	receipt = a.Receipt{Version: a.Version, Kind: "AccessReceipt", OperationID: g.OperationID, Action: action, RequestSHA256: SHA256(request), PolicySHA256: SHA256(policyRaw), GrantSHA256: SHA256(grantRaw), ImplementationSHA256: engine.ImplementationSHA256, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "unknown", LocalEffect: "possible", ProviderEffect: "none", Diagnostic: "operation unresolved; reconcile without resend", MigrationActivation: "blocked", LiveQualification: "pending", ProductionActivation: "not-authorized"}
	ctx = context.WithValue(ctx, nativeAuthorityKey{}, nativeAuthority{p, g.OperationID})
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	check := func() error {
		if e := bounded.Err(); e != nil {
			return e
		}
		return recheck()
	}
	// This exact policy snapshot cannot establish online revocation service
	// conformance. Enrollment for actual live use remains an independent gate.
	var opErr error
	var lock string
	if c.ConnectionID != "" {
		lock = filepath.Join(root, "connection-"+SHA256([]byte(c.ConnectionID)))
		var journalErr error
		if runtime.GOOS == "windows" {
			journalErr = accessjournal.Hold(bounded, root, "connection:"+c.ConnectionID)
		}
		lf, le := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if lf != nil {
			lf.Close()
		}
		if le != nil || journalErr != nil {
			opErr = errors.New("connection locked; reconcile")
		} else {
			if runtime.GOOS != "windows" {
				opErr = syncDir(root)
			}
		}
	}
	if opErr == nil && action == "connections test" {
		opErr = checkAccessState(bounded, root, c)
	}
	if opErr == nil {
		opErr = check()
		if opErr == nil {
			switch action {
			case "tools install", "tools update":
				var installed a.ToolPin
				installed, opErr = toolchain.Install(bounded, plan, check)
				if opErr == nil {
					if plan.Artifact.Layout == "azure-extension" && installed.Runtime != nil {
						receipt.Extension = &a.Extension{ID: "azure-devops", Version: plan.Artifact.ToolVersion, Path: installed.Runtime.Path, SHA256: installed.Runtime.SHA256, Layout: "tree-v1"}
					} else {
						receipt.Tool = &installed
					}
				}
			case "tools verify":
				_, opErr = toolchain.Observe(bounded, pin, time.Now().UTC(), check)
			default:
				receipt.ProviderEffect = "none"
				if c.Mode == "oauth2" && action != "connections test" {
					receipt.ProviderEffect = "possible"
				}
				var b a.Binding
				b, opErr = s.access.Authenticate(bounded, action, c, check)
				if opErr == nil {
					observed, timeErr := time.Parse(time.RFC3339Nano, b.ObservedAt)
					started, _ := time.Parse(time.RFC3339Nano, receipt.StartedAt)
					expectedStatus := "authenticated"
					if action == "auth logout" {
						expectedStatus = "disconnected"
					}
					if timeErr != nil || observed.Before(started) || observed.After(time.Now().UTC()) || b.Status != expectedStatus || !auth.ID.MatchString(b.Generation) || (b.StorageMode != "external-reference" && b.StorageMode != "os-keychain" && b.StorageMode != "secret-service" && b.StorageMode != "windows-credential-manager") {
						opErr = errors.New("backend observation invalid")
					} else if b.Version != a.Version || b.Kind != "AuthBindingObservation" || b.ConnectionID != c.ConnectionID || b.Principal != c.NativePrincipal || b.TenantID != c.TenantID || b.Mode != c.Mode || b.Generation == "" || b.AuthorizationGranted || b.ExecutionReady || b.ScopedReadVerified || b.LiveQualification != "pending" {
						opErr = errors.New("backend binding mismatch")
					} else {
						b.EvidenceTrust = "current-process-observation"
						b.ConfigurationSHA256 = SHA256(request)
						receipt.Binding = &b
					}
				}
			}
		}
	}
	if opErr == nil {
		receipt.Status = "ok"
		receipt.Diagnostic = "completed; qualification and activation remain gated"
		receipt.LocalEffect = "confirmed"
		if receipt.ProviderEffect != "none" {
			if receipt.Binding != nil && receipt.Binding.ProviderInteraction == "none" {
				receipt.ProviderEffect = "none"
			} else {
				receipt.ProviderEffect = "confirmed"
			}
		}

	}
	receipt.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	preserve, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	files := map[string][]byte{"request.json": request, "policy.json": policyRaw, "grant.json": grantRaw, "bundle.json": engine.Encode(receipt)}
	if e = reservation.Write(preserve, files); e != nil {
		return receipt, failure("evidence.uncertain", "output incomplete; operation remains consumed", 5, e)
	}
	if runtime.GOOS != "windows" {
		if e = reservation.Sync(preserve); e != nil {
			return receipt, failure("evidence.uncertain", "evidence flush uncertain; connection remains locked", 5, e)
		}
	}
	if opErr == nil && c.ConnectionID != "" {
		if e = persistAccessState(preserve, root, out, c, receipt); e != nil {
			return receipt, failure("state.uncertain", "receipt preserved but local state unresolved", 5, e)
		}
	}
	if opErr == nil && runtime.GOOS == "windows" {
		hold, stateKey := "", ""
		var state []byte
		if c.ConnectionID != "" {
			hold = "connection:" + c.ConnectionID
			stateKey = AuthScopeSHA256(c)
			state, e = ReadLocal(preserve, filepath.Join(root, "auth-state-"+stateKey+".json"))
			if e != nil {
				return receipt, failure("state.uncertain", "local state snapshot unavailable", 5, e)
			}
		}
		if e = accessjournal.Complete(preserve, root, g.OperationID, hold, stateKey, engine.Encode(receipt), state); e != nil {
			return receipt, failure("journal.uncertain", "durable finalization uncertain; holds retained", 5, e)
		}
	}
	if opErr == nil && lock != "" {
		if e := os.Remove(lock); e != nil {
			return receipt, failure("lock.uncertain", "receipt preserved but lock requires reconciliation", 5, e)
		}
	}
	if opErr != nil {
		var partial *Error
		if errors.As(opErr, &partial) && partial.ExitCode == 6 {
			return receipt, opErr
		}
	}
	if opErr != nil {
		return receipt, failure("access.uncertain", "operation failed or effects uncertain; no automatic retry", 5, nil)
	}
	return receipt, nil
}
func pathsOverlap(x, y string) bool {
	a, e := filepath.Abs(x)
	if e != nil {
		return true
	}
	b, e := filepath.Abs(y)
	if e != nil {
		return true
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		r, e := filepath.Rel(pair[0], pair[1])
		if e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator))) {
			return true
		}
	}
	return false
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// InspectAuth is strictly offline. It never invokes a CLI, refreshes credentials
// or promotes a stale authentication observation into current scoped access.
func InspectAuth(configRaw, receiptRaw []byte, at time.Time) (a.Binding, error) {
	c, e := ValidateAuthConfiguration(configRaw)
	if e != nil {
		return a.Binding{}, e
	}
	var r a.Receipt
	if e = DecodeRead(receiptRaw, &r); e != nil {
		return a.Binding{}, e
	}
	if r.Version != a.Version || r.Kind != "AccessReceipt" || r.Binding == nil || r.RequestSHA256 != SHA256(configRaw) || r.Binding.ConfigurationSHA256 != SHA256(configRaw) || r.Binding.ConnectionID != c.ConnectionID || r.Status != "ok" {
		return a.Binding{}, errors.New("binding receipt mismatch")
	}
	b := *r.Binding
	if b.Version != a.Version || b.Kind != "AuthBindingObservation" || b.LiveQualification != "pending" {
		return a.Binding{}, errors.New("untrusted auth binding kind/version/qualification")
	}
	if b.Principal != c.NativePrincipal || b.TenantID != c.TenantID || b.Mode != c.Mode || !access.Contains([]string{"authenticated", "disconnected", "expired", "error"}, b.Status) || !access.Contains([]string{"none", "identity-observation", "revocation"}, b.ProviderInteraction) || !access.Contains([]string{"external-reference", "os-keychain", "secret-service", "windows-credential-manager"}, b.StorageMode) {
		return a.Binding{}, errors.New("invalid historical auth metadata")
	}
	if b.AuthorizationGranted || b.ExecutionReady {
		return a.Binding{}, errors.New("auth receipt cannot grant authority")
	}
	b.AuthorizationGranted = false
	b.ExecutionReady = false
	b.ScopedReadVerified = false
	b.EvidenceTrust = "unverified-local-receipt"
	b.Status = "unknown"
	t, e := time.Parse(time.RFC3339Nano, b.ObservedAt)
	if e != nil || at.IsZero() || at.Before(t) {
		return b, errors.New("invalid observation time")
	}
	if at.Sub(t) > 5*time.Minute {
		b.Status = "expired"
	}
	b.ScopedReadVerified = false
	b.EvidenceTrust = "unverified-local-receipt"
	if b.Status != "expired" {
		b.Status = "unknown"
	}
	return b, nil
}

func WriteAccessPlan(ctx context.Context, p a.InstallPlan, policyRaw []byte, out string) error {
	return engine.WriteBundle(ctx, out, map[string][]byte{"plan.json": engine.Encode(p), "policy.json": policyRaw}, "")
}
