package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/cli"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector/providers"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/authority"
	codexcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/codex"
	githubcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/github"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/verifier"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot/experiment"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	workflow "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	usagecollectors "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type WorkerAuthority struct {
	Endpoint          string               `json:"endpoint"`
	ServerID          string               `json:"serverId"`
	EnrollmentID      string               `json:"enrollmentId"`
	Scope             workflow.TrialScope  `json:"scope"`
	ResponsePublicKey string               `json:"responsePublicKey"`
	TokenRef          string               `json:"tokenRef"`
	OperatorPublicKey string               `json:"operatorPublicKey"`
	Issuers           []authority.Issuer   `json:"issuers"`
	Enrollment        authority.Enrollment `json:"enrollment"`
	Bindings          map[string]string    `json:"bindings"`
	Epoch             operations.Epoch     `json:"epoch"`
	ActivationProof   json.RawMessage      `json:"activationProof"`
}

type CalibrationRecords struct {
	Manifest     pilot.Manifest           `json:"manifest"`
	Grant        pilot.CalibrationGrant   `json:"grant"`
	Request      pilot.CalibrationRequest `json:"request"`
	Bounds       json.RawMessage          `json:"bounds"`
	ManifestPath string                   `json:"manifestPath"`
	GrantPath    string                   `json:"grantPath"`
	OutputPath   string                   `json:"outputPath"`
}

type ExperimentRecords struct {
	Bundle     experiment.Bundle       `json:"bundle"`
	BundlePath string                  `json:"bundlePath"`
	Readiness  workflow.TrialReadiness `json:"readiness"`
	OutputPath string                  `json:"outputPath"`
}

type WorkerVerifier struct {
	Endpoint      string                          `json:"endpoint"`
	AuthReference string                          `json:"authReference"`
	Policy        verification.VerificationPolicy `json:"policy"`
	VerifierID    string                          `json:"verifierId"`
	Proof         verification.ControlProof       `json:"proof"`
}

type GitHubReadConfig struct {
	Collector          githubcollector.Config          `json:"collector"`
	ReadAuthorizations []githubcollector.Authorization `json:"readAuthorizations"`
	Credential         TokenMetadata                   `json:"credential"`
	OutputPath         string                          `json:"outputPath"`
}

type TokenMetadata struct {
	AuthReference      string   `json:"authReference"`
	EnvReference       string   `json:"envReference"`
	PrincipalPseudonym string   `json:"principalPseudonym"`
	GrantRef           string   `json:"grantRef"`
	Scopes             []string `json:"scopes"`
	ValidUntil         string   `json:"validUntil"`
}

type CodexQuotaConfig struct {
	Collector  codexcollector.Config `json:"collector"`
	Binding    usage.Binding         `json:"binding"`
	Policy     usage.Policy          `json:"policy"`
	OutputPath string                `json:"outputPath"`
}

type GitHubWriteRecords struct {
	ImmutableIssueID  string                         `json:"immutableIssueId"`
	IssueNumber       string                         `json:"issueNumber"`
	CommentAssetsRoot string                         `json:"commentAssetsRoot"`
	EvidenceRoot      string                         `json:"evidenceRoot"`
	Collector         githubcollector.Config         `json:"collector"`
	Credential        TokenMetadata                  `json:"credential"`
	Operation         contracts.SyncOperation        `json:"operation"`
	Capability        contracts.ConnectorCapability  `json:"capability"`
	Identity          contracts.RemoteIdentity       `json:"identity"`
	Grant             writeback.Grant                `json:"grant"`
	Policy            writeback.TrustPolicy          `json:"policy"`
	Revocation        writeback.RevocationSnapshot   `json:"revocation"`
	Current           writeback.GrantContext         `json:"current"`
	Reconciliation    writeback.ReconciliationReport `json:"reconciliation"`
	Owner             string                         `json:"owner"`
	OutboxPath        string                         `json:"outboxPath"`
}

type WorkerConfig struct {
	PostRun           *PostRunRecords     `json:"postRun,omitempty"`
	RuntimeRoot       string              `json:"runtimeRoot"`
	RunID             string              `json:"runId"`
	Authority         WorkerAuthority     `json:"authority"`
	ProtectedTrustKey string              `json:"protectedTrustKey"`
	Mode              string              `json:"mode"`
	Verifier          WorkerVerifier      `json:"verifier"`
	Calibration       *CalibrationRecords `json:"calibration,omitempty"`
	Experiment        *ExperimentRecords  `json:"experiment,omitempty"`
	GitHubRead        *GitHubReadConfig   `json:"githubRead,omitempty"`
	CodexQuota        *CodexQuotaConfig   `json:"codexQuota,omitempty"`
	GitHubWrite       *GitHubWriteRecords `json:"githubWrite,omitempty"`
}

// WorkerRun is a one-operation embedding. Its CLI arguments are derived only
// from signed operator configuration; caller/task input cannot replace records,
// roots, keys, credentials, or the selected purpose.
type WorkerRun struct {
	services cli.Phase9Services
	args     []string
	close    func() error
	run      func(context.Context, io.Writer, io.Writer) int
}

