package workkit

import (
	"bytes"
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (b AccessBackend) oauth(ctx context.Context, action string, c a.Configuration, client *http.Client, secret func(string) (string, error), check func() error) (a.BrokerResponse, error) {
	var stored a.BrokerResponse
	linear := c.Provider == "linear"
	jira := c.Provider == "jira-cloud"
	if !linear && !jira {
		return stored, errors.New("unsupported OAuth provider")
	}
	if linear && (c.OAuth.TokenURL != "https://api.linear.app/oauth/token" || c.OAuth.AuthorizationURL != "https://linear.app/oauth/authorize" || c.OAuth.RevokeURL != "https://api.linear.app/oauth/revoke") || jira && (c.OAuth.TokenURL != "https://auth.atlassian.com/oauth/token" || c.OAuth.AuthorizationURL != "https://auth.atlassian.com/authorize" || c.OAuth.RevokeURL != "") {
		return stored, errors.New("OAuth destination mismatch")
	}
	operation := "get"
	if action == "auth login" {
		operation = "status"
	}
	var e error
	stored, e = sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: operation, ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), Secrets: map[string]string{}}, check)
	if e != nil {
		return stored, e
	}
	if action == "connections test" {
		expires, e := time.Parse(time.RFC3339Nano, stored.Secrets["expiresAt"])
		if e != nil || !expires.After(time.Now()) {
			return stored, errors.New("stored OAuth expired; explicit refresh required")
		}
		return stored, nil
	}
	if action == "auth logout" {
		if linear {
			if e = check(); e != nil {
				return stored, e
			}
			if _, e = oauthPost(ctx, client, c.OAuth.RevokeURL, url.Values{"token": {stored.Secrets["accessToken"]}}); e != nil {
				return stored, e
			}
		}
		return sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "delete", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), ExpectedGeneration: stored.Generation, Secrets: map[string]string{}}, check)
	}
	form := url.Values{"client_id": {c.OAuth.ClientID}}
	// Resolve client secret before opening a challenge; it never enters argv.
	if c.SecretReferences["clientSecret"] != "" {
		v, e := secret("clientSecret")
		if e != nil {
			return stored, e
		}
		form.Set("client_secret", v)
	} else if jira {
		return stored, errors.New("confidential Atlassian app secret required")
	}
	switch action {
	case "auth login":
		if b.Challenge == nil {
			return stored, errors.New("challenge channel unavailable")
		}
		code, verifier, e := oauthChallenge(ctx, c.OAuth, b.Challenge)
		if e != nil {
			return stored, e
		}
		form.Set("grant_type", "authorization_code")
		form.Set("redirect_uri", c.OAuth.RedirectURI)
		form.Set("code", code)
		if linear {
			form.Set("code_verifier", verifier)
		}
	case "auth refresh":
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", stored.Secrets["refreshToken"])
	default:
		return stored, errors.New("unsupported OAuth operation")
	}
	if e = check(); e != nil {
		return stored, e
	}
	fresh, e := oauthPost(ctx, client, c.OAuth.TokenURL, form)
	if e != nil {
		return stored, e
	}
	for _, required := range c.OAuth.Scopes {
		if required == "offline_access" {
			continue
		}
		found := false
		for _, scope := range strings.Fields(fresh["scope"]) {
			if required == scope {
				found = true
			}
		}
		if !found {
			return stored, errors.New("OAuth scope missing")
		}
	}
	// Do not retry a rotating exchange or CAS after response loss.
	return sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "compare-and-swap", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), ExpectedGeneration: stored.Generation, Secrets: fresh}, check)
}
func jiraSite(ctx context.Context, client *http.Client, c a.Configuration, token string, check func() error) error {
	if e := check(); e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.atlassian.com/oauth/token/accessible-resources", nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	raw, e := readIdentityResponse(client, req, token)
	if e != nil {
		return e
	}
	var resources []struct {
		ID     string   `json:"id"`
		URL    string   `json:"url"`
		Scopes []string `json:"scopes"`
	}
	if e = sdk.DecodeProviderArray(raw, &resources); e != nil {
		return errors.New("malformed accessible resources")
	}
	matches := 0
	for _, r := range resources {
		if r.ID == c.TenantID && r.URL == c.Origin {
			valid := true
			for _, required := range c.OAuth.Scopes {
				if required == "offline_access" {
					continue
				}
				found := false
				for _, s := range r.Scopes {
					if required == s {
						found = true
					}
				}
				valid = valid && found
			}
			if valid {
				matches++
			}
		}
	}
	if matches != 1 {
		return errors.New("ambiguous or inaccessible Jira site")
	}
	return nil
}
func readIdentityResponse(client *http.Client, r *http.Request, token string) ([]byte, error) {
	resp, e := client.Do(r)
	if e != nil {
		return nil, errors.New("identity request failed")
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 || resp.StatusCode != 200 || bytes.Contains(raw, []byte(token)) {
		return nil, errors.New("identity response rejected")
	}
	return raw, nil
}
