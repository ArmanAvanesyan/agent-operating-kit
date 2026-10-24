// Package hostdriver contains immutable declarations for supported host
// protocols. A descriptor declares evidence and limits; it grants no authority.
package hostdriver

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostprotocol"
	"sort"
	"strings"
)

type Support string

const (
	Supported   Support = "supported"
	Unsupported Support = "unsupported"
)

// Descriptor is returned by value and its slices are copied by registry APIs.
type Descriptor struct {
	ID        string  `json:"id"`
	Protocol  string  `json:"protocol"`
	Version   string  `json:"version"`
	Support   Support `json:"support"`
	Replay    string  `json:"replay"`
	Execution string  `json:"execution"`
	Evidence  string  `json:"evidence"`
	Reason    string  `json:"reason,omitempty"`
	Digest    string  `json:"digest"`
}

var definitions = []Descriptor{
	{ID: "codex", Protocol: "codex-app-server-rpc", Version: "legacy-v1", Support: Supported, Replay: "native-v1", Execution: "p7-v1", Evidence: "existing internal/hostbridge Codex App Server implementation"},
	{ID: "claude-code", Protocol: "claude-stream-json", Version: "legacy-v1", Support: Supported, Replay: "native-v1", Execution: "p7-v1", Evidence: "existing internal/hostbridge Claude stream-json implementation"},
	{ID: "opencode", Protocol: "opencode-run-json-events", Version: "1.0.0", Support: Supported, Replay: "fixture", Execution: "p7-v2-controlled", Evidence: "https://docs.opencode.ai/docs/cli/"},
	{ID: "cursor", Protocol: "cursor-agent-stream-json", Version: "1.0.0", Support: Supported, Replay: "fixture", Execution: "p7-v2-controlled", Evidence: "https://cursor.com/docs/cli/headless"},
	{ID: "gemini", Protocol: "gemini-cli-stream-json", Version: "1.0.0", Support: Supported, Replay: "fixture", Execution: "p7-v2-controlled", Evidence: "https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/headless.md"},
	{ID: "openclaw", Protocol: "openclaw-agent-exec-json", Version: "1.0.0", Support: Supported, Replay: "fixture", Execution: "p7-v2-controlled", Evidence: "https://docs.openclaw.ai/cli/agent"},
	{ID: "agy", Protocol: "", Version: "", Support: Unsupported, Evidence: "https://www.agy.dev/docs/cli/reference/", Reason: "official documentation does not define a bounded machine event protocol for execution"},
	{ID: "hermes-agent", Protocol: "", Version: "", Support: Unsupported, Evidence: "", Reason: "no verified official machine protocol evidence registered"},
	{ID: "codex-cloud", Protocol: "", Version: "", Support: Unsupported, Evidence: "", Reason: "separate host surface; no bounded runner protocol declared"},
}

//go:embed registry.go
var implementationBytes []byte

func digest(d Descriptor) string {
	d.Digest = ""
	b, _ := json.Marshal(d)
	h := sha256.New()
	h.Write(b)
	h.Write(implementationBytes)
	h.Write([]byte(hostprotocol.ImplementationSHA256()))
	return hex.EncodeToString(h.Sum(nil))
}
func init() {
	for i := range definitions {
		definitions[i].Digest = digest(definitions[i])
	}
}

