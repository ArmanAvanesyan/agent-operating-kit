// Package enrollment prepares offline, non-authorizing operator handoff files.
// It never resolves credentials, signs records, starts services or invokes hosts.
package enrollment

import "time"

const Version = "aok-enrollment-preparation/v1"

func SecretReferenceIDs() []string {
	return []string{"github-read-token", "github-write-token", "anchor-bearer", "authority-bearer", "verifier-bearer", "anchor-response-key", "authority-response-key"}
}

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Need struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Stage    string `json:"stage"`
	Contract string `json:"contract"`
	Check    string `json:"check"`
}
type Manifest struct {
	Version  string  `json:"version"`
	Purpose  string  `json:"purpose"`
	Drafts   []Asset `json:"drafts"`
	Required []Need  `json:"required"`
}
type Inventory struct {
	Version               string            `json:"version"`
	Repository            string            `json:"repository"`
	RepositoryID          string            `json:"repositoryId"`
	TenantID              string            `json:"tenantId"`
	ImmutableIssueID      string            `json:"immutableIssueId"`
	IssueNumber           string            `json:"issueNumber"`
	Platform              string            `json:"platform"`
	CodexExecutableSHA256 string            `json:"codexExecutableSha256"`
	GHExecutableSHA256    string            `json:"ghExecutableSha256"`
	GHVersion             string            `json:"ghVersion"`
	ModelSelector         string            `json:"modelSelector"`
	ConfigurationID       string            `json:"configurationId"`
	ProfileSHA256         string            `json:"profileSha256"`
	AccountRef            string            `json:"accountRef"`
	PoolID                string            `json:"poolId"`
	Operator              string            `json:"operator"`
	RecoveryOwner         string            `json:"recoveryOwner"`
	SecretReferences      map[string]string `json:"secretReferences"`
}
type Finding struct {
	ID     string `json:"id"`
	Path   string `json:"path,omitempty"`
	Stage  string `json:"stage,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type Report struct {
	Version              string    `json:"version"`
	AsOf                 time.Time `json:"asOf"`
	Status               string    `json:"status"`
	OfflineOnly          bool      `json:"offlineOnly"`
	AuthorizationGranted bool      `json:"authorizationGranted"`
	ExecutionReady       bool      `json:"executionReady"`
	ProductionEligible   bool      `json:"productionEligible"`
	Findings             []Finding `json:"findings"`
}

// Requirements is compiled, rather than selected by untrusted package data.
// Evidence presence is never equivalent to independent live qualification.
func Requirements() []Need {
	return []Need{
		{"public-root", "inputs/bootstrap-root.ed25519", "qualification", "raw 32-byte Ed25519 public key", "public-key"},
		{"credential-provisioning", "inputs/credential-provisioning.json", "qualification", "external non-secret provisioning evidence", "evidence"},
		{"host-policy", "inputs/host-policy.json", "qualification", "P6 host policy and signed control assets", "evidence"},
		{"native-identity", "inputs/native-identity.json", "qualification", "independent account/HOME/environment measurements", "evidence"},
		{"capacity", "inputs/capacity.json", "qualification", "P3 observations, policies, reservation and full-matrix upper bound", "evidence"},
		{"anchor-retention", "inputs/anchor-retention.json", "qualification", "independent retention/CAS/recovery evidence", "evidence"},
		{"anchor-config", "signed/anchor.signed.json", "qualification", "anchor", "bootstrap"},
		{"verifier-config", "signed/verifier.signed.json", "qualification", "verifier", "bootstrap"},
		{"qualify-github-read", "signed/qualify-github-read.signed.json", "qualification", "qualification:github-read", "bootstrap"},
		{"qualify-codex-quota", "signed/qualify-codex-quota.signed.json", "qualification", "qualification:codex-quota", "bootstrap"},
		{"qualify-host", "signed/qualify-host-conformance.signed.json", "qualification", "qualification:host-conformance", "bootstrap"},
		{"qualify-verifier", "signed/qualify-verifier-conformance.signed.json", "qualification", "qualification:verifier-conformance", "bootstrap"},
		{"qualify-comment", "signed/qualify-github-comment.signed.json", "qualification", "qualification:github-comment", "bootstrap"},
		{"live-proofs", "inputs/live-proofs.json", "calibration", "13 independent live gate proofs with approved issuer registry", "evidence"},
		{"authority-config", "signed/authority.signed.json", "calibration", "authority", "bootstrap"},
		{"calibration-worker", "signed/calibration.signed.json", "calibration", "worker:calibration", "bootstrap"},
		{"verify-worker", "signed/verify-candidate.signed.json", "postrun", "worker:verify-candidate", "bootstrap"},
		{"accept-worker", "signed/accept-candidate.signed.json", "postrun", "worker:accept-candidate", "bootstrap"},
		{"handoff-worker", "signed/handoff-candidate.signed.json", "postrun", "worker:handoff-candidate", "bootstrap"},
		{"dispatch-worker", "signed/github-dispatch.signed.json", "postrun", "worker:github-dispatch", "bootstrap"},
		{"reconcile-worker", "signed/github-reconcile.signed.json", "postrun", "worker:github-reconcile", "bootstrap"},
		{"qualified-cohorts", "inputs/qualified-cohorts.json", "ordinary-pilot", "independently accepted P4 quality/demand evidence", "evidence"},
		{"experiment-worker", "signed/experiment.signed.json", "ordinary-pilot", "worker:experiment", "bootstrap"},
	}
}
