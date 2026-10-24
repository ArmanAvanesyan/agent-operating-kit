// Package evaluation prepares signed, independently accepted offline evidence.
package evaluation

import "encoding/json"

const Version = "aok-evaluation/v1"

type Cohort struct {
	ConfigurationID string `json:"configurationId"`
	ProfileSHA256   string `json:"profileSha256"`
	TaskClass       string `json:"taskClass"`
	Language        string `json:"language"`
	Risk            string `json:"risk"`
	AccountScope    string `json:"accountScope"`
	ProtocolSHA256  string `json:"protocolSha256"`
}
type Receipt struct {
	Issuer                string   `json:"issuer"`
	TaskSHA256            string   `json:"taskSha256"`
	Cohort                Cohort   `json:"cohort"`
	FamilyID              string   `json:"familyId"`
	Repository            string   `json:"repository"`
	NearDuplicateGroup    string   `json:"nearDuplicateGroup"`
	Partition             string   `json:"partition"`
	RegisteredRepetitions []string `json:"registeredRepetitions"`
	ConfigurationID       string   `json:"configurationId"`
	ProfileSHA256         string   `json:"profileSha256"`
	ProtocolSHA256        string   `json:"protocolSha256"`
	RepetitionID          string   `json:"repetitionId"`
	ObservedAt            string   `json:"observedAt"`
	VerifierSHA256        string   `json:"verifierSha256"`
	RubricSHA256          string   `json:"rubricSha256"`
	InputSHA256           string   `json:"inputSha256"`
	OutputSHA256          string   `json:"outputSha256"`
	Accepted              bool     `json:"accepted"`
	Signature             string   `json:"signature"`
}
type Measurement struct {
	PoolID       string       `json:"poolId"`
	WindowFamily string       `json:"windowFamily"`
	Metric       string       `json:"metric"`
	Unit         string       `json:"unit"`
	Value        *json.Number `json:"value"`
	Attributable bool         `json:"attributable"`
}
type OutcomeRecord struct {
	Version               string        `json:"version"`
	ID                    string        `json:"id"`
	FamilyID              string        `json:"familyId"`
	Repository            string        `json:"repository"`
	NearDuplicateGroup    string        `json:"nearDuplicateGroup"`
	Partition             string        `json:"partition"`
	Cohort                Cohort        `json:"cohort"`
	TaskSHA256            string        `json:"taskSha256"`
	InputSHA256           string        `json:"inputSha256"`
	OutputSHA256          string        `json:"outputSha256"`
	RepetitionID          string        `json:"repetitionId"`
	RegisteredRepetitions []string      `json:"registeredRepetitions"`
	TerminalOutcome       string        `json:"terminalOutcome"`
	ObservedAt            string        `json:"observedAt"`
	Producer              string        `json:"producer"`
	Receipt               *Receipt      `json:"receipt"`
	Cost                  *json.Number  `json:"cost"`
	Currency              string        `json:"currency"`
	Seconds               *json.Number  `json:"seconds"`
	Consumption           []Measurement `json:"consumption"`
}
type ShardManifest struct {
	Version   string `json:"version"`
	Issuer    string `json:"issuer"`
	Asset     string `json:"asset"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}
type Issuer struct {
	ID             string   `json:"id"`
	PublicKey      string   `json:"publicKey"`
	Roles          []string `json:"roles"`
	VerifierSHA256 string   `json:"verifierSha256"`
	RubricSHA256   string   `json:"rubricSha256"`
}
type EvidencePolicy struct {
	Version              string   `json:"version"`
	Issuers              []Issuer `json:"issuers"`
	QualityMaxAgeSeconds int      `json:"qualityMaxAgeSeconds"`
	DemandMaxAgeSeconds  int      `json:"demandMaxAgeSeconds"`
}
type Bound struct {
	PoolID       string       `json:"poolId"`
	WindowFamily string       `json:"windowFamily"`
	Metric       string       `json:"metric"`
	Unit         string       `json:"unit"`
	Upper        *json.Number `json:"upper"`
	Method       string       `json:"method"`
	Families     int          `json:"families"`
}
type EstimateArtifact struct {
	Version               string       `json:"version"`
	Cohort                Cohort       `json:"cohort"`
	Families              int          `json:"families"`
	SuccessfulFamilies    int          `json:"successfulFamilies"`
	QualityLower          json.Number  `json:"qualityLower"`
	CostUpper             *json.Number `json:"costUpper"`
	Currency              string       `json:"currency"`
	SecondsUpper          *json.Number `json:"secondsUpper"`
	Bounds                []Bound      `json:"bounds"`
	ObservedAt            string       `json:"observedAt"`
	QualityValidUntil     string       `json:"qualityValidUntil"`
	DemandValidUntil      string       `json:"demandValidUntil"`
	CalibrationFamilies   int          `json:"calibrationFamilies"`
	CalibrationPartition  string       `json:"calibrationPartition"`
	CalibrationObservedAt string       `json:"calibrationObservedAt"`
	DemandProvenance      []string     `json:"demandProvenance"`
	Method                string       `json:"method"`
	Provenance            []string     `json:"provenance"`
	PolicySHA256          string       `json:"policySha256"`
}
