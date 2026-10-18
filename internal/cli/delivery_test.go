package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryHelpDoesNotRequirePython(t *testing.T) {
	t.Setenv("AOK_DELIVERY_PYTHON", filepath.Join(t.TempDir(), "absent"))
	var out, stderr bytes.Buffer
	if code := Run([]string{"delivery", "--help"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "Usage: aok delivery") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), stderr.String())
	}
}

func TestDeliveryDoctorJSONDoesNotRequirePython(t *testing.T) {
	root := fakeKit(t)
	t.Setenv("AOK_ROOT", root)
	t.Setenv("AOK_DELIVERY_PYTHON", filepath.Join(t.TempDir(), "absent"))
	var out, stderr bytes.Buffer
	code := Run([]string{"delivery", "doctor", "--json"}, &out, &stderr)
	var report struct {
		Delivery deliveryDoctor `json:"delivery"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid doctor JSON: %v: %s", err, out.String())
	}
	if code != 1 || report.Delivery.Python != "" || report.Delivery.Payload != filepath.Join(root, "packs", "repository-delivery") {
		t.Fatalf("code=%d report=%+v stderr=%q", code, report, stderr.String())
	}
	if !strings.Contains(out.String(), `"requiredOk"`) {
		t.Fatalf("legacy doctor fields missing: %s", out.String())
	}
}

func TestDeliveryMissingPayloadIsActionable(t *testing.T) {
	root := fakeKit(t)
	t.Setenv("AOK_ROOT", root)
	var out, stderr bytes.Buffer
	if code := Run([]string{"delivery", "run"}, &out, &stderr); code != 1 || !strings.Contains(stderr.String(), "payload is missing") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestDeliveryRejectsOldExplicitPythonWithoutFallback(t *testing.T) {
	root := fakeKit(t)
	launcher := filepath.Join(root, "packs", "repository-delivery", deliveryLauncher)
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPython := filepath.Join(t.TempDir(), "python3")
	if err := os.WriteFile(oldPython, []byte("#!/bin/sh\necho 3.10.9\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOK_ROOT", root)
	t.Setenv("AOK_DELIVERY_PYTHON", oldPython)
	var out, stderr bytes.Buffer
	if code := Run([]string{"delivery", "run"}, &out, &stderr); code != 1 || !strings.Contains(stderr.String(), "Python 3.11 or newer is required") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestDeliveryCanonicalLauncherHelp(t *testing.T) {
	if _, _, err := findDeliveryPython(); err != nil {
		t.Skip(err)
	}
	t.Setenv("AOK_DELIVERY_PYTHON", "")
	t.Setenv("AOK_ROOT", "")
	var out, stderr bytes.Buffer
	if code := Run([]string{"delivery", "inspect", "--help"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "--repo") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}

func TestDeliveryDoctorReportsLocalFeatureVersions(t *testing.T) {
	root := fakeKit(t)
	launcher := filepath.Join(root, "packs", "repository-delivery", deliveryLauncher)
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(t.TempDir(), "python")
	script := "#!/bin/sh\ncase \"$3\" in *sys.version_info*) echo 3.12.2;; *importlib.metadata*) echo 4.26.1;; *) exit 1;; esac\n"
	if err := os.WriteFile(python, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOK_ROOT", root)
	t.Setenv("AOK_DELIVERY_PYTHON", python)
	var out bytes.Buffer
	if _, err := runDeliveryDoctor(&out); err != nil {
		t.Fatalf("err=%v", err)
	}
	var report struct {
		Delivery deliveryDoctor `json:"delivery"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Delivery.Python != python || report.Delivery.PythonVersion != "3.12.2" || !report.Delivery.JSONSchema || report.Delivery.JSONSchemaVersion != "4.26.1" {
		t.Fatalf("doctor report did not report isolated probes: %+v", report)
	}
}

func TestDeliveryForwardsArgumentsAndChildExit(t *testing.T) {
	root := fakeKit(t)
	launcher := filepath.Join(root, "packs", "repository-delivery", deliveryLauncher)
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(t.TempDir(), "fake python")
	argsPath := filepath.Join(t.TempDir(), "args")
	stdinPath := filepath.Join(t.TempDir(), "stdin")
	stdin, err := os.CreateTemp(t.TempDir(), "stdin-input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.WriteString("inherited input"); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = originalStdin; stdin.Close() })
	script := "#!/bin/sh\nif [ \"$2\" = \"-c\" ]; then echo 3.12.0; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\"\npwd > \"$CAPTURE_CWD\"\ncat > \"$CAPTURE_STDIN\"\nprintf 'child stdout'\nprintf 'child stderr' >&2\nexit 23\n"
	if err := os.WriteFile(python, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOK_ROOT", root)
	t.Setenv("AOK_DELIVERY_PYTHON", python)
	t.Setenv("CAPTURE_ARGS", argsPath)
	t.Setenv("CAPTURE_CWD", filepath.Join(t.TempDir(), "cwd"))
	t.Setenv("CAPTURE_STDIN", stdinPath)
	var out, stderr bytes.Buffer
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	code := Run([]string{"delivery", "run", "--repo", "path with spaces"}, &out, &stderr)
	got, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "-I\n" + launcher + "\nrun\n--repo\npath with spaces\n"
	gotCWD, err := os.ReadFile(os.Getenv("CAPTURE_CWD"))
	if err != nil {
		t.Fatal(err)
	}
	gotStdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if code != 23 || string(got) != want || strings.TrimSpace(string(gotCWD)) != cwd || string(gotStdin) != "inherited input" || out.String() != "child stdout" || stderr.String() != "child stderr" {
		t.Fatalf("code=%d args=%q cwd=%q stdin=%q stdout=%q stderr=%q", code, got, gotCWD, gotStdin, out.String(), stderr.String())
	}
}

func fakeKit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".codex-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex-plugin", "plugin.json"), []byte(`{"name":"agent-operating-kit"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
