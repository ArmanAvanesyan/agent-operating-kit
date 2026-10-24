package verifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/artifacts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestUploadingControlsTransfersBeforeExecuteAndAcceptsExactConflict(t *testing.T) {
	f := newServiceFixture(t)
	calls := []string{}
	proofSHA, _ := json.Marshal(f.proof)
	sum := sha256.Sum256(proofSHA)
	proofDigest := hex.EncodeToString(sum[:])
	route := verificationRoute(f, transportFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture-bearer" {
			t.Fatal("authorization missing")
		}
		switch r.URL.Path {
		case "/v1/candidates":
			if r.Header.Get("X-AOK-Candidate-SHA256") != f.c.Digest || r.Header.Get("X-AOK-Snapshot-SHA256") != f.c.SnapshotSHA256 {
				t.Fatal("candidate identity headers missing")
			}
			if r.Header.Get("X-AOK-Control-Proof") == "" {
				t.Fatal("signed control proof missing from artifact transfer")
			}
			_, _ = io.Copy(io.Discard, r.Body)
			b, _ := json.Marshal(map[string]string{"candidateSha256": f.c.Digest, "snapshotSha256": f.c.SnapshotSHA256})
			return response(http.StatusConflict, string(b)), nil
		case "/v1/execute":
			var q executeRequest
			if json.NewDecoder(r.Body).Decode(&q) != nil {
				t.Fatal("invalid execute request")
			}
			if q.CandidateSHA256 != f.c.Digest || q.SnapshotSHA256 != f.c.SnapshotSHA256 {
				t.Fatal("execute request lost candidate identity")
			}
			b, _ := json.Marshal(executeResponse{Protocol: "aok-verifier-transport/v1", ProofSHA256: proofDigest, ExitCode: 0, StdoutBase64: "b2s="})
			return response(http.StatusOK, string(b)), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return nil, nil
	}))
	c, err := NewUploadingControls(route, f.proof, f.spec, f.c)
	if err != nil {
		t.Fatal(err)
	}
	code, out, err := c.Execute(context.Background(), f.spec.Argv, f.spec.Environment, time.Duration(f.spec.TimeoutSeconds)*time.Second, f.spec.MaxOutputBytes)
	if err != nil || code != 0 || string(out) != "ok" {
		t.Fatalf("execute code=%d output=%q err=%v", code, out, err)
	}
	if len(calls) != 2 || calls[0] != "/v1/candidates" || calls[1] != "/v1/execute" {
		t.Fatalf("upload/execute order %v", calls)
	}
}

func TestUploadingControlsRejectsConflictWithDifferentSnapshotAndRedirect(t *testing.T) {
	f := newServiceFixture(t)
	executeCalls := 0
	for _, tc := range []struct {
		name string
		rt   transportFunc
	}{{"wrong conflict snapshot", func(r *http.Request) (*http.Response, error) {
		b, _ := json.Marshal(map[string]string{"candidateSha256": f.c.Digest, "snapshotSha256": strings.Repeat("a", 64)})
		return response(http.StatusConflict, string(b)), nil
	}}, {"redirect", func(*http.Request) (*http.Response, error) {
		v := response(http.StatusTemporaryRedirect, "")
		v.Header.Set("Location", "https://elsewhere.example/v1/candidates")
		return v, nil
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			rt := transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/v1/execute" {
					executeCalls++
				}
				return tc.rt(r)
			})
			route := verificationRoute(f, rt)
			c, e := NewUploadingControls(route, f.proof, f.spec, f.c)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = c.Execute(context.Background(), f.spec.Argv, f.spec.Environment, time.Duration(f.spec.TimeoutSeconds)*time.Second, f.spec.MaxOutputBytes); e == nil {
				t.Fatal("unverified upload response accepted")
			}
		})
	}
	if executeCalls != 0 {
		t.Fatal("execute called after invalid upload result")
	}
}

func verificationRoute(f serviceFixture, rt http.RoundTripper) verification.RemoteRoute {
	return verification.RemoteRoute{Endpoint: "https://verify.example.test/v1/execute", AuthReference: "broker-ref", Resolver: verification.RemoteSecretResolverFunc(func(context.Context, string) (string, error) { return "fixture-bearer", nil }), RoundTripper: rt}
}

