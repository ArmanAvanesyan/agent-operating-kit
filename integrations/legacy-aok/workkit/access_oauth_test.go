package workkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "jira" {
		os.Stdout.Write([]byte("synthetic-acli-status"))
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "devops" {
		root := os.Getenv("AZURE_EXTENSION_DIR")
		if os.Getenv("AZURE_DEVOPS_EXT_PAT") != "native-synthetic-token" || os.Getenv("AZURE_EXTENSION_USE_DYNAMIC_INSTALL") != "no" {
			os.Exit(8)
		}
		if _, e := os.Stat(filepath.Join(root, "azure-devops", "azext_devops", "__init__.py")); e != nil {
			os.Exit(9)
		}
		os.Stdout.Write([]byte(`{"authorizedUser":{"id":"7"}}`))
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "api" {
		if os.Getenv("GH_TOKEN") != "native-synthetic-token" {
			os.Exit(8)
		}
		os.Stdout.Write([]byte(`{"id":7}`))
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "exchange" {
		var r a.BrokerRequest
		if json.NewDecoder(io.LimitReader(os.Stdin, 16385)).Decode(&r) != nil {
			os.Exit(9)
		}
		mode := map[string]string{"darwin": "os-keychain", "linux": "secret-service", "windows": "windows-credential-manager"}[runtime.GOOS]
		generation := "generation-1"
		secrets := map[string]string{"accessToken": "old-access-token", "refreshToken": "old-refresh-token", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), "scope": "read"}
		if r.Operation == "compare-and-swap" {
			generation = "generation-2"
			secrets = r.Secrets
			if r.ConnectionID == "unchanged-generation" {
				generation = "generation-1"
			}
			if r.ConnectionID == "wrong-token" {
				secrets["accessToken"] = "unrelated-token"
			}
		}
		if r.Operation == "status" {
			secrets = map[string]string{}
		}
		if r.Operation == "delete" {
			generation = "generation-2"
			secrets = map[string]string{}
		}
		json.NewEncoder(os.Stdout).Encode(a.BrokerResponse{Version: a.Version, ConnectionID: r.ConnectionID, ScopeSHA256: r.ScopeSHA256, Generation: generation, StorageMode: mode, Secrets: secrets})
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestP3FullLinearRefreshBrokerCASIdentity(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	executable, e = filepath.EvalSymlinks(executable)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(executable)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	for _, mode := range []string{"good", "scope", "unchanged-generation", "wrong-token", "identity-failure"} {
		t.Run(mode, func(t *testing.T) {
			c := authConfig("linear")
			c.Mode = "oauth2"
			c.ConnectionID = mode
			c.SecretReferences = map[string]string{}
			c.Broker = &a.ToolPin{ID: "workkit-secret-broker", Path: executable, SHA256: hex.EncodeToString(hash[:]), VersionOutput: "fixture", Platform: runtime.GOOS + "-" + runtime.GOARCH, Extensions: []a.Extension{}}
			c.OAuth = &a.OAuth{ClientID: "fixture", AuthorizationURL: "https://linear.app/oauth/authorize", TokenURL: "https://api.linear.app/oauth/token", RevokeURL: "https://api.linear.app/oauth/revoke", RedirectURI: "http://127.0.0.1:43123/callback", Scopes: []string{"read"}}
			var tokenCalls, identityCalls atomic.Int32
			b := AccessBackend{LookupEnv: func(string) (string, bool) { t.Fatal("unexpected ambient credentials"); return "", false }, RoundTripper: readRoundTrip(func(r *http.Request) (*http.Response, error) {
				response := ""
				if r.URL.Path == "/oauth/token" {
					tokenCalls.Add(1)
					response = `{"access_token":"fresh-access-token","refresh_token":"fresh-refresh-token","scope":"read","expires_in":3600,"token_type":"Bearer"}`
					if mode == "scope" {
						response = strings.Replace(response, `"scope":"read"`, `"scope":"write"`, 1)
					}
				} else if r.URL.Path == "/graphql" {
					identityCalls.Add(1)
					if r.Header.Get("Authorization") != "Bearer fresh-access-token" {
						t.Fatal("wrong token used")
					}
					response = `{"data":{"viewer":{"id":"7","organization":{"id":"tenant"}}}}`
					if mode == "identity-failure" {
						response = `{"errors":[{"message":"private-provider-error"}]}`
					}
				} else {
					t.Fatal("unexpected destination")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
			})}
			result, e := b.Authenticate(context.Background(), "auth refresh", c, func() error { return nil })
			if mode == "good" {
				if e != nil || result.Generation != "generation-2" || result.StorageMode == "external-reference" {
					t.Fatalf("refresh failed %+v %v", result, e)
				}
			} else if e == nil {
				t.Fatal("bad refresh accepted")
			}
			if tokenCalls.Load() != 1 {
				t.Fatal("token refresh retried")
			}
			if mode != "good" && mode != "identity-failure" && identityCalls.Load() != 0 {
				t.Fatal("bad persistence advanced to identity")
			}
		})
	}
}
