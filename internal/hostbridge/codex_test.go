package hostbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func offlineManifest(t *testing.T, host string) string {
	t.Helper()
	path := filepath.Join("testdata", "offline", host, "replay.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func offlineFrames(t *testing.T) [][]byte {
	t.Helper()
	dir := filepath.Join("testdata", "offline", "codex")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "frame-") && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, entry.Name())
		}
	}
	sort.Strings(paths)
	var out [][]byte
	for _, name := range paths {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, b)
	}
	return out
}

func offlineCodexPrepared() Prepared {
	return Prepared{Version: Version, Host: "codex", HostVersion: CodexPinnedVersion, Model: "gpt-6.1-sol", Workspace: "/offline/workspace", OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","const":true}},"required":["ok"],"additionalProperties":false}`)}
}

func TestOfflineReplayFixturesForCodexAndClaude(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			bundle, err := Replay(offlineManifest(t, host))
			if err != nil {
				t.Fatal(err)
			}
			if bundle.Receipt == nil || !bundle.Receipt.OutputValidated || bundle.Receipt.Report.LiveGate != "fixture-only" || bundle.Transcript == nil || bundle.Transcript.HostOutcome != "completed" {
				t.Fatalf("offline replay did not validate: receipt=%+v transcript=%+v", bundle.Receipt, bundle.Transcript)
			}
			if string(bundle.Transcript.Output) != `{"ok":true}` {
				t.Fatalf("unexpected fixture output: %s", bundle.Transcript.Output)
			}
			if strings.Contains(string(encoded(bundle.Transcript)), "OFFLINE_CANARY") || strings.Contains(string(encoded(bundle.Transcript)), "SECRET_CANARY") || strings.Contains(string(encoded(bundle.Transcript)), "PRIVATE_ACCOUNT") {
				t.Fatal("offline replay persisted a private canary")
			}
		})
	}
}

func TestParseCodexFramesRejectsIdentityMismatch(t *testing.T) {
	p := offlineCodexPrepared()
	frames := offlineFrames(t)
	frames[0] = []byte(strings.Replace(string(frames[0]), p.Model, "other-model", 1))
	if _, err := ParseCodexFrames(p, nil, frames); err == nil {
		t.Fatal("accepted mismatched model identity")
	}
	frames = offlineFrames(t)
	frames[0] = []byte(strings.Replace(string(frames[0]), "offline-session-1", "other-session", 1))
	previous := &Receipt{SessionID: "offline-session-1", ThreadID: "offline-thread-1"}
	if _, err := ParseCodexFrames(p, previous, frames); err == nil {
		t.Fatal("accepted resume session mismatch")
	}
}

func TestParseCodexFramesRejectsConflictingDuplicateAndPostTerminal(t *testing.T) {
	p := offlineCodexPrepared()
	frames := offlineFrames(t)
	dup := []byte(strings.Replace(string(frames[5]), `"text":"`, `"text":"conflict-`, 1))
	frames = append(frames[:6], append([][]byte{dup}, frames[6:]...)...)
	if _, err := ParseCodexFrames(p, nil, frames); err == nil {
		t.Fatal("accepted conflicting duplicate item completion")
	}
	frames = offlineFrames(t)
	frames = append(frames, []byte(`{"method":"item/started","params":{"threadId":"offline-thread-1","turnId":"offline-turn-1","item":{"id":"late","type":"commandExecution"}}}`))
	if _, err := ParseCodexFrames(p, nil, frames); err == nil {
		t.Fatal("accepted event after terminal turn")
	}
}

func TestParseCodexFramesSanitizesCanaryMetadata(t *testing.T) {
	p := offlineCodexPrepared()
	transcript, err := ParseCodexFrames(p, nil, offlineFrames(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(transcript)
	if strings.Contains(string(raw), "OFFLINE_CANARY") {
		t.Fatalf("persisted ignored metadata canary: %s", raw)
	}
	for _, event := range transcript.Events {
		if event.Type == "thread/started" || event.Type == "account/rateLimits/updated" {
			t.Fatalf("unexpected private metadata event retained: %+v", event)
		}
	}
}

func TestCodexReplayFixtureUsesMinimalPreparedSchema(t *testing.T) {
	var manifest ReplayManifest
	data, err := os.ReadFile(offlineManifest(t, "codex"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Prepared.Digest != preparedDigest(manifest.Prepared) || validateOutputSchema(manifest.Prepared.OutputSchema) != nil {
		t.Fatal("offline prepared fixture is not hash-bound to its schema")
	}
	if manifest.Prepared.Configuration.Host != nil || manifest.Prepared.Configuration.ModelID != nil || manifest.Prepared.ConfigurationID != "" {
		t.Fatal("offline replay fixture should not require a full host configuration")
	}
}

func TestPinnedCodexProtocolReferenceIntegrity(t *testing.T) {
	root := filepath.Join("testdata", "codex-0.162.1-schema")
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Host        string  `json:"host"`
		HostVersion string  `json:"hostVersion"`
		Assets      []Asset `json:"assets"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Host != "codex" || manifest.HostVersion != CodexPinnedVersion || len(manifest.Assets) < 3 {
		t.Fatal("protocol reference pin missing")
	}
	for _, asset := range manifest.Assets {
		raw, err := os.ReadFile(filepath.Join(root, asset.Path))
		if err != nil || Hash(raw) != asset.SHA256 || !json.Valid(raw) {
			t.Fatalf("protocol reference changed: %s", asset.Path)
		}
	}
}
