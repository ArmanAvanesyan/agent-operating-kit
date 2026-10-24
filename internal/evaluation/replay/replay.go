// Package replay evaluates frozen routing projections against recorded labels
// and explicit synthetic quota traces. It makes no independent quality claim.
package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	eval "github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

const Version = eval.Version

type Dataset struct {
	Version              string  `json:"version"`
	Kind                 string  `json:"kind"` // recorded-state or synthetic-closed-loop
	PilotOnly            bool    `json:"pilotOnly"`
	FrozenPolicySHA256   string  `json:"frozenPolicySha256"`
	FrozenConfigSHA256   string  `json:"frozenConfigSha256"`
	FrozenEstimateSHA256 string  `json:"frozenEstimateSha256"`
	FrozenPolicyPath     string  `json:"frozenPolicyPath"`
	FrozenConfigPath     string  `json:"frozenConfigPath"`
	FrozenEstimatePath   string  `json:"frozenEstimatePath"`
	FrozenAt             string  `json:"frozenAt"`
	AuditAt              string  `json:"auditAt"`
	Tasks                []Task  `json:"tasks"`
	Runs                 []Run   `json:"runs"`
	Events               []Event `json:"events,omitempty"`
	seal                 string
	sourceSHA256         string
	loaded               bool
}
type Task struct {
	ID                 string `json:"id"`
	Class              string `json:"class"`
	Repository         string `json:"repository"`
	Family             string `json:"family"`
	NearDuplicateGroup string `json:"nearDuplicateGroup"`
	Partition          string `json:"partition"`
}
type Run struct {
	TaskID                   string       `json:"taskId"`
	Policy                   string       `json:"policy"`
	Configuration            string       `json:"configuration"`
	Repetition               int          `json:"repetition"`
	At                       int64        `json:"at"`
	Pool                     string       `json:"pool"`
	Decision                 string       `json:"decision"` // accept, defer, abstain
	Gates                    Gates        `json:"gates"`
	Capacity                 json.Number  `json:"capacity"`
	Reserved                 json.Number  `json:"reserved"`
	Required                 json.Number  `json:"required"`
	QuotaStateKnown          bool         `json:"quotaStateKnown"`
	Outcome                  string       `json:"outcome"` // accepted, rejected, interrupted, unknown
	Cost                     *json.Number `json:"cost,omitempty"`
	Seconds                  *json.Number `json:"seconds,omitempty"`
	QuotaBlocked             bool         `json:"quotaBlocked"`
	DeadlineMiss             bool         `json:"deadlineMiss"`
	CoverageStalenessSeconds *json.Number `json:"coverageStalenessSeconds,omitempty"`
	PredictionCovered        *bool        `json:"predictionCovered,omitempty"`
	Switch                   bool         `json:"switch"`
	EvidenceLabel            string       `json:"evidenceLabel"` // recorded or supplied-synthetic
}
type Event struct {
	At     int64       `json:"at"`   // virtual seconds, monotone; passage alone never replenishes
	Type   string      `json:"type"` // consume, reserve, release, replenish, external-consume
	Pool   string      `json:"pool"`
	Amount json.Number `json:"amount"`
}

// Gates are the shared eligibility inputs supplied for every policy replay.
// A false common gate cannot be overridden by policy utility or quota.
type Gates struct {
	Capability    bool `json:"capability"`
	Authorization bool `json:"authorization"`
	QualityFloor  bool `json:"qualityFloor"`
	DataFresh     bool `json:"dataFresh"`
}
type CapacityState struct {
	Capacity json.Number `json:"capacity"`
	Reserved json.Number `json:"reserved"`
	Required json.Number `json:"required"`
	Known    bool        `json:"known"`
}
type Admission struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// Admit applies common safety gates, then the policy's capacity rule. The
// unsafe flag is an explicitly counterfactual capacity ablation.
func sharedAdmit(policy string, gates Gates, state CapacityState, unsafe bool) Admission {
	capacity, capErr := exactNonnegative(state.Capacity)
	reserved, resErr := exactNonnegative(state.Reserved)
	required, reqErr := exactNonnegative(state.Required)
	return sharedAdmitExact(policy, gates, state.Known, capacity, reserved, required, capErr, resErr, reqErr, unsafe)
}