func (w *WorkerRun) Close() error {
	if w == nil || w.close == nil {
		return nil
	}
	return w.close()
}

func (w *WorkerRun) Run(ctx context.Context, out, errout io.Writer) int {
	if w == nil {
		return 1
	}
	if w.run != nil {
		return w.run(ctx, out, errout)
	}
	services := w.services
	services.Context = ctx
	return cli.RunWithPhase10Services(ctx, w.args, out, errout, cli.Phase10Services{Phase9: services})
}

// BuildWorker requires a compiled root pin and validates the active service
// enrollment and each dispatch record before opening runtime state. The only
// installed dispatches are one-shot calibration or experiment execution.
func BuildWorker(configPath, rootPath, compiledRootSHA256 string, now time.Time) (*WorkerRun, error) {
	_, c, _, err := loadAndVerify(configPath, rootPath, compiledRootSHA256, true, now)
	if err != nil {
		return nil, err
	}
	if c.Service != "worker" || c.Worker == nil || c.Authority != nil || c.Verifier != nil {
		return nil, ErrBlocked
	}
	if err = validateServiceConfig(c, now); err != nil {
		return nil, err
	}
	w := c.Worker
	if w.PostRun != nil || w.Mode == "verify-candidate" || w.Mode == "accept-candidate" || w.Mode == "handoff-candidate" {
		return buildPostRunWorker(c, w)
	}
	if w.Mode == "github-capture" || w.Mode == "codex-quota" {
		return buildCollectionWorker(c, w, configPath, rootPath, now)
	}
	if w.Mode == "github-dispatch" || w.Mode == "github-reconcile" {
		return buildGitHubWriteWorker(c, w, now)
	}
	// Check files selected for CLI readback against the exact records carried by
	// the signed enrollment before opening a mutable runtime ledger.
	if err = verifyWorkerRecordFiles(*w); err != nil {
		return nil, err
	}
	remote, err := buildRemote(c, w, now)
	if err != nil {
		return nil, err
	}
	activationScope := ""
	if w.Mode == "calibration" {
		d := calibrationDispatch(*w, remote)
		activationScope = workflow.CalibrationActivationScope(d)
	} else {
		d := experimentDispatch(*w, remote)
		activationScope = workflow.ExperimentActivationScope(d.Bundle)
	}
	if activationScope == "" {
		return nil, ErrBlocked
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	activation, err := operations.Bind(ctx, remote, remote, remote.Epoch.Authority, remote.Epoch.Holder, activationScope, w.Authority.ActivationProof, time.Now)
	if err != nil {
		return nil, errors.New("protected worker activation denied")
	}
	controller, err := workflow.Open(w.RuntimeRoot)
	if err != nil {
		return nil, ErrBlocked
	}
	if controller.Store == nil || controller.Runner == nil {
		controller.Store.Close()
		return nil, ErrBlocked
	}
	services := cli.Phase9Services{VerifierControls: workerVerifierControls(c, w.Verifier)}
	var args []string
	if w.Mode == "calibration" {
		d := calibrationDispatch(*w, remote)
		d.Activation = activation
		services.CalibrationExecution = &cli.CalibrationExecutionService{Controller: controller, Enrollment: d}
		r := w.Calibration
		args = []string{"pilot", "calibration", "execute", "--once", "--campaign", r.ManifestPath, "--authority", r.GrantPath, "--out", r.OutputPath}
	} else {
		d := experimentDispatch(*w, remote)
		d.Activation = activation
		services.ExperimentExecution = &cli.ExperimentExecutionService{Controller: controller, Enrollment: d}
		r := w.Experiment
		args = []string{"pilot", "experiment", "execute", "--once", "--bundle", r.BundlePath, "--out", r.OutputPath}
	}
	return &WorkerRun{services: services, args: args, close: controller.Store.Close}, nil
}

func validateWorkerConfig(c Config, now time.Time) error {
	if c.Worker != nil && (c.Worker.PostRun != nil || c.Worker.Mode == "verify-candidate" || c.Worker.Mode == "accept-candidate" || c.Worker.Mode == "handoff-candidate") {
		return validatePostRunConfig(c, now)
	}
	w := c.Worker
	if c.Authority != nil || c.Verifier != nil || w == nil || w.RuntimeRoot == "" || w.RunID == "" || w.Mode == "" || len(w.Authority.ActivationProof) == 0 {
		return ErrBlocked
	}
	if w.Mode == "github-capture" || w.Mode == "codex-quota" {
		if w.Calibration != nil || w.Experiment != nil || w.GitHubRead != nil && w.CodexQuota != nil {
			return ErrBlocked
		}
		if err := validateWorkerEnrollment(w.Authority, c.SecretEnv, now); err != nil {
			return err
		}
		if w.Mode == "github-capture" {
			g := w.GitHubRead
			if g == nil || w.CodexQuota != nil || g.OutputPath == "" || len(g.ReadAuthorizations) == 0 || g.Collector.Policy == nil || g.Credential.AuthReference != g.Collector.AuthReference || g.Credential.EnvReference == "" || c.SecretEnv[g.Credential.EnvReference] == "" || g.Credential.PrincipalPseudonym != g.Collector.Source.PrincipalPseudonym || g.Credential.GrantRef == "" || len(g.Credential.Scopes) == 0 {
				return ErrBlocked
			}
			if _, ok := c.SecretEnv[g.Credential.EnvReference]; !ok {
				return ErrBlocked
			}
			until, err := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
			if err != nil || !now.Before(until) {
				return ErrBlocked
			}
			seen := map[string]bool{}
			for _, a := range g.ReadAuthorizations {
				if a.SourceSHA256 != tracker.ScopeDigest(g.Collector.Source) || a.PolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) || a.Origin != g.Collector.Source.Origin || a.Principal != g.Credential.PrincipalPseudonym || a.GrantRef != g.Credential.GrantRef || a.Method != "GET" || !sameStrings(a.Scopes, g.Credential.Scopes) {
					return errors.New("GitHub read authorization is outside signed collector scope")
				}
				key := "github-read:" + usage.Digest(a)
				if seen[key] {
					return ErrBlocked
				}
				seen[key] = true
				if w.Authority.Bindings[key] != usage.Digest(a) {
					return errors.New("GitHub read authorization is not enrolled")
				}
			}
			if g.Collector.Source.ReadPolicySHA256 != "" && g.Collector.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) {
				return ErrBlocked
			}
			return nil
		}
		q := w.CodexQuota
		if q == nil || w.GitHubRead != nil || q.OutputPath == "" || q.Binding.Host != "codex" || q.Binding.SourceID != q.Collector.SourceID || q.Binding.ID == "" || usage.ValidateBinding(q.Binding) != nil || usage.ValidatePolicy(q.Policy) != nil {
			return ErrBlocked
		}
		if q.Binding.HostVersion != q.Collector.Version || q.Binding.AuthMode == "" || q.Binding.SourceID != q.Collector.SourceID {
			return ErrBlocked
		}
		codexBindings := map[string]string{"source": usage.Digest(q.Collector.SourceID), "codex-binary": q.Collector.ExecutableSHA256, "codex-version": usage.Digest(q.Collector.Version), "native-account-proof": q.Collector.NativeAccountProofSHA256, "native-account-reference": usage.Digest(q.Collector.NativeAccountProofRef), "codex-home": q.Collector.HomeSHA256, "codex-environment": q.Collector.EnvironmentCensusSHA256}
		for name, value := range codexBindings {
			if w.Authority.Bindings[name] != value {
				return errors.New("Codex collector inputs are not enrolled")
			}
		}
		return nil
	}
	if w.Mode == "github-dispatch" || w.Mode == "github-reconcile" {
		if err := validateWorkerEnrollment(w.Authority, c.SecretEnv, now); err != nil {
			return err
		}
		g := w.GitHubWrite
		if g == nil || w.GitHubRead != nil || w.CodexQuota != nil || w.Calibration != nil || w.Experiment != nil || g.OutboxPath == "" || g.Owner == "" || g.Collector.Policy == nil || g.Credential.AuthReference != g.Collector.AuthReference || g.Credential.EnvReference == "" || c.SecretEnv[g.Credential.EnvReference] == "" || g.Credential.GrantRef == "" || g.Credential.PrincipalPseudonym != g.Collector.Source.PrincipalPseudonym || !sameStrings(g.Credential.Scopes, []string{"issues:write", "metadata:read"}) || tracker.ValidateSource(g.Collector.Source) != nil || tracker.ReadPolicyHash(g.Collector.Policy) == "" {
			return ErrBlocked
		}
		until, err := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
		if err != nil || !now.Before(until) {
			return ErrBlocked
		}
		if err = validateGitHubWriteBindings(w.Authority, g); err != nil {
			return err
		}
		return nil
	}
	if _, ok := c.SecretEnv[w.Authority.TokenRef]; !ok || w.Authority.TokenRef == "" {
		return ErrBlocked
	}
	if w.Verifier.AuthReference == "" || c.SecretEnv[w.Verifier.AuthReference] == "" || w.Verifier.Endpoint == "" || w.Verifier.VerifierID == "" || verification.ValidatePolicy(w.Verifier.Policy) != nil || !workerVerifierSpecExists(w.Verifier) {
		return ErrBlocked
	}
	var verifierSpec *verification.VerifierSpec
	for i := range w.Verifier.Policy.Verifiers {
		if w.Verifier.Policy.Verifiers[i].ID == w.Verifier.VerifierID {
			verifierSpec = &w.Verifier.Policy.Verifiers[i]
		}
	}
	if verifierSpec == nil {
		return ErrBlocked
	}
	probeRoute := verification.RemoteRoute{Endpoint: w.Verifier.Endpoint, AuthReference: w.Verifier.AuthReference, Resolver: verification.RemoteSecretResolverFunc(func(context.Context, string) (string, error) { return "", nil })}
	if _, err := verification.NewRemoteControls(probeRoute, w.Verifier.Proof, *verifierSpec); err != nil {
		return errors.New("protected verifier route or proof descriptor is invalid")
	}
	if w.Mode == "calibration" {
		if w.Calibration == nil || w.Experiment != nil || w.Calibration.ManifestPath == "" || w.Calibration.GrantPath == "" || w.Calibration.OutputPath == "" || len(w.Calibration.Bounds) == 0 {
			return ErrBlocked
		}
		key, err := base64.StdEncoding.DecodeString(w.ProtectedTrustKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return ErrBlocked
		}
		if err = verifyCalibrationFiles(*w.Calibration); err != nil {
			return err
		}
		if err = checkWorkerRecordBindings(w); err != nil {
			return err
		}
	} else if w.Mode == "experiment" {
		if w.Experiment == nil || w.Calibration != nil || w.Experiment.BundlePath == "" || w.Experiment.OutputPath == "" {
			return ErrBlocked
		}
		key, err := base64.StdEncoding.DecodeString(w.ProtectedTrustKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return ErrBlocked
		}
		if _, err = experiment.ReadFile(w.Experiment.BundlePath, experiment.Limits{}); err != nil {
			return ErrBlocked
		}
		if err = checkWorkerRecordBindings(w); err != nil {
			return err
		}
	} else {
		return errors.New("worker mode is unsupported")
	}
	return validateWorkerEnrollment(w.Authority, c.SecretEnv, now)
}

