package v2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReplayDedupeAndPreservesUnknownRawFields(t *testing.T) {
	a := []byte(`{"type":"init","session_id":"s1","model":"m","futureControl":{"enabled":true}}`)
	b := []byte(`{"type":"result","result":"ok","stats":{"tokens":4}}`)
	o, e := Replay("gemini", [][]byte{a, a, b})
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Events) != 2 || o.Session.Status != "observed" || o.Usage.Status != "observed" || o.Acceptance != "not-evaluated" {
		t.Fatalf("%+v", o)
	}
	found := false
	for _, event := range o.Events {
		var raw map[string]any
		if json.Unmarshal(event.Raw, &raw) == nil && raw["futureControl"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown raw field lost")
	}
}
func TestReplayRejectsConflictingDuplicateAndMalformedFrames(t *testing.T) {
	if _, e := Replay("cursor", [][]byte{[]byte(`{"id":"1","type":"message"}`), []byte(`{"id":"1","type":"result"}`)}); e == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	for _, raw := range [][]byte{[]byte(`{`), make([]byte, MaxFrameBytes+1)} {
		if _, e := Replay("cursor", [][]byte{raw}); e == nil {
			t.Fatal("bad frame accepted")
		}
	}
}
func TestReplayKeepsAbsentDimensionsUnknownAndRejectsUnregistered(t *testing.T) {
	o, e := Replay("opencode", [][]byte{[]byte(`{"type":"text","part":"hello"}`)})
	if e != nil {
		t.Fatal(e)
	}
	if o.Usage.Status != "unknown" || o.Model.Status != "unknown" || o.Outcome.Status != "unknown" {
		t.Fatalf("absent dimensions inferred: %+v", o)
	}
	if _, e = Replay("agy", nil); e == nil {
		t.Fatal("guessed agy parser")
	}
}
func TestDecodeStrictRejectsUnknownAndTrailing(t *testing.T) {
	var x struct {
		A string `json:"a"`
	}
	if DecodeStrict([]byte(`{"a":"x","b":1}`), &x) == nil {
		t.Fatal("unknown field accepted")
	}
	if DecodeStrict([]byte(`{"a":"x"} {}`), &x) == nil {
		t.Fatal("trailing object accepted")
	}
	if DecodeStrict([]byte(`{"a":"x","a":"y"}`), &x) == nil {
		t.Fatal("duplicate key accepted")
	}
}

func TestClaudeErrorResultIsNotCompletion(t *testing.T) {
	o, err := Replay("claude-code", [][]byte{[]byte(`{"type":"result","subtype":"error","is_error":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if o.Outcome.Status != "observed" || !strings.Contains(string(o.Outcome.Value), `"state":"failed"`) {
		t.Fatalf("error result misclassified: %+v", o.Outcome)
	}
	if len(o.Diagnostics) == 0 {
		t.Fatal("error result diagnostic missing")
	}
}

func TestFailureDominatesTerminalForDocumentedDrivers(t *testing.T) {
	for host, raw := range map[string]string{
		"cursor":   `{"type":"result","subtype":"error","is_error":true}`,
		"gemini":   `{"type":"result","status":"error","response":"bad"}`,
		"opencode": `{"type":"step_finish","status":"error","error":{"message":"bad"}}`,
		"openclaw": `{"ok":false,"status":"error","final":"bad"}`,
	} {
		o, err := Replay(host, [][]byte{[]byte(raw)})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(o.Outcome.Value), `"state":"failed"`) {
			t.Fatalf("%s failure did not dominate: %+v", host, o.Outcome)
		}
	}
}

func TestEventIdentityIsScopedToRunAndAttempt(t *testing.T) {
	raw := [][]byte{[]byte(`{"type":"message","id":"evt-1"}`)}
	a, _ := ReplayScoped("cursor", "run-1", "attempt-1", raw)
	b, _ := ReplayScoped("cursor", "run-1", "attempt-2", raw)
	c, _ := ReplayScoped("cursor", "run-2", "attempt-1", raw)
	if a.Events[0].ID == b.Events[0].ID || a.Events[0].ID == c.Events[0].ID {
		t.Fatal("event ids collided across attempts or runs")
	}
}

func TestCredentialFieldsAreRejectedBeforeRawEvidenceRetention(t *testing.T) {
	for _, raw := range []string{`{"type":"message","authorization":"Bearer x"}`, `{"payload":{"api_key":"x"}}`, `{"type":"result","nested":{"password":"x"}}`, `{"access_token":"x"}`, `{"authToken":"x"}`, `{"private_key":"x"}`} {
		if _, err := Replay("cursor", [][]byte{[]byte(raw)}); err == nil {
			t.Fatalf("credential-bearing frame accepted: %s", raw)
		}
	}
	if _, err := Replay("cursor", [][]byte{[]byte(`{"type":"usage","tokens":{"input":2}}`)}); err != nil {
		t.Fatalf("usage token counts rejected: %v", err)
	}
	large := make([][]byte, 9)
	for i := range large {
		large[i] = []byte(`{"type":"message","payload":"` + strings.Repeat("x", 1<<20) + `"}`)
	}
	if _, err := Replay("cursor", large); err == nil {
		t.Fatal("aggregate fixture limit was not enforced")
	}
}

func TestMigrationPreservesBytesAndUnresolvedActivationDisabled(t *testing.T) {
	raw := []byte(`{"version":"aok-host/v1","future":{"x":1}}`)
	m, e := PreserveV1(raw, time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC), []string{"live-qualification"})
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateMigration(m); e != nil {
		t.Fatal(e)
	}
	if string(m.LegacyRaw) != string(raw) || m.Activation != "disabled" || len(m.UnresolvedObligations) != 1 {
		t.Fatalf("%+v", m)
	}
	m.Activation = "enabled"
	if ValidateMigration(m) == nil {
		t.Fatal("activation widening accepted")
	}
}

func TestOfflineFixtureQualificationNeverGrantsLiveOrRuntimeAuthority(t *testing.T) {
	fixtures := map[string]string{
		"opencode": `{"type":"step_finish","result":"ok"}`,
		"cursor":   `{"type":"result","result":"ok"}`,
		"gemini":   `{"type":"result","response":"ok"}`,
		"openclaw": `{"ok":true,"final":"ok"}`,
	}
	for host, fixture := range fixtures {
		t.Run(host, func(t *testing.T) {
			got, err := QualifyV2Fixtures(host, [][]byte{[]byte(fixture)})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "fixtures-parsed" || got.LiveQualification != "pending" || got.AuthorizationGranted {
				t.Fatalf("fixture result widened authority: %+v", got)
			}
			raw, _ := json.Marshal(got)
			if _, err := DecodeFixtureQualification(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := QualifyV2Fixtures("agy", [][]byte{[]byte(`{"type":"result"}`)}); err == nil {
		t.Fatal("unsupported agy fixture accepted")
	}
}
