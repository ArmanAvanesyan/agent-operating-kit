package workkit

import (
	"context"
	"encoding/json"
	"fmt"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func authConfig(provider string) a.Configuration {
	c := a.Configuration{Version: a.Version, Kind: "AuthConfiguration", ConnectionID: "fixture", Provider: provider, Origin: "https://fixture.example", NativePrincipal: "7", TenantID: "tenant", Containers: []string{"container"}, Mode: "token", SecretReferences: map[string]string{"token": "WK_P3_FIXTURE_TOKEN"}}
	switch provider {
	case "github":
		c.Origin = "https://github.com"
	case "azure-devops":
		c.Origin = "https://dev.azure.com"
		c.ProviderLookup = "fixture"
		c.BasePath = "/fixture"
		c.Mode = "pat"
	case "jira-cloud":
		c.Origin = "https://fixture.atlassian.net"
		c.Mode = "basic"
		c.SecretReferences["username"] = "WK_P3_FIXTURE_USERNAME"
	case "jira-datacenter":
		c.BasePath = "/jira"
	case "linear":
		c.Origin = "https://api.linear.app"
		c.Mode = "api-key"
	}
	return c
}
func TestP3ExplicitCredentialsIdentityAndNoMutation(t *testing.T) {
	const token = "synthetic-token-privacy-canary"
	for _, provider := range []string{"github", "azure-devops", "jira-cloud", "jira-datacenter", "linear"} {
		for _, mode := range []string{"good", "wrong-principal", "reflection", "duplicate-key", "permission", "redirect"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				c := authConfig(provider)
				var calls atomic.Int32
				b := AccessBackend{LookupEnv: func(ref string) (string, bool) {
					if ref == "WK_P3_FIXTURE_USERNAME" {
						return "fixture-user", true
					}
					if ref == "WK_P3_FIXTURE_TOKEN" {
						return token, true
					}
					t.Fatal("unrelated secret lookup")
					return "", false
				}, RoundTripper: readRoundTrip(func(q *http.Request) (*http.Response, error) {
					calls.Add(1)
					if q.Method != "GET" && !(provider == "linear" && q.Method == "POST") {
						t.Fatal("remote mutation")
					}
					auth := q.Header.Get("Authorization")
					if auth == "" || q.URL.User != nil {
						t.Fatal("missing or URL credential")
					}
					raw := `{"id":7}`
					switch provider {
					case "azure-devops":
						raw = `{"authorizedUser":{"id":"7"}}`
					case "jira-cloud":
						raw = `{"accountId":"7"}`
					case "jira-datacenter":
						raw = `{"key":"7"}`
					case "linear":
						raw = `{"data":{"viewer":{"id":"7","organization":{"id":"tenant"}}}}`
					}
					status := 200
					headers := http.Header{}
					switch mode {
					case "wrong-principal":
						raw = strings.ReplaceAll(raw, "7", "8")
					case "reflection":
						raw = fmt.Sprintf(`{"id":7,"private":"%s"}`, token)
					case "duplicate-key":
						raw = `{"id":7,"id":8}`
					case "permission":
						status = 403
					case "redirect":
						status = 302
						headers.Set("Location", "https://outside.example")
					}
					return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(raw)), Request: q}, nil
				})}
				result, e := b.Authenticate(context.Background(), "auth login", c, func() error { return nil })
				if mode == "good" {
					if e != nil || result.Status != "authenticated" || result.ScopedReadVerified || result.AuthorizationGranted || result.LiveQualification != "pending" {
						t.Fatalf("%+v %v", result, e)
					}
				} else if e == nil {
					t.Fatal("bad identity accepted")
				}
				encoded, _ := json.Marshal(result)
				if strings.Contains(string(encoded), token) || e != nil && strings.Contains(e.Error(), token) {
					t.Fatal("credential leak")
				}
				if calls.Load() != 1 {
					t.Fatal("retried or redirected")
				}
			})
		}
	}
}
func TestP3MissingCredentialAndLogoutDoNotCallProvider(t *testing.T) {
	c := authConfig("github")
	var calls atomic.Int32
	b := AccessBackend{LookupEnv: func(string) (string, bool) { return "", false }, RoundTripper: readRoundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		t.Fatal("unexpected network")
		return nil, nil
	})}
	if _, e := b.Authenticate(context.Background(), "auth login", c, func() error { return nil }); e == nil {
		t.Fatal("missing token accepted")
	}
	r, e := b.Authenticate(context.Background(), "auth logout", c, func() error { return nil })
	if e != nil || r.Status != "disconnected" || r.ProviderInteraction != "none" || calls.Load() != 0 {
		t.Fatal("external reference logout mutated provider")
	}
}
func TestP3OAuthExchangeValidityAndNoRetry(t *testing.T) {
	for _, bad := range []string{"good", "expiry", "scope", "duplicate", "missing-refresh", "response-loss"} {
		t.Run(bad, func(t *testing.T) {
			var calls atomic.Int32
			client := &http.Client{Transport: readRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Fatal("bad exchange")
				}
				if bad == "response-loss" {
					return nil, io.ErrUnexpectedEOF
				}
				raw := `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","expires_in":3600,"scope":"read","token_type":"Bearer"}`
				switch bad {
				case "expiry":
					raw = strings.Replace(raw, "3600", "0", 1)
				case "scope":
					raw = strings.Replace(raw, `"scope":"read"`, `"scope":""`, 1)
				case "duplicate":
					raw = strings.Replace(raw, `"expires_in":3600`, `"expires_in":3600,"expires_in":7200`, 1)
				case "missing-refresh":
					raw = strings.Replace(raw, `"refresh_token":"synthetic-refresh",`, "", 1)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
			})}
			tokens, e := oauthPost(context.Background(), client, "https://api.linear.app/oauth/token", url.Values{"grant_type": {"refresh_token"}})
			if bad == "good" {
				if e != nil || tokens["scope"] != "read" || tokens["expiresAt"] == "" {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("bad token response")
			}
			if calls.Load() != 1 {
				t.Fatal("exchange retried")
			}
		})
	}
}
func TestP3OAuthCallbackStatePKCEAndListenerCleanup(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	c := &a.OAuth{ClientID: "synthetic", AuthorizationURL: "https://linear.app/oauth/authorize", RedirectURI: "http://" + address + "/callback", Scopes: []string{"read"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, verifier, e := oauthChallenge(ctx, c, func(link string) error {
		u, _ := url.Parse(link)
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("state") == "" {
			t.Fatal("PKCE absent")
		}
		bad, e := http.Get(c.RedirectURI + "?state=wrong&code=synthetic")
		if e != nil {
			return e
		}
		bad.Body.Close()
		if bad.StatusCode != 400 {
			t.Fatal("wrong state")
		}
		resp, e := http.Get(c.RedirectURI + "?state=" + q.Get("state") + "&code=synthetic-code")
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if e != nil || code != "synthetic-code" || len(verifier) != 43 {
		t.Fatalf("callback failed %v", e)
	}
	rebound, e := net.Listen("tcp4", address)
	if e != nil {
		t.Fatal("callback listener leaked")
	}
	rebound.Close()
}

func TestP3GeneratedProviderLookupIdentityPaths(t *testing.T) {
	for _, provider := range []string{"github", "azure-devops", "jira", "linear"} {
		t.Run(provider, func(t *testing.T) {
			raw, e := os.ReadFile("../../../kits/workkit/testdata/access/" + provider + ".json")
			if e != nil {
				t.Fatal(e)
			}
			c, e := sdk.ValidateAuthConfiguration(raw)
			if e != nil {
				t.Fatal(e)
			}
			b := AccessBackend{LookupEnv: func(string) (string, bool) { return "synthetic-credential", true }, RoundTripper: readRoundTrip(func(r *http.Request) (*http.Response, error) {
				if provider == "github" && r.URL.String() != "https://api.github.com/user" {
					t.Fatal("public API origin misrouted", r.URL)
				}
				if provider == "azure-devops" && !strings.HasPrefix(r.URL.Path, "/acme/_apis/connectionData") {
					t.Fatal("organization lost", r.URL)
				}
				return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			})}
			if _, e = b.Authenticate(context.Background(), "auth login", c, func() error { return nil }); e == nil {
				t.Fatal("403 accepted")
			}
		})
	}
}