func validateGitHubWriteBindings(a WorkerAuthority, g *GitHubWriteRecords) error {
	if g == nil || g.Collector.Policy == nil || g.Collector.Source.Provider != "github" || g.Collector.Source.Origin != "https://api.github.com" || g.Collector.Source.Auth.Mode != "gh-session" || g.Collector.Source.Scope.Projects || g.Collector.Source.Scope.Relations || g.Collector.Source.Scope.Query != "" || len(g.Collector.Source.Scope.ItemIDs) != 1 || !strings.HasPrefix(g.Collector.Source.Scope.ItemIDs[0], "issue:") || g.Operation.OperationKind != "comment" || g.Operation.Change.Comment == nil {
		return ErrBlocked
	}
	if g.Collector.Policy.Version != g.Collector.Source.Version || len(g.Collector.Policy.Rules) != 1 || g.Collector.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) {
		return ErrBlocked
	}
	policyRule := g.Collector.Policy.Rules[0]
	if policyRule.Provider != "github" || policyRule.Origin != g.Collector.Source.Origin || policyRule.NativeTenantID != g.Collector.Source.NativeTenantID || policyRule.ContainerID != g.Collector.Source.ContainerID || policyRule.ContainerKey != g.Collector.Source.ContainerKey || policyRule.Auth != g.Collector.Source.Auth || policyRule.PrincipalPseudonym != g.Collector.Source.PrincipalPseudonym || policyRule.Deployment != nil {
		return ErrBlocked
	}
	issue := strings.TrimPrefix(g.Collector.Source.Scope.ItemIDs[0], "issue:")
	if issue == "" || g.IssueNumber != issue || g.Operation.Authority.Scope.RemoteID != g.ImmutableIssueID || g.Identity.RemoteID != g.ImmutableIssueID || g.Identity.ItemKind != "issue" || usage.Digest(g.Operation.Authority.Scope) != usage.Digest(g.Identity) || usage.Digest(g.Capability.Scope) != usage.Digest(g.Identity) {
		return ErrBlocked
	}
	for _, r := range issue {
		if r < '0' || r > '9' {
			return ErrBlocked
		}
	}
	if _, err := githubIssueNumberMapping(g.Collector.Source, g.ImmutableIssueID, g.IssueNumber, g.Operation.Authority.Scope.RemoteID); err != nil {
		return ErrBlocked
	}
	if !filepath.IsAbs(g.CommentAssetsRoot) || filepath.Clean(g.CommentAssetsRoot) != g.CommentAssetsRoot || !filepath.IsAbs(g.EvidenceRoot) || filepath.Clean(g.EvidenceRoot) != g.EvidenceRoot {
		return ErrBlocked
	}
	asset := g.Operation.Change.Comment
	if _, err := loadGitHubComment(g.CommentAssetsRoot, asset.Path, asset.SHA256); err != nil {
		return ErrBlocked
	}
	if requireRealDirectory(g.EvidenceRoot) != nil {
		return ErrBlocked
	}
	base := githubcollector.Authorization{SourceSHA256: tracker.ScopeDigest(g.Collector.Source), PolicySHA256: tracker.ReadPolicyHash(g.Collector.Policy), Origin: g.Collector.Source.Origin, Principal: g.Credential.PrincipalPseudonym, GrantRef: g.Credential.GrantRef, ItemIDs: append([]string(nil), g.Collector.Source.Scope.ItemIDs...)}
	read := base
	read.Method = "GET"
	read.Path = "/repos/" + g.Collector.Source.ContainerKey
	read.Scopes = []string{"metadata:read"}
	write := base
	write.Method = "POST"
	write.Path = "/repos/" + g.Collector.Source.ContainerKey + "/issues/" + g.IssueNumber + "/comments"
	write.Scopes = append([]string(nil), g.Credential.Scopes...)
	for _, authz := range []githubcollector.Authorization{read, write} {
		key := "github-read:" + usage.Digest(authz)
		if authz.Method == "POST" {
			key = "github-write:" + usage.Digest(authz)
		}
		if a.Bindings[key] != usage.Digest(authz) {
			return errors.New("exact GitHub read/write route is not enrolled")
		}
	}
	if a.Bindings["github-operation-config"] != usage.Digest(*g) {
		return errors.New("GitHub operation config is not root enrolled")
	}
	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func buildCollectionWorker(c Config, w *WorkerConfig, configPath, rootPath string, now time.Time) (*WorkerRun, error) {
	remote, err := buildRemote(c, w, now)
	if err != nil {
		return nil, err
	}
	run := func(runCtx context.Context, out, errout io.Writer) int {
		if runCtx == nil {
			return 1
		}
		var writeErr error
		switch w.Mode {
		case "github-capture":
			g := w.GitHubRead
			preflight, cancel := context.WithTimeout(runCtx, 5*time.Second)
			_, e := remote.Client.Call(preflight, authority.Request{Action: "check", Bindings: cloneStringMap(remote.Bindings)})
			cancel()
			if e != nil {
				fmt.Fprintln(errout, "protected GitHub enrollment is not current")
				return 1
			}
			resolver := githubcollector.TokenResolverFunc(func(ctx context.Context, ref string) (githubcollector.Token, error) {
				if ctx == nil || ref != g.Credential.AuthReference {
					return githubcollector.Token{}, ErrBlocked
				}
				secret, e := resolveSecret(c, g.Credential.EnvReference)
				if e != nil {
					return githubcollector.Token{}, ErrBlocked
				}
				until, e := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
				if e != nil || !time.Now().Before(until) {
					return githubcollector.Token{}, ErrBlocked
				}
				return githubcollector.Token{Value: secret, PrincipalPseudonym: g.Credential.PrincipalPseudonym, GrantRef: g.Credential.GrantRef, Scopes: append([]string(nil), g.Credential.Scopes...), ValidUntil: until}, nil
			})
			reader, e := githubcollector.New(g.Collector, resolver, authority.GitHubAuthority{Remote: remote})
			if e != nil {
				fmt.Fprintln(errout, "protected GitHub collector unavailable")
				return 1
			}
			capture, e := reader.Collect(runCtx, "capture")
			if e != nil {
				fmt.Fprintln(errout, "protected GitHub capture failed")
				return 1
			}
			writeErr = evidence.WriteJSON(g.OutputPath, "github-capture.json", capture, configPath, rootPath, w.RuntimeRoot)
		case "codex-quota":
			q := w.CodexQuota
			factory, e := codexcollector.New(q.Collector, authority.CollectorAuthority{Remote: remote}, authority.CollectorAuthority{Remote: remote})
			if e != nil {
				fmt.Fprintln(errout, "protected Codex collector unavailable")
				return 1
			}
			observation, payload, e := factory.Collect(runCtx)
			if e != nil {
				fmt.Fprintln(errout, "protected Codex quota capture failed")
				return 1
			}
			if observation.Ordinal == 0 || observation.Ordinal > uint64(^uint64(0)>>1) {
				fmt.Fprintln(errout, "protected Codex capture order invalid")
				return 1
			}
			export, report := usagecollectors.NormalizeCapture("codex", payload, q.Binding, q.Policy, time.Now().UTC(), int64(observation.Ordinal))
			bundle := struct {
				Version  string       `json:"version"`
				Ordinal  uint64       `json:"ordinal"`
				Revision int64        `json:"revision"`
				LeaseID  string       `json:"leaseId"`
				SourceID string       `json:"sourceId"`
				Export   usage.Export `json:"export"`
				Report   usage.Report `json:"report"`
			}{"aok-protected-quota-capture/v1", observation.Ordinal, observation.Revision, observation.LeaseID, observation.SourceID, export, report}
			writeErr = evidence.WriteJSON(q.OutputPath, "codex-quota.json", bundle, configPath, rootPath, w.RuntimeRoot)
		}
		if writeErr != nil {
			fmt.Fprintln(errout, "protected capture output failed")
			return 1
		}
		fmt.Fprintln(out, "protected capture completed")
		return 0
	}
	return &WorkerRun{run: run}, nil
}