func sharedAdmitExact(policy string, gates Gates, known bool, capacity, reserved, required *big.Rat, capErr, resErr, reqErr error, unsafe bool) Admission {
	if !contains(Policies, policy) {
		return Admission{Reason: "unknown-policy"}
	}
	if !gates.Capability {
		return Admission{Reason: "capability"}
	}
	if !gates.Authorization {
		return Admission{Reason: "authorization"}
	}
	if !gates.QualityFloor {
		return Admission{Reason: "quality-floor"}
	}
	if !gates.DataFresh {
		return Admission{Reason: "stale-or-missing-data"}
	}
	if !unsafe && (policy == "quota-aware" || policy == "reservation-aware") {
		if !known || capErr != nil || resErr != nil {
			return Admission{Reason: "unknown-capacity"}
		}
		available := new(big.Rat).Set(capacity)
		if policy == "reservation-aware" {
			available.Sub(available, reserved)
		}
		if reqErr != nil {
			return Admission{Reason: "unknown-demand"}
		}
		if available.Cmp(required) < 0 {
			return Admission{Reason: "insufficient-capacity"}
		}
	}
	return Admission{Allowed: true, Reason: "eligible"}
}

// Admit is exported for command layers projecting selection decisions.
func Admit(policy string, gates Gates, state CapacityState, unsafe bool) Admission {
	return sharedAdmit(policy, gates, state, unsafe)
}

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func exactNumber(n json.Number) (*big.Rat, error) {
	s := string(n)
	if s == "" || len(s) > 128 || !decimalPattern.MatchString(s) {
		return nil, fmt.Errorf("invalid exact decimal")
	}
	if i := strings.LastIndexAny(s, "eE"); i >= 0 {
		exp, err := strconv.Atoi(s[i+1:])
		if err != nil || exp > 1000 || exp < -1000 {
			return nil, fmt.Errorf("decimal exponent out of bounds")
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("invalid exact decimal")
	}
	return r, nil
}
func exactNonnegative(n json.Number) (*big.Rat, error) {
	r, e := exactNumber(n)
	if e != nil {
		return nil, e
	}
	if r.Sign() < 0 {
		return nil, fmt.Errorf("negative decimal")
	}
	return r, nil
}
func sameExact(a, b json.Number) bool {
	if a == "" || b == "" {
		return a == b
	}
	x, e1 := exactNonnegative(a)
	y, e2 := exactNonnegative(b)
	return e1 == nil && e2 == nil && x.Cmp(y) == 0
}

type Metrics struct {
	Opportunities           int      `json:"opportunities"`
	AttemptedStarts         int      `json:"attemptedStarts"`
	AdmittedStarts          int      `json:"admittedStarts"`
	Attempts                int      `json:"attempts"`
	Accepted                int      `json:"accepted"`
	ConsequentialMisses     int      `json:"consequentialMisses"`
	AcceptedCompletionRate  *float64 `json:"acceptedCompletionRate"`
	AcceptedPerOpportunity  *float64 `json:"acceptedPerOpportunity"`
	CostPerAccepted         *float64 `json:"costPerAccepted"`
	SecondsPerAccepted      *float64 `json:"secondsPerAccepted"`
	UnknownCostAttempts     int      `json:"unknownCostAttempts"`
	UnknownCapacityAttempts int      `json:"unknownCapacityAttempts"`
	UnknownTimeAttempts     int      `json:"unknownTimeAttempts"`
	QuotaBlocked            int      `json:"quotaBlocked"`
	DeadlineMisses          int      `json:"deadlineMisses"`
	Abstentions             int      `json:"abstentions"`
	CoverageStalenessMean   *float64 `json:"coverageStalenessMean"`
	PredictionBoundCoverage *float64 `json:"predictionBoundCoverage"`
	Switches                int      `json:"switches"`
	OversubscriptionEvents  int      `json:"oversubscriptionEvents"`
	LabelSource             string   `json:"labelSource"`
	SafetyDenominator       bool     `json:"safetyDenominator"`
}
type Report struct {
	Version                         string             `json:"version"`
	DatasetKind                     string             `json:"datasetKind"`
	PilotOnly                       bool               `json:"pilotOnly"`
	DecisionProjectionSource        string             `json:"decisionProjectionSource"`
	SuppliedOutcomeLabelsUnverified bool               `json:"suppliedOutcomeLabelsUnverified"`
	AcceptanceEvidenceQualified     bool               `json:"acceptanceEvidenceQualified"`
	FrozenArtifactsVerified         bool               `json:"frozenArtifactsVerified"`
	FrozenPolicySHA256              string             `json:"frozenPolicySha256"`
	FrozenConfigSHA256              string             `json:"frozenConfigSha256"`
	FrozenEstimateSHA256            string             `json:"frozenEstimateSha256"`
	FrozenAt                        string             `json:"frozenAt"`
	AuditAt                         string             `json:"auditAt"`
	DatasetSHA256                   string             `json:"datasetSha256"`
	Metrics                         map[string]Metrics `json:"metrics"`
}
type Config struct {
	Partition              string `json:"partition"`
	UnsafeCapacityAblation bool   `json:"unsafeCapacityAblation"`
}
type Comparison struct {
	Version  string   `json:"version"`
	Report   Report   `json:"report"`
	Policies []string `json:"policies"`
}

var Policies = []string{"pinned", "quality-only", "cost-aware", "quota-aware", "reservation-aware"}

// RecordedLabel projects a verified outcome record into a replay label. The
// result is still only a replay label; this package does not attest acceptance.
func RecordedLabel(r eval.OutcomeRecord) string {
	switch r.TerminalOutcome {
	case "accepted":
		return "accepted"
	case "rejected":
		return "rejected"
	case "interrupted":
		return "interrupted"
	default:
		return "unknown"
	}
}

func Load(path string) (Dataset, error) {
	b, err := modelcatalog.ReadFile(path)
	if err != nil {
		return Dataset{}, err
	}
	var d Dataset
	if err = modelcatalog.Decode(b, &d); err != nil {
		return Dataset{}, err
	}
	if err = Validate(d); err != nil {
		return Dataset{}, err
	}
	root := filepath.Dir(path)
	for _, a := range []struct{ path, hash string }{{d.FrozenPolicyPath, d.FrozenPolicySHA256}, {d.FrozenConfigPath, d.FrozenConfigSHA256}, {d.FrozenEstimatePath, d.FrozenEstimateSHA256}} {
		asset, e := modelcatalog.ReadAsset(root, a.path)
		if e != nil {
			return Dataset{}, e
		}
		if digest(asset) != a.hash {
			return Dataset{}, fmt.Errorf("frozen artifact hash mismatch: %s", a.path)
		}
	}
	d.sourceSHA256 = digest(b)
	d.loaded = true
	d.seal = semanticSeal(d)
	return d, nil
}

func Validate(d Dataset) error {
	if d.Version != Version {
		return fmt.Errorf("unsupported replay version %q", d.Version)
	}
	if d.Kind != "recorded-state" && d.Kind != "synthetic-closed-loop" {
		return fmt.Errorf("invalid replay kind")
	}
	for _, h := range []string{d.FrozenPolicySHA256, d.FrozenConfigSHA256, d.FrozenEstimateSHA256} {
		if !validHash(h) {
			return fmt.Errorf("frozen policy, configuration and estimate hashes must be SHA256")
		}
	}
	for _, p := range []string{d.FrozenPolicyPath, d.FrozenConfigPath, d.FrozenEstimatePath} {
		if err := validateAssetPath(p); err != nil {
			return err
		}
	}
	frozen, ferr := time.Parse(time.RFC3339, d.FrozenAt)
	audit, aerr := time.Parse(time.RFC3339, d.AuditAt)
	if ferr != nil || aerr != nil || audit.Before(frozen) {
		return fmt.Errorf("valid freeze and audit timestamps are required, with audit at or after freeze")
	}
	if len(d.Tasks) == 0 || len(d.Runs) == 0 {
		return fmt.Errorf("tasks and runs are required")
	}
	tasks := map[string]Task{}
	repos, fams, dups := map[string]string{}, map[string]string{}, map[string]string{}
	for _, t := range d.Tasks {
		if t.ID == "" || t.Class == "" || t.Repository == "" || t.Family == "" || t.NearDuplicateGroup == "" {
			return fmt.Errorf("task identity fields are required")
		}
		if _, ok := tasks[t.ID]; ok {
			return fmt.Errorf("duplicate task %q", t.ID)
		}
		if t.Partition != "development" && t.Partition != "validation" && t.Partition != "locked-test" {
			return fmt.Errorf("invalid partition")
		}
		tasks[t.ID] = t
		for _, x := range []struct {
			m    map[string]string
			k, v string
		}{{repos, t.Repository, t.Partition}, {fams, t.Family, t.Partition}, {dups, t.NearDuplicateGroup, t.Partition}} {
			if x.k != "" {
				if prev, ok := x.m[x.k]; ok && prev != x.v {
					return fmt.Errorf("partition leakage for %q", x.k)
				}
				x.m[x.k] = x.v
			}
		}
	}
	seen := map[string]bool{}
	quotaProjection := map[string]CapacityState{}
	gateProjection := map[string]Gates{}
	timeProjection := map[string]int64{}
	for _, r := range d.Runs {
		if _, ok := tasks[r.TaskID]; !ok {
			return fmt.Errorf("run references unknown task")
		}
		if r.Policy == "" || r.Configuration == "" || r.Repetition < 1 || r.At < 0 {
			return fmt.Errorf("run identity fields are required")
		}
		if !contains(Policies, r.Policy) {
			return fmt.Errorf("unknown policy %q", r.Policy)
		}
		if r.Decision == "accept" && r.Gates == (Gates{}) {
			return fmt.Errorf("accepted projection requires explicit common gates")
		}
		if d.Kind == "synthetic-closed-loop" && r.Pool == "" {
			return fmt.Errorf("synthetic run pool is required")
		}
		key := fmt.Sprintf("%s/%s/%s/%d", r.TaskID, r.Policy, r.Configuration, r.Repetition)
		if seen[key] {
			return fmt.Errorf("duplicate run %s", key)
		}
		seen[key] = true
		if d.Kind == "recorded-state" {
			group := fmt.Sprintf("%s/%d", r.TaskID, r.Repetition)
			state := CapacityState{Capacity: r.Capacity, Reserved: r.Reserved, Known: r.QuotaStateKnown}
			if prev, ok := quotaProjection[group]; ok && (!sameExact(prev.Capacity, state.Capacity) || !sameExact(prev.Reserved, state.Reserved) || prev.Known != state.Known) {
				return fmt.Errorf("recorded quota state differs across policy projections for %s", group)
			} else {
				quotaProjection[group] = state
			}
			if prev, ok := gateProjection[group]; ok && prev != r.Gates {
				return fmt.Errorf("common gates differ across policy projections for %s", group)
			} else {
				gateProjection[group] = r.Gates
			}
			if prev, ok := timeProjection[group]; ok && prev != r.At {
				return fmt.Errorf("recorded quota observation time differs across policy projections for %s", group)
			} else {
				timeProjection[group] = r.At
			}
		}
		if r.Decision != "accept" && r.Decision != "defer" && r.Decision != "abstain" {
			return fmt.Errorf("invalid decision")
		}
		if r.Outcome != "accepted" && r.Outcome != "rejected" && r.Outcome != "interrupted" && r.Outcome != "unknown" {
			return fmt.Errorf("invalid outcome")
		}
		if r.EvidenceLabel != "recorded" && r.EvidenceLabel != "supplied-synthetic" {
			return fmt.Errorf("invalid evidence label")
		}
		if d.Kind == "recorded-state" && r.EvidenceLabel != "recorded" {
			return fmt.Errorf("recorded-state requires recorded labels")
		}
		if d.Kind == "synthetic-closed-loop" && r.EvidenceLabel != "supplied-synthetic" {
			return fmt.Errorf("synthetic replay requires supplied-synthetic labels")
		}
		for _, n := range []json.Number{r.Capacity, r.Reserved, r.Required} {
			if n != "" {
				if _, err := exactNonnegative(n); err != nil {
					return fmt.Errorf("quota values must be exact finite nonnegative decimals")
				}
			}
		}
		if r.QuotaStateKnown && (r.Capacity == "" || r.Reserved == "") {
			return fmt.Errorf("known quota state requires capacity and reservation values")
		}
		for _, v := range []*json.Number{r.Cost, r.Seconds, r.CoverageStalenessSeconds} {
			if v != nil {
				if _, err := exactNonnegative(*v); err != nil {
					return fmt.Errorf("metrics must be exact finite nonnegative decimals")
				}
			}
		}
	}
	if d.Kind == "recorded-state" && len(d.Events) > 0 {
		return fmt.Errorf("recorded-state cannot contain synthetic events")
	}
	if d.Kind == "synthetic-closed-loop" {
		var at int64
		knownPools := map[string]bool{}
		for i, e := range d.Events {
			if e.At < at || e.Pool == "" {
				return fmt.Errorf("invalid event")
			}
			if _, err := exactNonnegative(e.Amount); err != nil {
				return fmt.Errorf("event amounts must be exact finite nonnegative decimals")
			}
			if i > 0 && e.At < at {
				return fmt.Errorf("events must be time ordered")
			}
			at = e.At
			if e.Type != "consume" && e.Type != "reserve" && e.Type != "release" && e.Type != "replenish" && e.Type != "external-consume" {
				return fmt.Errorf("unknown event type")
			}
			if e.Type == "replenish" {
				knownPools[e.Pool] = true
			} else if !knownPools[e.Pool] {
				return fmt.Errorf("pool %q must have an explicit replenish event before use", e.Pool)
			}
		}
	}
	if d.PilotOnly {
		classes := map[string]int{}
		parts := map[string]int{}
		for _, t := range d.Tasks {
			classes[t.Class]++
			parts[t.Partition]++
		}
		if len(d.Tasks) != 48 || len(classes) != 6 {
			return fmt.Errorf("pilot requires 48 tasks across six classes")
		}
		for _, n := range classes {
			if n != 8 {
				return fmt.Errorf("pilot requires eight tasks per class")
			}
		}
		if parts["development"] != 24 || parts["validation"] != 12 || parts["locked-test"] != 12 {
			return fmt.Errorf("pilot partitions must be 24/12/12")
		}
		attempts := map[string]bool{}
		configs := map[string]map[string]map[int]bool{}
		for _, r := range d.Runs {
			attempts[fmt.Sprintf("%s/%s/%d", r.TaskID, r.Configuration, r.Repetition)] = true
			if configs[r.TaskID] == nil {
				configs[r.TaskID] = map[string]map[int]bool{}
			}
			if configs[r.TaskID][r.Configuration] == nil {
				configs[r.TaskID][r.Configuration] = map[int]bool{}
			}
			configs[r.TaskID][r.Configuration][r.Repetition] = true
		}
		if len(attempts) < 288 || len(attempts) > 432 {
			return fmt.Errorf("pilot requires 288 to 432 attempts")
		}
		for _, t := range d.Tasks {
			if len(configs[t.ID]) < 2 || len(configs[t.ID]) > 3 {
				return fmt.Errorf("pilot requires two or three configurations per task")
			}
			for _, reps := range configs[t.ID] {
				if len(reps) != 3 || !reps[1] || !reps[2] || !reps[3] {
					return fmt.Errorf("pilot requires three repetitions per configuration")
				}
			}
		}
	}
	return nil
}

func validateAssetPath(p string) error {
	if p == "" || filepath.IsAbs(p) || filepath.VolumeName(p) != "" || strings.Contains(p, "\\") || strings.Contains(p, ":") {
		return fmt.Errorf("frozen artifact path must be portable and relative")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("frozen artifact path must use canonical relative components")
		}
	}
	return nil
}
func digest(b []byte) string        { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func semanticSeal(d Dataset) string { b, _ := json.Marshal(d); return digest(b) }

func validHash(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func Replay(d Dataset, c Config) (Report, error) {
	if err := Validate(d); err != nil {
		return Report{}, err
	}
	if c.Partition == "" {
		c.Partition = "locked-test"
	}
	if c.Partition != "development" && c.Partition != "validation" && c.Partition != "locked-test" {
		return Report{}, fmt.Errorf("invalid partition")
	}
	selected := map[string]bool{}
	for _, t := range d.Tasks {
		if t.Partition == c.Partition {
			selected[t.ID] = true
		}
	}
	by := map[string][]Run{}
	for _, r := range d.Runs {
		if selected[r.TaskID] {
			by[r.Policy] = append(by[r.Policy], r)
		}
	}
	datasetHash := d.sourceSHA256
	if datasetHash == "" {
		b, _ := json.Marshal(d)
		datasetHash = digest(b)
	}
	verified := d.loaded && d.seal != "" && d.seal == semanticSeal(d)
	out := Report{Version: Version, DatasetKind: d.Kind, PilotOnly: d.PilotOnly, DecisionProjectionSource: "supplied-projection", SuppliedOutcomeLabelsUnverified: true, AcceptanceEvidenceQualified: false, FrozenArtifactsVerified: verified, FrozenPolicySHA256: d.FrozenPolicySHA256, FrozenConfigSHA256: d.FrozenConfigSHA256, FrozenEstimateSHA256: d.FrozenEstimateSHA256, FrozenAt: d.FrozenAt, AuditAt: d.AuditAt, DatasetSHA256: datasetHash, Metrics: map[string]Metrics{}}
	for policy, rows := range by {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].At < rows[j].At })
		m := Metrics{Attempts: len(rows), Opportunities: len(rows), SafetyDenominator: !c.UnsafeCapacityAblation, LabelSource: d.Kind}
		cost, secs, stale := new(big.Rat), new(big.Rat), new(big.Rat)
		var costN, secN, staleN, predN, predHit int
		admitted, blocked, unknownCapacity, overs := replayTimeline(policy, rows, d.Events, d.Kind == "synthetic-closed-loop", c.UnsafeCapacityAblation)
		m.OversubscriptionEvents = overs
		for i, r := range rows {
			ok := admitted[i]
			if r.Decision == "accept" {
				m.AttemptedStarts++
			}
			if ok {
				m.AdmittedStarts++
			}
			if unknownCapacity[i] {
				m.UnknownCapacityAttempts++
			}
			if r.Decision == "abstain" {
				m.Abstentions++
			}
			if r.QuotaBlocked || blocked[i] {
				m.QuotaBlocked++
			}
			if r.DeadlineMiss && ok {
				m.DeadlineMisses++
			}
			if r.Switch {
				m.Switches++
			}
			if r.Outcome == "accepted" && r.Decision == "accept" && ok {
				m.Accepted++
			}
			if r.Decision == "accept" && (r.Outcome != "accepted" || !ok) {
				m.ConsequentialMisses++
			}
			if ok {
				if r.Cost != nil {
					v, _ := exactNonnegative(*r.Cost)
					cost.Add(cost, v)
					costN++
				} else {
					m.UnknownCostAttempts++
				}
				if r.Seconds != nil {
					v, _ := exactNonnegative(*r.Seconds)
					secs.Add(secs, v)
					secN++
				} else {
					m.UnknownTimeAttempts++
				}
			}
			if r.CoverageStalenessSeconds != nil {
				v, _ := exactNonnegative(*r.CoverageStalenessSeconds)
				stale.Add(stale, v)
				staleN++
			}
			if r.PredictionCovered != nil {
				predN++
				if *r.PredictionCovered {
					predHit++
				}
			}
		}
		if m.AdmittedStarts > 0 {
			x := float64(m.Accepted) / float64(m.AdmittedStarts)
			m.AcceptedCompletionRate = &x
		}
		if m.Opportunities > 0 {
			x := float64(m.Accepted) / float64(m.Opportunities)
			m.AcceptedPerOpportunity = &x
		}
		if m.Accepted > 0 && costN == m.AdmittedStarts {
			x, _ := new(big.Rat).Quo(cost, big.NewRat(int64(m.Accepted), 1)).Float64()
			m.CostPerAccepted = &x
		}
		if m.Accepted > 0 && secN == m.AdmittedStarts {
			x, _ := new(big.Rat).Quo(secs, big.NewRat(int64(m.Accepted), 1)).Float64()
			m.SecondsPerAccepted = &x
		}
		if staleN > 0 {
			x, _ := new(big.Rat).Quo(stale, big.NewRat(int64(staleN), 1)).Float64()
			m.CoverageStalenessMean = &x
		}
		if predN > 0 {
			x := float64(predHit) / float64(predN)
			m.PredictionBoundCoverage = &x
		}
		out.Metrics[policy] = m
	}
	return out, nil
}