func List() []Descriptor {
	out := append([]Descriptor(nil), definitions...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func Lookup(id string) (Descriptor, bool) {
	for _, d := range definitions {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}

type Probe interface {
	Version(context.Context, string) (string, error)
}
type Discovery struct {
	Host             string `json:"host"`
	DescriptorSHA256 string `json:"descriptorSha256"`
	Executable       string `json:"executable"`
	Version          string `json:"version,omitempty"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
}

// Discover performs only an injected version probe. It never invokes a model,
// reads credentials, or starts a host session.
func Discover(ctx context.Context, host, executable string, probe Probe) (Discovery, error) {
	d, ok := Lookup(host)
	if !ok {
		return Discovery{}, fmt.Errorf("unknown host %q", host)
	}
	r := Discovery{Host: host, DescriptorSHA256: d.Digest, Executable: executable}
	if d.Support != Supported {
		r.Status = "unsupported"
		r.Reason = d.Reason
		return r, nil
	}
	if probe == nil || strings.TrimSpace(executable) == "" {
		r.Status = "unknown"
		r.Reason = "version probe unavailable"
		return r, nil
	}
	v, err := probe.Version(ctx, executable)
	if err != nil {
		r.Status = "unknown"
		r.Reason = "version probe failed"
		return r, nil
	}
	r.Version = v
	r.Status = "discovered"
	return r, nil
}

type Assignment struct {
	ID        string          `json:"id"`
	Host      string          `json:"host"`
	Workspace string          `json:"workspace"`
	Model     string          `json:"model"`
	Input     json.RawMessage `json:"input"`
	Digest    string          `json:"digest"`
}
type Policy struct {
	Host                string `json:"host"`
	HostVersion         string `json:"hostVersion"`
	Executable          string `json:"executable"`
	ExecutableSHA256    string `json:"executableSha256"`
	AuthMode            string `json:"authMode"`
	AccountScope        string `json:"accountScope"`
	Workspace           string `json:"workspace"`
	Model               string `json:"model"`
	ProfileSHA256       string `json:"profileSha256"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	MaxSeconds          int    `json:"maxSeconds"`
	MaxBytes            int    `json:"maxBytes"`
	AllowDelegation     bool   `json:"allowDelegation"`
}

// CurrentPolicy is an immutable binding consumed by runtime/readiness adapters.
// It has no setters and contains only value fields.
type CurrentPolicy interface {
	CurrentPolicy() Policy
	ImmutableDriverSHA() string
}
type boundPolicy struct {
	policy    Policy
	driverSHA string
}

func (b boundPolicy) CurrentPolicy() Policy      { return b.policy }
func (b boundPolicy) ImmutableDriverSHA() string { return b.driverSHA }

func BindPolicy(p Policy) (CurrentPolicy, error) {
	d, ok := Lookup(p.Host)
	if !ok || d.Support != Supported || p.HostVersion == "" || p.Workspace == "" || p.Model == "" || p.MaxSeconds < 1 || p.MaxSeconds > 60 || p.MaxBytes < 1 || p.MaxBytes > 8*1024*1024 || p.AllowDelegation || p.Executable == "" || !validSHA(p.ExecutableSHA256) || p.AuthMode == "" || p.AccountScope == "" || !validSHA(p.ProfileSHA256) || !validSHA(p.ConfigurationSHA256) {
		return nil, errors.New("policy is not bounded for the registered driver")
	}
	return boundPolicy{policy: p, driverSHA: d.Digest}, nil
}

type Prepared struct {
	Version             string `json:"version"`
	Host                string `json:"host"`
	HostVersion         string `json:"hostVersion"`
	Executable          string `json:"executable"`
	ExecutableSHA256    string `json:"executableSha256"`
	AuthMode            string `json:"authMode"`
	AccountScope        string `json:"accountScope"`
	DescriptorSHA256    string `json:"descriptorSha256"`
	AssignmentSHA256    string `json:"assignmentSha256"`
	InputSHA256         string `json:"inputSha256"`
	PolicySHA256        string `json:"policySha256"`
	Workspace           string `json:"workspace"`
	Model               string `json:"model"`
	ProfileSHA256       string `json:"profileSha256"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	MaxSeconds          int    `json:"maxSeconds"`
	MaxBytes            int    `json:"maxBytes"`
	AllowDelegation     bool   `json:"allowDelegation"`
	Digest              string `json:"digest"`
	OutputReference     string `json:"outputReference"`
}

func Prepare(a Assignment, p Policy) (Prepared, error) {
	d, ok := Lookup(a.Host)
	if !ok || d.Support != Supported {
		return Prepared{}, errors.New("host driver unsupported")
	}
	if a.Host != p.Host || a.Workspace == "" || a.Workspace != p.Workspace || a.Model == "" || a.Model != p.Model || p.HostVersion == "" || p.Executable == "" || !validSHA(p.ExecutableSHA256) || p.AuthMode == "" || p.AccountScope == "" || !validSHA(p.ProfileSHA256) || !validSHA(p.ConfigurationSHA256) || p.MaxSeconds < 1 || p.MaxSeconds > 60 || p.MaxBytes < 1 || p.MaxBytes > 8*1024*1024 || p.AllowDelegation || !json.Valid(a.Input) {
		return Prepared{}, errors.New("invalid or unbounded host preparation")
	}
	ab, _ := json.Marshal(a)
	pb, _ := json.Marshal(p)
	ah := sha256.Sum256(ab)
	ph := sha256.Sum256(pb)
	ih := sha256.Sum256(a.Input)
	r := Prepared{Version: "aok-hostdriver/v1", Host: a.Host, HostVersion: p.HostVersion, Executable: p.Executable, ExecutableSHA256: p.ExecutableSHA256, AuthMode: p.AuthMode, AccountScope: p.AccountScope, DescriptorSHA256: d.Digest, AssignmentSHA256: hex.EncodeToString(ah[:]), InputSHA256: hex.EncodeToString(ih[:]), PolicySHA256: hex.EncodeToString(ph[:]), Workspace: a.Workspace, Model: a.Model, ProfileSHA256: p.ProfileSHA256, ConfigurationSHA256: p.ConfigurationSHA256, MaxSeconds: p.MaxSeconds, MaxBytes: p.MaxBytes, AllowDelegation: false}
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	r.Digest = hex.EncodeToString(h[:])
	r.OutputReference = "aok-prepared://sha256/" + r.Digest
	return r, nil
}

func validSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func VerifyPrepared(p Prepared) bool {
	if p.Version != "aok-hostdriver/v1" || p.AllowDelegation || p.MaxSeconds < 1 || p.MaxSeconds > 60 || p.MaxBytes < 1 || p.MaxBytes > 8*1024*1024 {
		return false
	}
	got, ref := p.Digest, p.OutputReference
	p.Digest = ""
	p.OutputReference = ""
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	want := hex.EncodeToString(h[:])
	return got == want && ref == "aok-prepared://sha256/"+want
}

// Arguments is compiled driver code, included in descriptor identity. Prompts
// are argument values and are never interpreted by a shell.
func Arguments(host, workspace, model, prompt string) ([]string, error) {
	if !json.Valid([]byte(prompt)) || !strings.HasPrefix(prompt, "{") {
		return nil, fmt.Errorf("compiled driver requires the structured JSON task envelope")
	}
	switch host {
	case "opencode":
		return []string{"run", "--format", "json", "--model", model, prompt}, nil
	case "cursor":
		return []string{"--print", "--output-format", "stream-json", "--model", model, prompt}, nil
	case "gemini":
		return []string{"--output-format", "stream-json", "--model", model, "--prompt", prompt}, nil
	case "openclaw":
		return []string{"agent", "exec", "--isolated", "--json", "--cwd", workspace, "--model", model, prompt}, nil
	}
	return nil, fmt.Errorf("no compiled invocation for host %q", host)
}
