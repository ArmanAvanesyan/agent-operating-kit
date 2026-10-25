package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func readTestDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func readConfig(t *testing.T, id string, s readsource.Source) []byte {
	t.Helper()
	provider := providerName(s.Provider)
	transport := "rest"
	if provider == "github" {
		transport = "gh"
	}
	if provider == "linear" {
		transport = "graphql"
	}
	base := ""
	spec := map[string]any{"provider": provider, "origin": s.Origin, "basePath": base, "transport": transport, "authRef": "credentials/" + id, "scope": map[string]any{"tenantId": s.NativeTenantID, "containerIds": []string{s.ContainerID}}, "apiVersion": s.APIVersion}
	if s.Deployment != nil {
		spec["basePath"] = s.Deployment.BasePath
		spec["deployment"] = map[string]any{"serverVersion": s.Deployment.ServerVersion, "tlsTrust": map[string]string{"path": "assets/ca.pem", "sha256": s.Deployment.TLSTrustSHA256}}
	}
	profile := encodedRead(map[string]any{"apiVersion": "workkit/v1alpha1", "kind": "ConnectionProfile", "metadata": map[string]any{"id": id, "revision": 1}, "spec": spec})
	raw := encodedRead(r.Configuration{Version: r.Version, ConnectionID: id, Source: s, Profile: profile})
	if _, _, e := writeReadConfig(t, raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
func writeReadConfig(t *testing.T, raw []byte) (string, string, error) {
	t.Helper()
	dir := readTestDir(t)
	path := filepath.Join(dir, "connection.json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	_, _, e := LoadReadConfiguration(context.Background(), path)
	return dir, path, e
}
func importFixture(t *testing.T, provider, id string) (string, r.Manifest) {
	t.Helper()
	manifest := filepath.Join("../../../internal/tracker/testdata/offline", provider, "manifest.json")
	raw, e := os.ReadFile(manifest)
	if e != nil {
		t.Fatal(e)
	}
	var original readsource.Manifest
	if e = DecodeRead(raw, &original); e != nil {
		t.Fatal(e)
	}
	cfg := readConfig(t, id, original.Source)
	dir, path, e := writeReadConfig(t, cfg)
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(dir, "bundle")
	m, e := ImportRead(context.Background(), path, manifest, out)
	if e != nil {
		t.Fatal(e)
	}
	return out, m
}
func TestReadFiveProviderImportsAndExactAssetIntegrity(t *testing.T) {
	for _, provider := range []string{"github", "azure-devops", "linear", "jira"} {
		t.Run(provider, func(t *testing.T) {
			root, m := importFixture(t, provider, "connection")
			b, e := LoadReadBundle(context.Background(), root)
			if e != nil {
				t.Fatal(e)
			}
			if len(b.Records()) != 1 || m.AuthorizationGranted || m.ExecutionReady || m.MigrationActivated || m.ProductionActivated || m.LiveQualification != "not-established" {
				t.Fatalf("unexpected read claims: %+v", m)
			}
			if b.Records()[0].Identity.NativeID == "" || b.Manifest().OriginalObservedAt == nil || *m.OriginalObservedAt != "2026-10-24T12:00:00Z" {
				t.Fatal("native identity/time lost")
			}
			copy := b.Records()
			copy[0].Data.Fields["bad"] = json.RawMessage(`true`)
			if _, ok := b.Records()[0].Data.Fields["bad"]; ok {
				t.Fatal("mutable read snapshot")
			}
			for _, a := range m.Assets {
				if a.Role == "source" && strings.Contains(a.Path, "assets/0000") {
					p := filepath.Join(root, a.Path)
					raw, _ := os.ReadFile(p)
					os.WriteFile(p, append(raw, ' '), 0600)
					if _, e = LoadReadBundle(context.Background(), root); e == nil {
						t.Fatal("changed source accepted")
					}
					break
				}
			}
		})
	}
	s := dcReadSource()
	raw := []byte(`{"id":"100","key":"ENG-1","fields":{"project":{"id":"10","key":"ENG"},"summary":"hello","description":"wiki","updated":"2026-10-24T12:00:00Z"}}`)
	a, _ := readsource.Provider(s.Provider)
	reqs, e := a.Initial(s, "capture", nil)
	if e != nil {
		t.Fatal(e)
	}
	assets := map[string][]byte{"assets/0000.json": raw}
	manifest := readsource.Manifest{Version: s.Version, Source: s, Mode: "capture", Pages: []readsource.Page{{Request: reqs[0], Status: 200, Headers: map[string]string{}, Asset: readsource.AssetRef{Path: "assets/0000.json", SHA256: SHA256(raw)}, ObservedAt: "2026-10-24T12:00:00Z"}}}
	capture, e := readsource.Reconstruct(manifest, assets)
	if e != nil {
		t.Fatal(e)
	}
	cfg := readConfig(t, "dc", s)
	m, files, e := buildReadBundle(cfg, capture, r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: s.PrincipalPseudonym, AuthMode: s.Auth.Mode}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(readTestDir(t), "dc")
	if e = engine.WriteBundle(context.Background(), out, files, ""); e != nil {
		t.Fatal(e)
	}
	bundle, e := LoadReadBundle(context.Background(), out)
	if e != nil {
		t.Fatal(e)
	}
	if m.Complete || bundle.Records()[0].Identity.BasePath != "/jira" || bundle.Records()[0].Identity.Provider != "jira-datacenter" {
		t.Fatal("DC partial/identity lost")
	}
}
func dcReadSource() readsource.Source {
	return readsource.Source{Version: readsource.Version2, Provider: "jira-datacenter", Origin: "https://jira.example.test", APIVersion: "2", NativeTenantID: "instance", ContainerID: "10", ContainerKey: "ENG", PrincipalPseudonym: "operator", MappingVersion: "1", Auth: readsource.Auth{Mode: "bearer", TokenEnv: "DC_TOKEN"}, Deployment: &readsource.Deployment{BasePath: "/jira", ServerVersion: "10.3", TLSTrustSHA256: strings.Repeat("a", 64)}, Scope: readsource.Scope{ItemIDs: []string{"100"}}}
}
func TestReadClosedRecordsUnsafePathsAndImmutableOutput(t *testing.T) {
	root, m := importFixture(t, "github", "gh")
	raw, _ := os.ReadFile(filepath.Join(root, "bundle.json"))
	var v map[string]json.RawMessage
	json.Unmarshal(raw, &v)
	delete(v, "authorizationGranted")
	changed, _ := json.Marshal(v)
	os.WriteFile(filepath.Join(root, "bundle.json"), changed, 0600)
	if _, e := LoadReadBundle(context.Background(), root); e == nil {
		t.Fatal("missing required false flag accepted")
	}
	os.WriteFile(filepath.Join(root, "bundle.json"), raw, 0600)
	for _, unsafe := range []string{"../escape", "/absolute", "assets/../escape", "assets\\escape", "assets/x:y"} {
		copy := m
		copy.Assets = append([]r.Asset{}, m.Assets...)
		copy.Assets[0].Path = unsafe
		os.WriteFile(filepath.Join(root, "bundle.json"), encodedRead(copy), 0600)
		if _, e := LoadReadBundle(context.Background(), root); e == nil {
			t.Fatal("unsafe asset accepted", unsafe)
		}
	}
	os.WriteFile(filepath.Join(root, "bundle.json"), raw, 0600)
	os.WriteFile(filepath.Join(root, "extra"), []byte("x"), 0600)
	if _, e := LoadReadBundle(context.Background(), root); e == nil {
		t.Fatal("unmanifested file accepted")
	}
	os.Remove(filepath.Join(root, "extra"))
	if e := engine.WriteBundle(context.Background(), root, map[string][]byte{}, ""); e == nil {
		t.Fatal("overwrite accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := LoadReadBundle(ctx, root); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestReadQueryThreeValuedExactAndFederatedScopes(t *testing.T) {
	a, _ := importFixture(t, "github", "a")
	b, _ := importFixture(t, "github", "b")
	q := r.Query{Version: r.Version, Dialect: "portable", Fields: map[string]r.Field{"title": {Type: "string", ProviderFields: map[string]string{"a": "title", "b": "title"}}, "missing": {Type: "string", ProviderFields: map[string]string{"a": "absent", "b": "absent"}}}, Expression: `not (missing == "x")`, Limit: 100, MaxAgeSeconds: 60}
	result, e := Federate(context.Background(), []string{b, a}, q, "2026-10-24T12:00:30Z")
	if e != nil || result.Unknown != 2 || result.Matched != 0 || result.Exhaustive {
		t.Fatalf("unknown negation: %+v %v", result, e)
	}
	q.Expression = `title != "not-the-title"`
	result, e = Federate(context.Background(), []string{b, a}, q, "2026-10-24T12:00:30Z")
	if e != nil || result.Returned != 2 || result.Records[0].ConnectionID != "a" || result.Records[1].ConnectionID != "b" || !result.Exhaustive {
		t.Fatalf("federation %+v %v", result, e)
	}
	q.Limit = 1
	limited, e := Federate(context.Background(), []string{a, b}, q, "2026-10-25T12:00:30Z")
	if e != nil || limited.Exhaustive || limited.Returned != 1 || limited.Coverage[0].Fresh {
		t.Fatal("limit/staleness claimed exhaustive")
	}
	q.Dialect = "provider-native"
	if _, e = Federate(context.Background(), []string{a, b}, q, "2026-10-24T12:00:30Z"); e == nil {
		t.Fatal("native query routed across providers")
	}
}

type readFixtureTransport struct {
	calls           atomic.Int32
	timeout         bool
	malformed       bool
	interruptSecond bool
}

func (t *readFixtureTransport) Do(ctx context.Context, s readsource.Source, req readsource.Request) (readsource.Response, error) {
	call := t.calls.Add(1)
	if t.interruptSecond && call == 2 {
		return readsource.Response{}, context.DeadlineExceeded
	}
	if t.timeout {
		<-ctx.Done()
		return readsource.Response{}, ctx.Err()
	}
	if t.malformed {
		return readsource.Response{Status: 200, Body: []byte(`{"invalid"`)}, nil
	}
	if req.Kind == "github-repository" {
		return readsource.Response{Status: 200, Body: []byte(`{"node_id":"repo","owner":{"node_id":"tenant"}}`)}, nil
	}
	return readsource.Response{Status: 200, Body: []byte(`{"node_id":"item","number":1,"title":"fixture","updated_at":"2026-10-25T12:00:00Z"}`)}, nil
}

type readFixtureBackend struct {
	transport     *readFixtureTransport
	opens         atomic.Int32
	identityError bool
}

func (b *readFixtureBackend) OpenRead(ctx context.Context, c, p, e []byte) (*ReadSession, error) {
	b.opens.Add(1)
	return NewReadSession(c, p, e, b.transport, func(ctx context.Context) error {
		if b.identityError {
			return errors.New("synthetic identity mismatch")
		}
		return ctx.Err()
	})
}
func enrollmentFixture(t *testing.T, out string, source readsource.Source) (string, string, string, []byte, []byte, []byte, ed25519.PrivateKey) {
	t.Helper()
	raw := readConfig(t, "fixture", source)
	dir, path, e := writeReadConfig(t, raw)
	if e != nil {
		t.Fatal(e)
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	rule := readsource.ReadRule{Provider: source.Provider, Origin: source.Origin, NativeTenantID: source.NativeTenantID, ContainerID: source.ContainerID, ContainerKey: source.ContainerKey, Auth: source.Auth, Deployment: source.Deployment, PrincipalPseudonym: source.PrincipalPseudonym}
	policy := encodedRead(r.AccessPolicy{Version: r.Version, LegacyReadPolicy: encodedRead(readsource.ReadPolicy{Version: source.Version, Rules: []readsource.ReadRule{rule}}), Issuers: []r.Issuer{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(pub)}}})
	now := time.Now().UTC().Truncate(time.Second)
	en := r.ReadEnrollment{Version: r.Version, ImplementationSHA256: ReadImplementationSHA256(), Action: "capture", ConfigurationSHA256: SHA256(raw), ReadPolicySHA256: SHA256(policy), Transport: r.TransportIdentity{Kind: "gh", Version: "fixture", ExecutableSHA256: strings.Repeat("a", 64), Principal: source.PrincipalPseudonym, AuthMode: "gh-token"}, Executable: "/synthetic/gh", SecretEnvironment: map[string]string{"GH_TOKEN": "AOK_GH_FIXTURE_TOKEN"}, IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ValidUntil: now.Add(3 * time.Minute).Format(time.RFC3339Nano), RequestSeconds: 5, CommandSeconds: 10, QualificationOnly: true, Issuer: "fixture", NativePrincipalID: "7", OutputPath: out}
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, EnrollmentSigningBytes(en)))
	enRaw := encodedRead(en)
	policyPath := filepath.Join(dir, "policy.json")
	enrollmentPath := filepath.Join(dir, "enrollment.json")
	os.WriteFile(policyPath, policy, 0600)
	os.WriteFile(enrollmentPath, enRaw, 0600)
	return path, policyPath, enrollmentPath, raw, policy, enRaw, priv
}
func basicReadSource() readsource.Source {
	return readsource.Source{Version: readsource.Version, Provider: "github", Origin: "https://api.github.com", APIVersion: "2022-11-28", NativeTenantID: "tenant", ContainerID: "repo", ContainerKey: "owner/repo", PrincipalPseudonym: "operator", MappingVersion: "1", Auth: readsource.Auth{Mode: "gh-session"}, Scope: readsource.Scope{ItemIDs: []string{"1"}}}
}
func TestReadSessionEnrollmentOutputConsumptionAndTimeoutPreservation(t *testing.T) {
	for _, mode := range []string{"success", "timeout", "malformed", "identity"} {
		t.Run(mode, func(t *testing.T) {
			out := filepath.Join(readTestDir(t), "capture")
			cfg, policy, en, _, _, _, _ := enrollmentFixture(t, out, basicReadSource())
			backend := &readFixtureBackend{transport: &readFixtureTransport{timeout: mode == "timeout", malformed: mode == "malformed"}, identityError: mode == "identity"}
			service := New(nil).WithReadBackend(backend)
			ctx := context.Background()
			if mode == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			m, e := service.CaptureRead(ctx, cfg, policy, en, "capture", "", out)
			if mode == "identity" {
				if e == nil || backend.transport.calls.Load() != 0 {
					t.Fatal("identity failure performed provider read")
				}
				if _, e = os.Stat(filepath.Join(out, "failure.json")); e != nil {
					t.Fatal("failure evidence missing", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			b, e := LoadReadBundle(context.Background(), out)
			if e != nil {
				t.Fatal(e)
			}
			if mode != "success" && m.Complete {
				t.Fatal("failed capture complete")
			}
			if mode == "success" && (!m.Complete || len(b.Records()) != 1) {
				t.Fatalf("valid fixture incomplete %+v", m)
			}
			calls := backend.transport.calls.Load()
			if _, e = service.CaptureRead(context.Background(), cfg, policy, en, "capture", "", out); e == nil {
				t.Fatal("output reused")
			}
			if backend.transport.calls.Load() != calls || backend.opens.Load() != 1 {
				t.Fatal("output reuse reached external bootstrap")
			}
		})
	}
}
func TestReadSignaturesScopeExpiryAndHistoricalInspection(t *testing.T) {
	out := filepath.Join(readTestDir(t), "capture")
	_, _, _, cfg, p, en, _ := enrollmentFixture(t, out, basicReadSource())
	now := time.Now().UTC()
	if _, _, _, e := ValidateReadEnrollment(cfg, p, en, now); e != nil {
		t.Fatal(e)
	}
	for _, which := range []string{"tamper", "wrong-policy", "expired", "wrong-config", "native-session"} {
		cc, pp, ee := append([]byte(nil), cfg...), append([]byte(nil), p...), append([]byte(nil), en...)
		at := now
		switch which {
		case "tamper":
			ee = bytes.Replace(ee, []byte(`"nativePrincipalId":"7"`), []byte(`"nativePrincipalId":"8"`), 1)
		case "wrong-policy":
			pp = append(pp, ' ')
		case "expired":
			at = now.Add(time.Hour)
		case "wrong-config":
			cc = append(cc, ' ')
		case "native-session":
			ee = bytes.Replace(ee, []byte(`"gh-token"`), []byte(`"gh-session"`), 1)
		}
		if _, _, _, e := ValidateReadEnrollment(cc, pp, ee, at); e == nil {
			t.Fatal("invalid enrollment accepted", which)
		}
	}
}
func TestCatalogPointerAndUnknownViewCapabilities(t *testing.T) {
	raw := []byte(`{"data":{"team":{"id":"team","states":{"nodes":[{"id":"a"}]}}}}`)
	pointer, known := catalogPointer(raw, []byte(`{"nodes":[{"id":"a"}]}`))
	if !known || pointer != "/data/team/states" {
		t.Fatal(pointer, known)
	}
	if _, known = catalogPointer([]byte(`{"a":1,"b":1}`), []byte(`1`)); known {
		t.Fatal("ambiguous provenance claimed")
	}
	root, _ := importFixture(t, "github", "github")
	report, e := ReadCatalog(context.Background(), root, "iterations", "2026-10-25T12:00:00Z")
	if e != nil || report["status"] != "unknown" {
		t.Fatal("unsupported catalog represented as empty success")
	}
	caps, e := ReadCapabilities(context.Background(), root, "2026-10-25T12:00:00Z")
	if e != nil || caps["liveQualification"] != "not-established" {
		t.Fatal("catalog granted qualification")
	}
}

func TestReadResumeOriginatingProofLineageAndOverlap(t *testing.T) {
	dir := readTestDir(t)
	first := filepath.Join(dir, "first")
	cfg, p, en, _, _, enRaw, key := enrollmentFixture(t, first, basicReadSource())
	backend := &readFixtureBackend{transport: &readFixtureTransport{interruptSecond: true}}
	service := New(nil).WithReadBackend(backend)
	m, e := service.CaptureRead(context.Background(), cfg, p, en, "capture", "", first)
	if e != nil || m.Complete {
		t.Fatal("partial capture required", e)
	}
	prior, e := LoadReadBundle(context.Background(), first)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := os.ReadFile(filepath.Join(first, "bundle.json"))
	var grant r.ReadEnrollment
	if DecodeRead(enRaw, &grant) != nil {
		t.Fatal("fixture grant")
	}
	grant.PreviousBundleSHA256 = prior.Digest()
	grant.IssuedAt = time.Now().UTC().Format(time.RFC3339Nano)
	grant.ValidUntil = time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
	sign := func(out string) string {
		t.Helper()
		copy := grant
		copy.OutputPath = out
		copy.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, EnrollmentSigningBytes(copy)))
		path := filepath.Join(readTestDir(t), "grant.json")
		os.WriteFile(path, encodedRead(copy), 0600)
		return path
	}
	overlap := filepath.Join(first, "forbidden-new-attempt")
	overlapGrant := sign(overlap)
	opens := backend.opens.Load()
	if _, e = service.CaptureRead(context.Background(), cfg, p, overlapGrant, "capture", first, overlap); e == nil {
		t.Fatal("output inside historical bundle accepted")
	}
	if backend.opens.Load() != opens {
		t.Fatal("overlap reached credentials")
	}
	if _, e = os.Stat(overlap); !os.IsNotExist(e) {
		t.Fatal("overlap modified historical inventory")
	}
	if _, e = LoadReadBundle(context.Background(), first); e != nil {
		t.Fatal("prior bundle invalidated", e)
	}
	backend.transport.interruptSecond = false
	second := filepath.Join(dir, "second")
	secondGrant := sign(second)
	completed, e := service.CaptureRead(context.Background(), cfg, p, secondGrant, "capture", first, second)
	if e != nil || !completed.Complete {
		t.Fatal("resume failed", e)
	}
	resumed, e := LoadReadBundle(context.Background(), second)
	if e != nil {
		t.Fatal("fresh enrollment invalidated inherited observation", e)
	}
	if resumed.Manifest().PreviousBundleSHA256 != prior.Digest() || len(resumed.lineage) != 4 || len(resumed.Records()) != 1 {
		t.Fatal("resume lineage incomplete")
	}
	now, _ := os.ReadFile(filepath.Join(first, "bundle.json"))
	if !bytes.Equal(original, now) {
		t.Fatal("historical evidence rewritten")
	}
	if *resumed.Manifest().OriginalObservedAt != *prior.Manifest().OriginalObservedAt {
		t.Fatal("inherited page observation renewed")
	}
	for name := range resumed.lineage {
		if strings.HasSuffix(name, "enrollment.json") {
			path := filepath.Join(second, name)
			raw, _ := os.ReadFile(path)
			os.WriteFile(path, append(raw, ' '), 0600)
			if _, e = LoadReadBundle(context.Background(), second); e == nil {
				t.Fatal("tampered lineage accepted")
			}
			break
		}
	}
}
func TestReadNullRequiredFieldsProfilePinsAndLimits(t *testing.T) {
	root, m := importFixture(t, "github", "gh")
	path := filepath.Join(root, "bundle.json")
	raw, _ := os.ReadFile(path)
	for _, field := range []string{"authorizationGranted", "complete", "liveQualification", "previousBundleSha256"} {
		var obj map[string]json.RawMessage
		json.Unmarshal(raw, &obj)
		obj[field] = json.RawMessage(`null`)
		changed, _ := json.Marshal(obj)
		os.WriteFile(path, changed, 0600)
		if _, e := LoadReadBundle(context.Background(), root); e == nil {
			t.Fatal("null normalized into default", field)
		}
	}
	m.PreviousBundleSHA256 = "not-a-digest"
	os.WriteFile(path, encodedRead(m), 0600)
	if _, e := LoadReadBundle(context.Background(), root); e == nil {
		t.Fatal("invalid lineage hash accepted")
	}
	cfgRaw := readConfig(t, "dc", dcReadSource())
	var cfg r.Configuration
	DecodeRead(cfgRaw, &cfg)
	cfg.Source.Deployment.ServerVersion = "other"
	if validateReadConfiguration(cfg) == nil {
		t.Fatal("server version disagreement accepted")
	}
	DecodeRead(cfgRaw, &cfg)
	cfg.Source.Deployment.TLSTrustSHA256 = strings.Repeat("b", 64)
	if validateReadConfiguration(cfg) == nil {
		t.Fatal("TLS pin disagreement accepted")
	}
	s := basicReadSource()
	cfgRaw = readConfig(t, "gh", s)
	DecodeRead(cfgRaw, &cfg)
	var profile map[string]any
	DecodeRead(cfg.Profile, &profile)
	profile["spec"].(map[string]any)["limits"] = map[string]any{"timeoutSeconds": 1, "maxItems": 1, "maxBytes": 100}
	cfg.Profile = encodedRead(profile)
	if validateReadConfiguration(cfg) == nil {
		t.Fatal("profile exceeded by source defaults")
	}
}
func TestReadBatchesBeyondWorkspaceResourceLimitViewsAndSemanticDiff(t *testing.T) {
	s := basicReadSource()
	s.Scope.ItemIDs = nil
	a, _ := readsource.Provider(s.Provider)
	reqs, e := a.Initial(s, "capture", nil)
	if e != nil {
		t.Fatal(e)
	}
	rows := []map[string]any{}
	for i := 0; i < 1101; i++ {
		title := "Z"
		if i == 1100 {
			title = "A"
		}
		rows = append(rows, map[string]any{"node_id": fmt.Sprintf("node-%d", i), "number": i + 1, "title": title, "updated_at": "2026-10-24T12:00:00Z"})
	}
	body := encodedRead(rows)
	repo := []byte(`{"node_id":"repo","owner":{"node_id":"tenant"}}`)
	manifest := readsource.Manifest{Version: s.Version, Source: s, Mode: "capture", Pages: []readsource.Page{{Request: reqs[0], Status: 200, Headers: map[string]string{}, Asset: readsource.AssetRef{Path: "assets/0000.json", SHA256: SHA256(repo)}, ObservedAt: "2026-10-24T12:00:00Z"}, {Request: reqs[1], Status: 200, Headers: map[string]string{}, Asset: readsource.AssetRef{Path: "assets/0001.json", SHA256: SHA256(body)}, ObservedAt: "2026-10-24T12:00:00Z"}}}
	c, e := readsource.Reconstruct(manifest, map[string][]byte{"assets/0000.json": repo, "assets/0001.json": body})
	if e != nil {
		t.Fatal(e)
	}
	cfg := readConfig(t, "gh", s)
	_, files, e := buildReadBundle(cfg, c, r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: s.PrincipalPseudonym, AuthMode: s.Auth.Mode}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(readTestDir(t), "batch")
	if e = engine.WriteBundle(context.Background(), root, files, ""); e != nil {
		t.Fatal(e)
	}
	b, e := LoadReadBundle(context.Background(), root)
	if e != nil || len(b.Records()) != 1101 {
		t.Fatal("item batch constrained by configuration limit", e)
	}
	q := r.Query{Version: r.Version, Dialect: "portable", Expression: `title != "none"`, Fields: map[string]r.Field{"title": {Type: "string", ProviderFields: map[string]string{"gh": "title"}}}, Limit: 1, MaxAgeSeconds: 60}
	view := encodedRead(map[string]any{"apiVersion": "workkit/v1alpha1", "kind": "ViewDefinition", "metadata": map[string]any{"id": "view", "revision": 1}, "spec": map[string]any{"location": "local", "query": map[string]any{"dialect": "portable", "expression": q.Expression}, "layout": "board", "groupBy": []string{"title"}, "sortBy": []string{"title"}, "columns": []string{"title"}}})
	report, e := EvaluateReadView(context.Background(), []string{root}, q, view, "2026-10-24T12:00:30Z")
	if e != nil {
		t.Fatal(e)
	}
	result := report["result"].(r.QueryResult)
	if result.Returned != 1 || string(result.Records[0].Data.Fields["title"]) != `"A"` || len(report["groups"].([]map[string]any)) != 1 {
		t.Fatal("view did not sort/group before limiting")
	}
	if sourceSemantic(map[string]any{"x": json.Number("1.0")}) != sourceSemantic(map[string]any{"x": json.Number("1")}) {
		t.Fatal("numeric lexical formatting became a semantic change")
	}
	old := c.Snapshot.Items[0]
	newer := old
	newer.Provenance = map[string][]readsource.Pointer{"title": {{AssetSHA256: strings.Repeat("f", 64)}}}
	if semanticReadItem(old) != semanticReadItem(newer) {
		t.Fatal("provenance change became semantic item change")
	}
	left, right := dcReadSource(), dcReadSource()
	right.Deployment = &readsource.Deployment{BasePath: "/other", ServerVersion: left.Deployment.ServerVersion, TLSTrustSHA256: left.Deployment.TLSTrustSHA256}
	if readIdentityKey(r.Configuration{ConnectionID: "dc", Source: left}) == readIdentityKey(r.Configuration{ConnectionID: "dc", Source: right}) {
		t.Fatal("Data Center installations collapsed")
	}
}