func replayTimeline(policy string, rows []Run, events []Event, synthetic, unsafe bool) (map[int]bool, map[int]bool, map[int]bool, int) {
	allowed, blocked, unknown := map[int]bool{}, map[int]bool{}, map[int]bool{}
	capacity, reserved := map[string]*big.Rat{}, map[string]*big.Rat{}
	known := map[string]bool{}
	overs, ei := 0, 0
	for i, r := range rows {
		if synthetic {
			for ei < len(events) && events[ei].At <= r.At {
				applyEvent(events[ei], capacity, reserved, known, &overs)
				ei++
			}
		}
		if r.Decision != "accept" {
			continue
		}
		capVal, capErr := exactNonnegative(r.Capacity)
		resVal, resErr := exactNonnegative(r.Reserved)
		reqVal, reqErr := exactNonnegative(r.Required)
		stateKnown := r.QuotaStateKnown
		if synthetic {
			capVal, resVal = poolValue(capacity, r.Pool), poolValue(reserved, r.Pool)
			capErr, resErr = nil, nil
			stateKnown = known[r.Pool]
		}
		if !stateKnown || reqErr != nil {
			unknown[i] = true
		}
		admission := sharedAdmitExact(policy, r.Gates, stateKnown, capVal, resVal, reqVal, capErr, resErr, reqErr, unsafe)
		if admission.Allowed {
			allowed[i] = true
			if synthetic && reqErr == nil && reqVal.Sign() > 0 {
				cur := poolValue(capacity, r.Pool)
				cur.Sub(cur, reqVal)
				capacity[r.Pool] = cur
				if overReserved(cur, poolValue(reserved, r.Pool)) {
					overs++
				}
			}
		} else if admission.Reason == "insufficient-capacity" {
			blocked[i] = true
		} else if admission.Reason == "unknown-capacity" {
			unknown[i] = true
		}
	}
	if synthetic {
		for ei < len(events) {
			applyEvent(events[ei], capacity, reserved, known, &overs)
			ei++
		}
	}
	return allowed, blocked, unknown, overs
}