func TestDockerExecutorPinsSandboxArgumentsAndRequiresCleanup(t *testing.T) {
	f := newServiceFixture(t)
	root := t.TempDir()
	log := filepath.Join(root, "calls.log")
	script := filepath.Join(root, "docker-fixture")
	marker := filepath.Join(root, "container.marker")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + log + "\nprintf 'token=%s\\n' \"${AOK_TEST_SECRET-unset}\" >> " + filepath.Join(root, "environment.log") + "\ncase \"$5\" in\ncreate) : > " + marker + "; printf '0123456789abcdef0123456789abcdef\\n' ;;\nstart) printf 'fixture output\\n' ;;\nwait) printf '7\\n' ;;\nstop) exit 0 ;;\nrm) /bin/rm -f " + marker + " ;;\nps) if [ -e " + marker + " ]; then printf '0123456789abcdef0123456789abcdef\\n'; fi ;;\n*) exit 2 ;;\nesac\nexit 0\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(script)
	sum := sha256.Sum256(raw)
	socket := filepath.Join(root, "docker.sock")
	configDir := filepath.Join(root, "docker-config")
	err := os.Mkdir(configDir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	d := DockerExecutor{DockerPath: script, DockerSHA256: hex.EncodeToString(sum[:]), DockerHost: "unix://" + socket, DockerConfigDir: configDir, ImageDigest: "example.test/verifier@sha256:" + strings.Repeat("a", 64), MemoryLimit: "512m", PidsLimit: 32, MaxCPUs: "1.0"}
	identity, err := d.IdentitySHA256()
	if err != nil || len(identity) != 64 {
		t.Fatalf("executor identity: %q %v", identity, err)
	}
	changed := d
	changed.MaxCPUs = "2.0"
	changedIdentity, e := changed.IdentitySHA256()
	if e != nil || changedIdentity == identity {
		t.Fatal("controls resource pin did not change executor identity")
	}
	policy := f.policy
	policy.Issuers = append([]verification.TrustedIssuer(nil), policy.Issuers...)
	policy.Issuers[0].ExecutorSHA256 = identity
	store, err := artifacts.NewStore(filepath.Join(root, "protected-store"), policy, f.spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Store: store, Policy: policy, VerifierID: f.spec.ID, AuthReference: "broker-ref", Resolver: SecretResolverFunc(func(context.Context, string) (string, error) { return "fixture-bearer", nil }), Executor: d, AllowInsecureForTests: true}
	if _, err = NewHandler(cfg); err != nil {
		t.Fatalf("actual executor identity did not satisfy trusted issuer pin: %v", err)
	}
	policy.Issuers[0].ExecutorSHA256 = hash("operator assertion")
	if _, err = NewHandler(Config{Store: store, Policy: policy, VerifierID: f.spec.ID, AuthReference: "broker-ref", Resolver: cfg.Resolver, Executor: d, AllowInsecureForTests: true}); err == nil {
		t.Fatal("arbitrary executor identity accepted")
	}
	oldSecret, hadSecret := os.LookupEnv("AOK_TEST_SECRET")
	if err = os.Setenv("AOK_TEST_SECRET", "ambient-secret"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if hadSecret {
			_ = os.Setenv("AOK_TEST_SECRET", oldSecret)
		} else {
			_ = os.Unsetenv("AOK_TEST_SECRET")
		}
	}()
	code, out, err := d.Execute(context.Background(), f.c, f.spec.Argv, f.spec.Environment, 2*time.Second, 1024)
	if err != nil || code != 7 || !strings.Contains(string(out), "fixture output") {
		t.Fatalf("fixture docker result code=%d output=%q err=%v", code, out, err)
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(args)
	for _, want := range []string{"--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--memory-swap\n512m", "type=bind,src=" + f.c.BundlePath + ",dst=/candidate,readonly", "--tmpfs\n/tmp:rw,noexec,nosuid,nodev,size=64m", "--env\nMODE=offline", "--env\nHOME=/nonexistent"} {
		if !strings.Contains(text, want) {
			t.Fatalf("sandbox flag %q missing from args: %s", want, text)
		}
	}
	if strings.Contains(text, "--mount\ntype=bind,src="+f.c.BundlePath+",dst=/candidate\n") || !strings.Contains(text, "rm\n--force\n") {
		t.Fatalf("writable mount or missing cleanup: %s", text)
	}
	envLog, _ := os.ReadFile(filepath.Join(root, "environment.log"))
	if strings.Contains(string(envLog), "ambient-secret") || !strings.Contains(string(envLog), "token=unset") {
		t.Fatalf("ambient environment inherited by Docker client: %q", envLog)
	}
	badScript := filepath.Join(root, "docker-bad-cleanup")
	badContent := strings.Replace(content, "rm) /bin/rm -f "+marker+" ;;", "rm) exit 1 ;;", 1)
	if err = os.WriteFile(badScript, []byte(badContent), 0700); err != nil {
		t.Fatal(err)
	}
	badRaw, _ := os.ReadFile(badScript)
	badSum := sha256.Sum256(badRaw)
	d.DockerPath = badScript
	d.DockerSHA256 = hex.EncodeToString(badSum[:])
	if _, _, err = d.Execute(context.Background(), f.c, f.spec.Argv, f.spec.Environment, 2*time.Second, 1024); err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("unconfirmed sandbox cleanup accepted: %v", err)
	}
}