func buildGitHubWriteWorker(c Config, w *WorkerConfig, now time.Time) (*WorkerRun, error) {
	g := w.GitHubWrite
	remote, err := buildRemote(c, w, now)
	if err != nil {
		return nil, err
	}
	resolver := githubcollector.TokenResolverFunc(func(ctx context.Context, ref string) (githubcollector.Token, error) {
		if ctx == nil || g == nil || ref != g.Credential.AuthReference {
			return githubcollector.Token{}, ErrBlocked
		}
		secret, e := resolveSecret(c, g.Credential.EnvReference)
		if e != nil {
			return githubcollector.Token{}, ErrBlocked
		}
		until, e := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
		if e != nil || !time.Now().Before(until) {
			return githubcollector.Token{}, ErrBlocked
		}
		return githubcollector.Token{Value: secret, PrincipalPseudonym: g.Credential.PrincipalPseudonym, GrantRef: g.Credential.GrantRef, Scopes: append([]string(nil), g.Credential.Scopes...), ValidUntil: until}, nil
	})
	ghAuth := authority.GitHubAuthority{Remote: remote}
	writer, err := buildGitHubWriterWithTransport(g, resolver, ghAuth, ghAuth, nil)
	if err != nil {
		return nil, errors.New("protected GitHub writer route invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	activation, err := operations.Bind(ctx, remote, remote, remote.Epoch.Authority, remote.Epoch.Holder, remote.Epoch.ScopeSHA256, w.Authority.ActivationProof, time.Now)
	if err != nil {
		return nil, errors.New("protected GitHub activation denied")
	}
	verifyEvidence := func(dep writeback.EvidenceDependency) error {
		found := false
		for _, candidate := range g.Grant.EvidenceDependencies {
			if usage.Digest(candidate) == usage.Digest(dep) {
				found = true
				break
			}
		}
		if !found || remote.Bindings["github-evidence:"+dep.Digest] != dep.Digest {
			return errors.New("evidence is outside protected GitHub grant")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, e := remote.Client.Call(ctx, authority.Request{Action: "check", Bindings: cloneStringMap(remote.Bindings)})
		if e != nil {
			return errors.New("protected evidence attestation is not current")
		}
		return nil
	}
	dispatch := cli.TrackerDispatchInputs{Operation: g.Operation, Capability: g.Capability, Identity: g.Identity, Grant: g.Grant, Policy: g.Policy, Revocation: g.Revocation, Current: g.Current, Owner: g.Owner}
	reconcile := cli.TrackerReconciliationInputs{Operation: g.Operation, Identity: g.Identity, Policy: g.Policy, Report: g.Reconciliation}
	services, err := githubcollector.Services(githubcollector.ServicesConfig{OperationID: g.Operation.OperationID, OperationDigest: g.Operation.OperationDigest, Dispatch: dispatch, Reconciliation: reconcile, Writer: writer, Activation: activation, RouteScope: activation.ScopeSHA256(), EvidenceVerifier: verifyEvidence})
	if err != nil {
		return nil, errors.New("protected GitHub operation services invalid")
	}
	args := []string{"tracker", "dispatch", "--outbox", g.OutboxPath, "--operation", g.Operation.OperationID, "--once"}
	if w.Mode == "github-reconcile" {
		args = []string{"tracker", "reconcile", "--outbox", g.OutboxPath, "--operation", g.Operation.OperationID}
	}
	return &WorkerRun{services: services, args: args}, nil
}

// buildGitHubWriterWithTransport is the single bootstrap constructor used by
// the installed worker and its fake-transport conformance tests. The transport
// parameter is kept out of signed JSON and production callers always pass nil.
func buildGitHubWriterWithTransport(g *GitHubWriteRecords, resolver githubcollector.TokenResolver, readAuthority githubcollector.Authority, writeAuthority githubcollector.WriteAuthority, roundTripper http.RoundTripper) (connector.Writer, error) {
	if g == nil || resolver == nil || readAuthority == nil || writeAuthority == nil || g.Collector.Policy == nil || g.Operation.Change.Comment == nil {
		return nil, ErrBlocked
	}
	if g.Collector.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) {
		return nil, ErrBlocked
	}
	if _, err := githubIssueNumberMapping(g.Collector.Source, g.ImmutableIssueID, g.IssueNumber, g.Operation.Authority.Scope.RemoteID); err != nil {
		return nil, err
	}
	if _, err := loadGitHubComment(g.CommentAssetsRoot, g.Operation.Change.Comment.Path, g.Operation.Change.Comment.SHA256); err != nil {
		return nil, err
	}
	if requireRealDirectory(g.EvidenceRoot) != nil {
		return nil, ErrBlocked
	}
	credentialResolver := providers.CredentialResolverFunc(func(ctx context.Context, ref string) (providers.Credential, error) {
		if ctx == nil || ref != g.Credential.AuthReference {
			return providers.Credential{}, ErrBlocked
		}
		token, err := resolver.Resolve(ctx, ref)
		if err != nil || token.Value == "" || token.GrantRef != g.Credential.GrantRef || token.PrincipalPseudonym != g.Credential.PrincipalPseudonym || !time.Now().Before(token.ValidUntil) || !sameStrings(token.Scopes, []string{"issues:write", "metadata:read"}) {
			return providers.Credential{}, ErrBlocked
		}
		base := githubcollector.Authorization{SourceSHA256: tracker.ScopeDigest(g.Collector.Source), PolicySHA256: tracker.ReadPolicyHash(g.Collector.Policy), Origin: g.Collector.Source.Origin, Principal: token.PrincipalPseudonym, GrantRef: token.GrantRef, Scopes: append([]string(nil), token.Scopes...), ItemIDs: append([]string(nil), g.Collector.Source.Scope.ItemIDs...)}
		identity := base
		identity.Method = "GET"
		identity.Path = "/repos/" + g.Collector.Source.ContainerKey
		identity.Scopes = []string{"metadata:read"}
		write := base
		write.Method = "POST"
		write.Path = "/repos/" + g.Collector.Source.ContainerKey + "/issues/" + g.IssueNumber + "/comments"
		if err = readAuthority.CheckRead(ctx, identity); err != nil {
			return providers.Credential{}, ErrBlocked
		}
		if err = writeAuthority.CheckWrite(ctx, write); err != nil {
			return providers.Credential{}, ErrBlocked
		}
		return providers.Credential{Token: token.Value}, nil
	})
	mapping, err := githubIssueNumberMapping(g.Collector.Source, g.ImmutableIssueID, g.IssueNumber, g.Operation.Authority.Scope.RemoteID)
	if err != nil {
		return nil, err
	}
	return providers.NewWriter(providers.Route{Source: g.Collector.Source, AuthReference: g.Credential.AuthReference, Resolver: credentialResolver, RoundTripper: roundTripper}, providers.WriterOptions{CommentLoader: func(path, digest string) ([]byte, error) {
		return loadGitHubComment(g.CommentAssetsRoot, path, digest)
	}, GitHubIssueNumbers: mapping, EvidenceSink: githubWriteEvidenceSink(g.EvidenceRoot)})
}

func checkWorkerRecordBindings(w *WorkerConfig) error {
	if w == nil {
		return ErrBlocked
	}
	expected := map[string]string{"run-id": usage.Digest(w.RunID), "witness-proof": usage.Hash(w.Authority.ActivationProof)}
	switch w.Mode {
	case "calibration":
		r := w.Calibration
		if r == nil {
			return ErrBlocked
		}
		expected["calibration-manifest"] = usage.Digest(r.Manifest)
		expected["calibration-grant"] = usage.Digest(r.Grant)
		expected["calibration-request"] = usage.Digest(r.Request)
		expected["calibration-bounds"] = usage.Hash(r.Bounds)
	case "experiment":
		r := w.Experiment
		if r == nil {
			return ErrBlocked
		}
		expected["experiment-bundle"] = usage.Digest(r.Bundle)
		expected["readiness-route"] = usage.Digest(r.Readiness.Route)
		expected["readiness-policy"] = usage.Digest(r.Readiness.Policy)
		expected["readiness-proof"] = usage.Hash(r.Readiness.Proof)
	default:
		return ErrBlocked
	}
	expected["verifier-policy"] = usage.Digest(w.Verifier.Policy)
	expected["verifier-proof"] = usage.Digest(w.Verifier.Proof)
	expected["verifier-endpoint"] = usage.Digest(w.Verifier.Endpoint)
	for binding, digest := range expected {
		if w.Authority.Bindings[binding] != digest || w.Authority.Enrollment.Bindings[binding] != digest {
			return errors.New("worker record does not match enrolled authority binding")
		}
	}
	return nil
}

func validateWorkerEnrollment(a WorkerAuthority, refs map[string]string, now time.Time) error {
	operator, err := decodeKey(a.OperatorPublicKey, ed25519.PublicKeySize)
	if err != nil || a.Enrollment.ID != a.EnrollmentID || a.Enrollment.Scope != a.Scope || a.Enrollment.Activation != a.Epoch || len(a.Bindings) == 0 || len(a.Bindings) != len(a.Enrollment.Bindings) {
		return ErrBlocked
	}
	for k, v := range a.Enrollment.Bindings {
		if a.Bindings[k] != v {
			return ErrBlocked
		}
	}
	if _, ok := refs[a.TokenRef]; !ok || a.Endpoint == "" || a.ServerID == "" {
		return ErrBlocked
	}
	if err = authority.ValidateEnrollment(a.Enrollment, ed25519.PublicKey(operator), a.Issuers, now); err != nil {
		return errors.New("protected worker authority enrollment invalid or expired")
	}
	responseKey, err := decodeKey(a.ResponsePublicKey, ed25519.PublicKeySize)
	if err != nil || len(a.ActivationProof) > 1<<20 {
		return ErrBlocked
	}
	_ = responseKey
	return nil
}

func verifyWorkerRecordFiles(w WorkerConfig) error {
	switch w.Mode {
	case "calibration":
		if err := verifyCalibrationFiles(*w.Calibration); err != nil {
			return err
		}
	case "experiment":
		actual, err := experiment.ReadFile(w.Experiment.BundlePath, experiment.Limits{})
		if err != nil || usage.Digest(actual) != usage.Digest(w.Experiment.Bundle) {
			return errors.New("experiment input differs from signed worker record")
		}
	}
	return nil
}

func verifyCalibrationFiles(r CalibrationRecords) error {
	manifestBytes, err := readSafe(r.ManifestPath, 2<<20, false)
	if err != nil {
		return ErrBlocked
	}
	grantBytes, err := readSafe(r.GrantPath, 1<<20, false)
	if err != nil {
		return ErrBlocked
	}
	var m pilot.Manifest
	var g pilot.CalibrationGrant
	if usage.Decode(manifestBytes, &m) != nil || usage.Digest(m) != usage.Digest(r.Manifest) || usage.Decode(grantBytes, &g) != nil || usage.Digest(g) != usage.Digest(r.Grant) {
		return errors.New("calibration inputs differ from signed worker records")
	}
	return nil
}

func buildRemote(c Config, w *WorkerConfig, now time.Time) (*authority.Remote, error) {
	if err := validateWorkerEnrollment(w.Authority, c.SecretEnv, now); err != nil {
		return nil, err
	}
	key, err := decodeKey(w.Authority.ResponsePublicKey, ed25519.PublicKeySize)
	if err != nil {
		return nil, ErrBlocked
	}
	client := authority.Client{Endpoint: w.Authority.Endpoint, ServerID: w.Authority.ServerID, EnrollmentID: w.Authority.EnrollmentID, Scope: w.Authority.Scope, PublicKey: ed25519.PublicKey(key), Token: func(ctx context.Context) (string, error) { return resolveSecret(c, w.Authority.TokenRef) }}
	remote := &authority.Remote{Client: client, Bindings: cloneStringMap(w.Authority.Bindings), Epoch: w.Authority.Epoch}
	if w.Experiment != nil {
		policy := w.Experiment.Readiness.Policy
		remote.ReadinessPolicy = &policy
	}
	return remote, nil
}

func calibrationDispatch(w WorkerConfig, remote *authority.Remote) workflow.CalibrationDispatch {
	r := w.Calibration
	key, _ := base64.StdEncoding.DecodeString(w.ProtectedTrustKey)
	var exactBounds selection.ExplicitUpperBound
	_ = usage.Decode(r.Bounds, &exactBounds)
	return workflow.CalibrationDispatch{Manifest: r.Manifest, Grant: r.Grant, Request: r.Request, TrustedKey: ed25519.PublicKey(key), Guards: authority.Guard{Remote: remote}, Consumption: remote, Bounds: exactBounds, Services: remote, RunID: w.RunID}
}

func experimentDispatch(w WorkerConfig, remote *authority.Remote) workflow.ExperimentDispatch {
	r := w.Experiment
	key, _ := base64.StdEncoding.DecodeString(w.ProtectedTrustKey)
	trust := experiment.TrustPolicy{Principal: r.Bundle.Grant.Issuer, Role: r.Bundle.Grant.Role, PublicKey: ed25519.PublicKey(key), MaxTTL: 24 * time.Hour, Revocations: remote}
	validators := map[string]readinessv2.Validator{}
	for _, requirement := range r.Readiness.Policy.Requirements {
		validators[requirement.Validator] = remote
	}
	return workflow.ExperimentDispatch{Bundle: r.Bundle, Trust: trust, Consumption: remote, Services: remote, Readiness: workflow.TrialReadiness{Route: r.Readiness.Route, Policy: r.Readiness.Policy, PolicyAuthority: remote, Validators: validators, Proof: append(json.RawMessage(nil), r.Readiness.Proof...)}, RunID: w.RunID}
}

func workerVerifierSpecExists(v WorkerVerifier) bool {
	for _, spec := range v.Policy.Verifiers {
		if spec.ID == v.VerifierID && spec.Kind == "command" {
			return true
		}
	}
	return false
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func workerVerifierControls(c Config, protected WorkerVerifier) func(context.Context, verification.Candidate, verification.VerificationPolicy, verification.VerifierSpec) (verification.Controls, error) {
	return func(ctx context.Context, candidate verification.Candidate, policy verification.VerificationPolicy, spec verification.VerifierSpec) (verification.Controls, error) {
		if ctx == nil || usage.Digest(policy) != usage.Digest(protected.Policy) || spec.ID != protected.VerifierID {
			return nil, ErrBlocked
		}
		found := false
		for _, pinned := range protected.Policy.Verifiers {
			if pinned.ID == spec.ID && usage.Digest(pinned) == usage.Digest(spec) {
				found = true
			}
		}
		if !found {
			return nil, ErrBlocked
		}
		resolver := verification.RemoteSecretResolverFunc(func(context.Context, string) (string, error) {
			return resolveSecret(c, protected.AuthReference)
		})
		route := verification.RemoteRoute{Endpoint: protected.Endpoint, AuthReference: protected.AuthReference, Resolver: resolver}
		return verifier.NewUploadingControls(route, protected.Proof, spec, candidate)
	}
}

func (r *WorkerRun) Args() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.args...)
}