func poolValue(m map[string]*big.Rat, key string) *big.Rat {
	if v, ok := m[key]; ok {
		return new(big.Rat).Set(v)
	}
	return new(big.Rat)
}
func overReserved(capacity, reserved *big.Rat) bool {
	return capacity.Sign() < 0 || capacity.Cmp(reserved) < 0
}

func applyEvent(e Event, capacity, reserved map[string]*big.Rat, known map[string]bool, overs *int) {
	amount, _ := exactNonnegative(e.Amount)
	switch e.Type {
	case "replenish":
		v := poolValue(capacity, e.Pool)
		v.Add(v, amount)
		capacity[e.Pool] = v
		known[e.Pool] = true
	case "reserve":
		v := poolValue(reserved, e.Pool)
		v.Add(v, amount)
		reserved[e.Pool] = v
		if overReserved(poolValue(capacity, e.Pool), v) {
			*overs++
		}
	case "release":
		v := poolValue(reserved, e.Pool)
		v.Sub(v, amount)
		if v.Sign() < 0 {
			v.SetInt64(0)
		}
		reserved[e.Pool] = v
	case "consume", "external-consume":
		v := poolValue(capacity, e.Pool)
		v.Sub(v, amount)
		capacity[e.Pool] = v
		if overReserved(v, poolValue(reserved, e.Pool)) {
			*overs++
		}
	}
}
func Compare(d Dataset, c Config) (Comparison, error) {
	if c.Partition == "" {
		c.Partition = "locked-test"
	}
	r, e := Replay(d, c)
	if e != nil {
		return Comparison{}, e
	}
	if len(r.Metrics) != len(Policies) {
		return Comparison{}, fmt.Errorf("comparison requires all five policy projections")
	}
	for _, p := range Policies {
		if _, ok := r.Metrics[p]; !ok {
			return Comparison{}, fmt.Errorf("comparison is missing policy %q", p)
		}
	}
	grids := map[string]map[string]bool{}
	for _, run := range d.Runs {
		if tasksPartition(d.Tasks, run.TaskID) == c.Partition {
			key := fmt.Sprintf("%s/%s/%d", run.TaskID, run.Configuration, run.Repetition)
			if grids[run.Policy] == nil {
				grids[run.Policy] = map[string]bool{}
			}
			grids[run.Policy][key] = true
		}
	}
	base := grids[Policies[0]]
	for _, p := range Policies[1:] {
		if len(grids[p]) != len(base) {
			return Comparison{}, fmt.Errorf("policy projections do not share the same trial grid")
		}
		for key := range base {
			if !grids[p][key] {
				return Comparison{}, fmt.Errorf("policy projections do not share the same trial grid")
			}
		}
	}
	ps := make([]string, 0, len(r.Metrics))
	for p := range r.Metrics {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return Comparison{Version: Version, Report: r, Policies: ps}, nil
}

func tasksPartition(tasks []Task, id string) string {
	for _, t := range tasks {
		if t.ID == id {
			return t.Partition
		}
	}
	return ""
}

// ParseNumber accepts JSON numbers for callers constructing projections.
func ParseNumber(v string) (float64, error) {
	x, e := strconv.ParseFloat(v, 64)
	if e == nil && (math.IsNaN(x) || math.IsInf(x, 0)) {
		e = fmt.Errorf("number must be finite")
	}
	return x, e
}

// Save writes canonical-enough JSON for offline artifacts; loading still uses
// the strict bounded, duplicate-key rejecting decoder.
func Save(path string, d Dataset) error {
	if e := Validate(d); e != nil {
		return e
	}
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0600)
}
