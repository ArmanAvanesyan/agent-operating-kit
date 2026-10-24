// Package v2 defines closed observation records and bounded event replay for
// additional host protocols. Host completion is an observation, never acceptance.
package hostprotocol

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"io"
	"sort"
	"strings"
)

//go:embed records.go
var implementationBytes []byte

func ImplementationSHA256() string {
	sum := sha256.Sum256(implementationBytes)
	return hex.EncodeToString(sum[:])
}

const Version = "aok-host-bridge/v2"
const MaxFrameBytes = 1 << 20
const MaxFrames = 10000
const MaxTotalBytes = 8 << 20

type Dimension struct {
	Value  json.RawMessage `json:"value,omitempty"`
	Status string          `json:"status"`
	Source string          `json:"source,omitempty"`
}
type Event struct {
	ID        string          `json:"id"`
	Host      string          `json:"host"`
	RunID     string          `json:"runId"`
	AttemptID string          `json:"attemptId"`
	Kind      string          `json:"kind"`
	NativeID  string          `json:"nativeId,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Sequence  *int64          `json:"sequence,omitempty"`
	Raw       json.RawMessage `json:"raw"`
}
type Observation struct {
	Version     string    `json:"version"`
	Host        string    `json:"host"`
	RunID       string    `json:"runId"`
	AttemptID   string    `json:"attemptId"`
	Protocol    string    `json:"protocol"`
	Events      []Event   `json:"events"`
	Outcome     Dimension `json:"outcome"`
	Model       Dimension `json:"model"`
	Usage       Dimension `json:"usage"`
	Tools       Dimension `json:"tools"`
	Session     Dimension `json:"session"`
	Controls    Dimension `json:"controls"`
	Context     Dimension `json:"context"`
	Output      Dimension `json:"output"`
	Diagnostics []string  `json:"diagnostics"`
	Acceptance  string    `json:"acceptance"`
}

type frame struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	ID         string          `json:"id"`
	SessionID  string          `json:"session_id"`
	SessionID2 string          `json:"sessionId"`
	Model      string          `json:"model"`
	Model2     string          `json:"model_id"`
	Result     string          `json:"result"`
	Response   string          `json:"response"`
	Status     string          `json:"status"`
	Content    json.RawMessage `json:"content"`
	Stats      json.RawMessage `json:"stats"`
	Usage      json.RawMessage `json:"usage"`
	Error      json.RawMessage `json:"error"`
	Seq        *int64          `json:"seq"`
	Sequence   *int64          `json:"sequence"`
	OK         *bool           `json:"ok"`
	Final      string          `json:"final"`
	Part       json.RawMessage `json:"part"`
	Tokens     json.RawMessage `json:"tokens"`
}

// Replay accepts one JSON document or JSONL frames. Unknown fields remain in
// Event.Raw. Missing measurements remain unknown; no defaults imply zero.
func Replay(host string, frames [][]byte) (Observation, error) {
	return ReplayScoped(host, "", "", frames)
}

func ReplayScoped(host, runID, attemptID string, frames [][]byte) (Observation, error) {
	o := Observation{Version: Version, Host: host, RunID: runID, AttemptID: attemptID, Outcome: unknown(), Model: unknown(), Usage: unknown(), Tools: unknown(), Session: unknown(), Controls: unknown(), Context: unknown(), Output: unknown(), Events: []Event{}, Diagnostics: []string{}, Acceptance: "not-evaluated"}
	if len(frames) > MaxFrames {
		return Observation{}, errors.New("too many host frames")
	}
	protocol := map[string]string{"opencode": "opencode-run-json-events", "cursor": "cursor-agent-stream-json", "gemini": "gemini-cli-stream-json", "openclaw": "openclaw-agent-exec-json", "codex": "codex-app-server-rpc", "claude-code": "claude-stream-json"}[host]
	if protocol == "" {
		return Observation{}, fmt.Errorf("no parser registered for host %q", host)
	}
	o.Protocol = protocol
	seen := map[string]string{}
	ids := map[string]bool{}
	var textOutput strings.Builder
	failed := false
	totalBytes := 0
	for _, raw := range frames {
		totalBytes += len(raw)
		if totalBytes > MaxTotalBytes {
			return Observation{}, errors.New("host fixture exceeds aggregate replay bound")
		}
		if len(raw) == 0 || len(raw) > MaxFrameBytes || !json.Valid(raw) {
			return Observation{}, errors.New("invalid or oversized host frame")
		}
		if err := contracts.ValidateJSON(raw); err != nil {
			return Observation{}, fmt.Errorf("invalid protocol JSON: %w", err)
		}
		if containsCredentialField(raw) {
			return Observation{}, errors.New("host frame contains credential-bearing field; raw evidence not retained")
		}
		var f frame
		if err := json.Unmarshal(raw, &f); err != nil {
			return Observation{}, err
		}
		kind := f.Type
		if kind == "" {
			kind = f.Subtype
		}
		kind = normalizeKind(host, kind, f)
		if kind == "" {
			kind = "unknown"
		}
		var identity string
		if f.ID != "" {
			identity = f.ID
		} else {
			sum := sha256.Sum256(raw)
			identity = hex.EncodeToString(sum[:])
		}
		sum := sha256.Sum256(raw)
		rawHash := hex.EncodeToString(sum[:])
		id := host + ":" + runID + ":" + attemptID + ":" + identity
		if old, ok := seen[id]; ok {
			if old != rawHash {
				return Observation{}, errors.New("conflicting duplicate host event")
			}
			continue
		}
		seen[id] = rawHash
		if ids[id] {
			return Observation{}, errors.New("duplicate normalized event id")
		}
		ids[id] = true
		sid := f.SessionID
		if sid == "" {
			sid = f.SessionID2
		}
		seq := f.Sequence
		if seq == nil {
			seq = f.Seq
		}
		e := Event{ID: id, Host: host, RunID: runID, AttemptID: attemptID, Kind: kind, NativeID: f.ID, SessionID: sid, Sequence: seq, Raw: append(json.RawMessage(nil), raw...)}
		o.Events = append(o.Events, e)
		if sid != "" {
			o.Session = knownString(sid, "event")
		}
		model := f.Model
		if model == "" {
			model = f.Model2
		}
		if model != "" {
			o.Model = knownString(model, "event")
		}
		if host == "opencode" && kind == "text" && len(f.Part) > 0 {
			var part struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(f.Part, &part) == nil {
				textOutput.WriteString(part.Text)
			}
		}
		if len(f.Usage) > 0 && string(f.Usage) != "null" {
			o.Usage = Dimension{Value: append(json.RawMessage(nil), f.Usage...), Status: "observed", Source: "event"}
		} else if len(f.Stats) > 0 && string(f.Stats) != "null" {
			o.Usage = Dimension{Value: append(json.RawMessage(nil), f.Stats...), Status: "observed", Source: "event"}
		} else if host == "opencode" && len(f.Part) > 0 {
			var part map[string]json.RawMessage
			if json.Unmarshal(f.Part, &part) == nil && len(part["tokens"]) > 0 {
				o.Usage = Dimension{Value: append(json.RawMessage(nil), part["tokens"]...), Status: "observed", Source: "event.part.tokens"}
			}
		} else if host == "gemini" && len(f.Tokens) > 0 {
			o.Usage = Dimension{Value: append(json.RawMessage(nil), f.Tokens...), Status: "observed", Source: "event.tokens"}
		}
		errorEvent := kind == "error" || f.Status == "error" || f.Subtype == "error" || (f.OK != nil && !*f.OK) || (len(f.Error) > 0 && string(f.Error) != "null")
		if errorEvent {
			failed = true
			v, _ := json.Marshal(map[string]any{"state": "failed", "stopConfirmed": true})
			o.Outcome = Dimension{Value: v, Status: "observed", Source: "error-event"}
		}
		if terminal(host, kind, f) && !errorEvent {
			v, _ := json.Marshal(map[string]any{"state": "completed", "textAvailable": f.Result != "" || f.Response != "" || f.Final != ""})
			o.Outcome = Dimension{Value: v, Status: "observed", Source: "terminal-event"}
			out := f.Result
			if out == "" {
				out = f.Response
			}
			if out == "" {
				out = f.Final
			}
			if out == "" && len(f.Content) > 0 {
				_ = json.Unmarshal(f.Content, &out)
			}
			if out == "" && len(f.Part) > 0 {
				var part struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(f.Part, &part)
				out = part.Text
			}
			if out != "" {
				b, _ := json.Marshal(out)
				o.Output = Dimension{Value: b, Status: "observed", Source: "terminal-event"}
			}
		}
		if kind == "error" || f.Status == "error" || f.Subtype == "error" || (f.OK != nil && !*f.OK) {
			o.Diagnostics = append(o.Diagnostics, "host emitted an error event")
		}
	}
	if textOutput.Len() > 0 {
		v, _ := json.Marshal(textOutput.String())
		o.Output = Dimension{Value: v, Status: "observed", Source: "opencode-text-events"}
	}
	if failed {
		v, _ := json.Marshal(map[string]any{"state": "failed", "stopConfirmed": true})
		o.Outcome = Dimension{Value: v, Status: "observed", Source: "error-event"}
	}
	if len(o.Events) == 0 {
		o.Diagnostics = append(o.Diagnostics, "no host events observed")
	}
	sort.Slice(o.Events, func(i, j int) bool { return o.Events[i].ID < o.Events[j].ID })
	return o, nil
}

func containsCredentialField(raw []byte) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	var scan func(any) bool
	scan = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				n := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "-", ""), "_", ""))
				usageCount := map[string]bool{"tokens": true, "inputtokens": true, "outputtokens": true, "totaltokens": true, "prompttokens": true, "completiontokens": true, "cachedtokens": true, "cachecreationinputtokens": true, "cachereadinputtokens": true}
				credentialToken := (n == "token" || strings.HasSuffix(n, "token")) && !usageCount[n]
				if n == "authorization" || n == "apikey" || n == "password" || n == "secret" || n == "credential" || n == "credentials" || n == "cookie" || credentialToken || strings.HasSuffix(n, "secret") || strings.HasSuffix(n, "apikey") || strings.HasSuffix(n, "privatekey") {
					return true
				}
				if usageCount[n] && !numericUsage(child) {
					return true
				}
				if scan(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if scan(child) {
					return true
				}
			}
		}
		return false
	}
	return scan(value)
}

func normalizeKind(host, kind string, f frame) string {
	switch host {
	case "opencode": // `opencode run --format json` emits typed raw event records.
		if kind == "step_finish" {
			return "completed"
		}
	case "cursor": // Cursor stream-json ends with a result record.
		if kind == "result" {
			return "completed"
		}
	case "gemini": // Gemini headless stream-json uses init/message/tool_use/tool_result/result.
		if kind == "result" {
			return "completed"
		}
	case "openclaw": // --json returns a single {ok,status,final,...} envelope.
		if f.OK != nil {
			if *f.OK {
				return "completed"
			}
			return "error"
		}
	case "codex":
		if kind == "turn/completed" {
			return "completed"
		}
	case "claude-code":
		if kind == "result" {
			if f.Subtype == "success" {
				return "completed"
			}
			if f.Subtype != "" {
				return "error"
			}
		}
	}
	return kind
}
func terminal(host, kind string, f frame) bool {
	if f.Status == "error" || f.Subtype == "error" || (f.OK != nil && !*f.OK) || (len(f.Error) > 0 && string(f.Error) != "null") {
		return false
	}
	if host == "claude-code" {
		return kind == "completed"
	}
	if kind == "completed" || kind == "result" || kind == "turn.completed" || f.Subtype == "success" {
		return true
	}
	return host == "openclaw" && kind == "completed"
}
func unknown() Dimension { return Dimension{Status: "unknown"} }
func knownString(s, source string) Dimension {
	b, _ := json.Marshal(s)
	return Dimension{Value: b, Status: "observed", Source: source}
}

// DecodeStrict rejects unknown fields and trailing JSON for v2 persisted records.
func DecodeStrict(raw []byte, dst any) error {
	if err := contracts.ValidateJSON(raw); err != nil {
		return fmt.Errorf("invalid strict JSON: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}

func ValidHost(host string) bool { return strings.TrimSpace(host) != "" }

// Usage counter exemptions accept numeric objects, never arbitrary token strings.
func numericUsage(v any) bool {
	switch n := v.(type) {
	case nil:
		return true
	case float64:
		return n >= 0
	case map[string]any:
		for _, x := range n {
			if !numericUsage(x) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
