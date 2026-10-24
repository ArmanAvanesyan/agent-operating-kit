package runtime

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func replayFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join("testdata", "offline", "replay.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}
func asOf(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-10-23T12:00:19Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestReplayIsDeterministicAndSyntheticOnly(t *testing.T) {
	path := replayFixture(t)
	files1, report1, err := Replay(path, asOf(t))
	if err != nil {
		t.Fatal(err)
	}
	files2, report2, err := Replay(path, asOf(t))
	if err != nil {
		t.Fatal(err)
	}
	if report1.Accepted || report2.Accepted || report1.RuntimeGate != "synthetic-replay-only" {
		t.Fatalf("replay promoted acceptance/readiness: %+v", report1)
	}
	if len(files1) != 2 || len(files2) != 2 {
		t.Fatalf("unexpected artifact files: %#v", files1)
	}
	for _, name := range []string{"report.json", "eventtrace.json"} {
		if !bytes.Equal(files1[name], files2[name]) {
			t.Fatalf("%s differs across identical replays", name)
		}
	}
	if !bytes.Equal(files1["report.json"], Encode(report1)) {
		t.Fatal("report artifact differs from returned report")
	}
	if strings.Contains(string(files1["eventtrace.json"]), "runtime.sqlite") {
		t.Fatal("SQLite artifact leaked into trace")
	}
	if report1.State == nil || len(report1.State.Runs) != 1 || len(report1.State.Runs[0].Steps) != 2 {
		t.Fatal("runtime ledger state missing")
	}
	if report1.State.Runs[0].Steps[0].Phase != "cancelled" || report1.State.Runs[0].Steps[0].Accepted {
		t.Fatal("synthetic cancellation or acceptance boundary incorrect")
	}
	if report1.State.Runs[0].Steps[1].Token.Fence < 2 {
		t.Fatal("expired claim was not fenced during recovery")
	}
}

func readScenario(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(replayFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func writeScenario(t *testing.T, s map[string]any) string {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func events(s map[string]any) []any { return s["events"].([]any) }

func TestReplayRejectsBackwardsAndFutureEventTimes(t *testing.T) {
	backwards := readScenario(t)
	e := events(backwards)
	e[1].(map[string]any)["at"] = "2026-10-23T11:59:59Z"
	if _, _, err := Replay(writeScenario(t, backwards), asOf(t)); err == nil || !strings.Contains(err.Error(), "backwards") {
		t.Fatalf("backwards event accepted: %v", err)
	}
	future := readScenario(t)
	e = events(future)
	e[len(e)-1].(map[string]any)["at"] = "2026-10-23T12:00:20Z"
	if _, _, err := Replay(writeScenario(t, future), asOf(t)); err == nil || !strings.Contains(err.Error(), "after replay as-of") {
		t.Fatalf("future event accepted: %v", err)
	}
}

func TestReplayDoesNotAutoAdmitAfterObservedQuotaReset(t *testing.T) {
	s := readScenario(t)
	e := events(s)
	var ingest map[string]any
	for _, item := range e {
		if item.(map[string]any)["operation"] == "ingest" {
			ingest = item.(map[string]any)["payload"].(map[string]any)
			break
		}
	}
	if ingest == nil {
		t.Fatal("ingest event missing")
	}
	export := ingest["export"].(map[string]any)
	quotas := export["quotas"].([]any)
	quotas[0].(map[string]any)["resetAt"] = "2026-10-23T12:00:14Z"
	if _, _, err := Replay(writeScenario(t, s), asOf(t)); err == nil || !strings.Contains(err.Error(), "capacity admission blocked") {
		t.Fatalf("reset quota was auto-admitted: %v", err)
	}
}

func TestReplayRejectsDispatchEnabledScenario(t *testing.T) {
	s := readScenario(t)
	s["policy"].(map[string]any)["allowDispatch"] = true
	if _, _, err := Replay(writeScenario(t, s), asOf(t)); err == nil || !strings.Contains(err.Error(), "cannot enable dispatch") {
		t.Fatalf("dispatch-enabled scenario accepted: %v", err)
	}
}

func TestReplayRejectsUnsafeOrMalformedScenarioFiles(t *testing.T) {
	fixture, err := os.ReadFile(replayFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	target := filepath.Join(base, "scenario.json")
	if err = os.WriteFile(target, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "scenario-link.json")
	if err = os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Replay(link, asOf(t)); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink scenario accepted: %v", err)
	}

	oversize := filepath.Join(base, "oversized.json")
	if err = os.WriteFile(oversize, bytes.Repeat([]byte(" "), (4<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Replay(oversize, asOf(t)); err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") {
		t.Fatalf("oversized scenario accepted: %v", err)
	}

	duplicate := append([]byte(`{"version":"aok-runtime/v1",`), fixture[1:]...)
	duplicatePath := filepath.Join(base, "duplicate.json")
	if err = os.WriteFile(duplicatePath, duplicate, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Replay(duplicatePath, asOf(t)); err == nil {
		t.Fatal("duplicate top-level JSON key accepted")
	}

	unsupported := readScenario(t)
	unsupported["version"] = "aok-runtime/v999"
	if _, _, err = Replay(writeScenario(t, unsupported), asOf(t)); err == nil || !strings.Contains(err.Error(), "scenario contract") {
		t.Fatalf("unsupported version accepted: %v", err)
	}
}

func TestReplayCanonicalizesSystemTemporaryDirectory(t *testing.T) {
	t.Setenv("TMPDIR", os.TempDir())
	// macOS's default /tmp is a symlink. The runtime must canonicalize only its
	// own newly created temporary directory, without relaxing input-path checks.
	files, _, err := Replay(replayFixture(t), asOf(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatal("replay artifacts absent")
	}
}
