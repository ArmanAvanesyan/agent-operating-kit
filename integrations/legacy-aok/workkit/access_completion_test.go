package workkit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestJiraOAuthRefreshSiteIdentityAndRotation(t *testing.T) {
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	bytes, _ := os.ReadFile(exe)
	sum := sha256.Sum256(bytes)
	for _, mode := range []string{"good", "wrong-site", "duplicate-site", "no-scope"} {
		t.Run(mode, func(t *testing.T) {
			c := authConfig("jira-cloud")
			c.Mode = "oauth2"
			c.Broker = &a.ToolPin{ID: "workkit-secret-broker", Path: exe, SHA256: hex.EncodeToString(sum[:]), Platform: runtime.GOOS + "-" + runtime.GOARCH, VersionOutput: "fixture"}
			c.SecretReferences = map[string]string{"clientSecret": "P3_CLIENT_SECRET"}
			c.OAuth = &a.OAuth{ClientID: "registered-app", AuthorizationURL: "https://auth.atlassian.com/authorize", TokenURL: "https://auth.atlassian.com/oauth/token", RedirectURI: "http://127.0.0.1:43123/callback", Scopes: []string{"read:jira-user", "offline_access"}}
			calls := 0
			b := AccessBackend{LookupEnv: func(ref string) (string, bool) {
				if ref != "P3_CLIENT_SECRET" {
					t.Fatal("unexpected secret")
				}
				return "synthetic-client-secret", true
			}, RoundTripper: readRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				body := ""
				switch r.URL.Host + r.URL.Path {
				case "auth.atlassian.com/oauth/token":
					if r.Header.Get("Content-Type") != "application/json" {
						t.Fatal("wrong token content type")
					}
					var form map[string]string
					json.NewDecoder(r.Body).Decode(&form)
					if form["grant_type"] != "refresh_token" || form["client_secret"] != "synthetic-client-secret" {
						t.Fatal("wrong confidential app request")
					}
					body = `{"access_token":"fresh-jira-access","refresh_token":"fresh-jira-refresh","scope":"read:jira-user","expires_in":3600}`
				case "api.atlassian.com/oauth/token/accessible-resources":
					item := map[string]any{"id": c.TenantID, "url": c.Origin, "scopes": []string{"read:jira-user"}}
					if mode == "wrong-site" {
						item["id"] = "other"
					}
					if mode == "no-scope" {
						item["scopes"] = []string{"write:jira-work"}
					}
					items := []any{item}
					if mode == "duplicate-site" {
						items = append(items, item)
					}
					raw, _ := json.Marshal(items)
					body = string(raw)
				case "api.atlassian.com/ex/jira/tenant/rest/api/3/myself":
					body = `{"accountId":"7"}`
				default:
					t.Fatal("unapproved OAuth destination", r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			result, e := b.Authenticate(context.Background(), "auth refresh", c, func() error { return nil })
			if mode == "good" {
				if e != nil || result.Principal != "7" || result.ScopedReadVerified || calls != 3 {
					t.Fatalf("%+v %v calls=%d", result, e, calls)
				}
			} else if e == nil {
				t.Fatal("wrong Jira site access accepted")
			}
		})
	}
}
func TestNativeGHRequiresExactSignedInheritedControls(t *testing.T) { nativeFixture(t, "github") }
func TestNativeAzureUsesPinnedExtensionParent(t *testing.T)         { nativeFixture(t, "azure-devops") }
func nativeFixture(t *testing.T, provider string) {
	if runtime.GOOS == "windows" {
		t.Skip("native effectful durability is separately blocked")
	}
	dir := backendDir(t)
	profileRoot := filepath.Join(dir, "native-profile")
	os.Mkdir(profileRoot, 0700)
	ledger := filepath.Join(dir, "ledger")
	os.Mkdir(ledger, 0700)
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	rawExe, _ := os.ReadFile(exe)
	hash := sha256.Sum256(rawExe)
	sha := hex.EncodeToString(hash[:])
	tree, e := sdk.HashLocalRuntime(context.Background(), profileRoot)
	if e != nil {
		t.Fatal(e)
	}
	c := authConfig(provider)
	c.BasePath = ""
	c.Mode = "gh-native"
	if provider == "azure-devops" {
		c.Mode = "az-native"
	}
	if provider == "jira-cloud" {
		c.Mode = "acli-native"
	}
	transport, apiVersion := "gh", "2022-11-28"
	if provider == "azure-devops" {
		transport, apiVersion = "az", "7.1"
	}
	if provider == "jira-cloud" {
		transport, apiVersion = "acli", "3"
	}
	c.Profile = backendJSON(map[string]any{"apiVersion": "workkit/v1alpha1", "kind": "ConnectionProfile", "metadata": map[string]any{"id": c.ConnectionID, "revision": 1}, "spec": map[string]any{"provider": provider, "origin": c.Origin, "basePath": "", "transport": transport, "authRef": "credentials/fixture", "scope": map[string]any{"tenantId": c.TenantID, "containerIds": c.Containers}, "apiVersion": apiVersion}})
	c.Tool = &a.ToolPin{ID: "gh", Path: exe, SHA256: sha, VersionOutput: "fixture", Platform: runtime.GOOS + "-" + runtime.GOARCH}
	if provider == "azure-devops" {
		c.Tool.ID = "az"
		extensionRoot := filepath.Join(dir, "extensions")
		os.MkdirAll(filepath.Join(extensionRoot, "azure-devops", "azext_devops"), 0700)
		os.WriteFile(filepath.Join(extensionRoot, "azure-devops", "azext_devops", "__init__.py"), []byte("synthetic"), 0600)
		tree, e := sdk.HashLocalRuntime(context.Background(), extensionRoot)
		if e != nil {
			t.Fatal(e)
		}
		c.Tool.Extensions = []a.Extension{{ID: "azure-devops", Version: "fixture", Path: extensionRoot, SHA256: tree, Layout: "tree-v1"}}
	}
	if provider == "jira-cloud" {
		c.Tool.ID = "acli"
	}
	now := time.Now().UTC()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	start, startErr := sdk.CurrentProcessStart()
	if startErr != nil {
		t.Fatal(startErr)
	}
	c.Native = &a.NativeEnrollment{ProcessStart: start, CredentialMode: "token", ProfileRoot: profileRoot, ProfileSHA256: tree, ProcessID: os.Getpid(), ProcessSHA256: sha, ToolSHA256: sha, OperationID: "native-1", ImplementationSHA256: sdk.AccessImplementationSHA256(), ObservedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), Issuer: "fixture", Controls: map[string]string{}}
	if provider == "azure-devops" {
		c.Native.CredentialMode = "pat"
	}
	if provider == "jira-cloud" {
		c.Native.CredentialMode = "jira-basic"
		c.Native.StatusSHA256 = sdk.SHA256([]byte("synthetic-acli-status"))
	}
	for _, name := range []string{"filesystem", "network", "environment", "credential-store", "no-implicit-refresh", "stop-monitor"} {
		c.Native.Controls[name] = "independently-observed"
	}
	c.Native.ScopeSHA256 = sdk.AuthScopeSHA256(c)
	c.Native.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sdk.NativeSigningBytes(*c.Native)))
	request := backendJSON(c)
	policy := backendJSON(a.Policy{Version: a.Version, Kind: "AccessPolicy", LedgerRoot: ledger, Issuers: []a.Issuer{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(pub)}}, AllowedConnections: []string{c.ConnectionID}, AllowedActions: []string{"auth login"}, NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(2 * time.Minute).Format(time.RFC3339Nano)})
	out := filepath.Join(dir, "attempt")
	g := a.Grant{Version: a.Version, Kind: "AccessGrant", OperationID: "native-1", Action: "auth login", RequestSHA256: sdk.SHA256(request), PolicySHA256: sdk.SHA256(policy), ImplementationSHA256: sdk.AccessImplementationSHA256(), Output: out, Issuer: "fixture", IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sdk.AccessSigningBytes(g)))
	calls := 0
	b := AccessBackend{LookupEnv: func(ref string) (string, bool) {
		if ref == "WK_P3_FIXTURE_USERNAME" {
			return "native-fixture-user", true
		}
		return "native-synthetic-token", true
	}}
	if provider == "jira-cloud" {
		b.RoundTripper = readRoundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			u, p, ok := req.BasicAuth()
			if !ok || u != "native-fixture-user" || p != "native-synthetic-token" {
				t.Fatal("hybrid credentials not explicit basic")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"accountId":"7"}`)), Request: req}, nil
		})
	}
	receipt, e := sdk.New(nil).WithAccessBackend(b).RunAccess(context.Background(), "auth login", request, policy, backendJSON(g), out)
	if e != nil || receipt.Status != "ok" || receipt.Binding.AuthorizationGranted {
		t.Fatalf("native fixture failed %+v %v", receipt, e)
	}
	if provider == "jira-cloud" {
		if calls != 1 {
			t.Fatal("hybrid did not verify immutable principal")
		}
		c.Native.StatusSHA256 = sdk.SHA256([]byte("wrong"))
		c.Native.OperationID = "native-2"
		c.Native.ScopeSHA256 = sdk.AuthScopeSHA256(c)
		c.Native.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sdk.NativeSigningBytes(*c.Native)))
		request = backendJSON(c)
		g.OperationID = "native-2"
		g.Output = out + "-wrong"
		g.RequestSHA256 = sdk.SHA256(request)
		g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sdk.AccessSigningBytes(g)))
		if _, e = sdk.New(nil).WithAccessBackend(b).RunAccess(context.Background(), "auth login", request, policy, backendJSON(g), g.Output); e == nil || calls != 1 {
			t.Fatal("changed native status reached HTTP")
		}
	}
}

func TestNativeAcliHybridStatusAndBasicIdentity(t *testing.T) { nativeFixture(t, "jira-cloud") }
