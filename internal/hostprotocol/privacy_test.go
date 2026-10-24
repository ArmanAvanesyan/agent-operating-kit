package hostprotocol

import "testing"

func TestUsageTokenExemptionsCannotCarryCredentials(t *testing.T) {
	for _, raw := range []string{`{"tokens":"private-secret"}`, `{"usage":{"tokens":["secret"]}}`, `{"usage":{"input_tokens":"secret"}}`, `{"usage":{"tokens":{"input":"secret"}}}`, `{"access_token":"secret"}`, `{"authToken":"secret"}`} {
		if !containsCredentialField([]byte(raw)) {
			t.Fatal("credential retained", raw)
		}
	}
	for _, raw := range []string{`{"usage":{"tokens":{"input":12,"output":3}}}`, `{"usage":{"input_tokens":10,"output_tokens":4}}`} {
		if containsCredentialField([]byte(raw)) {
			t.Fatal("numeric usage rejected", raw)
		}
	}
}
func TestReplayRejectsConflictingDuplicateProtocolKeys(t *testing.T) {
	if _, e := ReplayScoped("cursor", "run", "attempt", [][]byte{[]byte(`{"type":"result","status":"error","status":"success"}`)}); e == nil {
		t.Fatal("duplicate protocol key accepted")
	}
}
