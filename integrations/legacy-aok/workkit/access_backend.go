package workkit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// AccessBackend supports only explicit credential routes. Native CLI stores are
// never consulted. RoundTripper and Challenge are trusted embedding/test ports.
type AccessBackend struct {
	LookupEnv    func(string) (string, bool)
	RoundTripper http.RoundTripper
	Challenge    func(string) error
}

func (b AccessBackend) Authenticate(ctx context.Context, action string, c a.Configuration, check func() error) (a.Binding, error) {
	result := a.Binding{Version: a.Version, Kind: "AuthBindingObservation", ConnectionID: c.ConnectionID, Principal: c.NativePrincipal, TenantID: c.TenantID, Mode: c.Mode, ProviderInteraction: "identity-observation", StorageMode: "external-reference", LiveQualification: "pending"}
	var snapshot *credentialReadSnapshot
	if c.Read != nil {
		cfg, raw, p, en, e := loadCredentialRead(ctx, c)
		if e != nil {
			return result, e
		}
		snapshot = &credentialReadSnapshot{cfg, raw, p, en}
	}
	result.Generation = "external-reference"
	result.Status = "authenticated"

	lookup := b.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	secret := func(key string) (string, error) {
		ref := c.SecretReferences[key]
		if !secretName.MatchString(ref) {
			return "", errors.New("secret reference unavailable")
		}
		v, ok := lookup(ref)
		if !ok || len(v) < 1 || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return "", errors.New("secret unavailable")
		}
		return v, nil
	}
	transport := b.RoundTripper
	if transport == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if c.TLS != nil {
			raw, e := sdk.ReadLocal(ctx, c.TLS.CAPath)
			if e != nil || sdk.SHA256(raw) != c.TLS.CASHA256 {
				return result, errors.New("TLS pin mismatch")
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(raw) {
				return result, errors.New("invalid external CA")
			}
			tr.TLSClientConfig.RootCAs = roots
		}
		defer tr.CloseIdleConnections()
		transport = tr
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("credential forwarding redirect rejected")
	}}
	if action == "auth logout" && c.Mode != "oauth2" {
		if c.Broker != nil {
			status, e := sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "status", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), Secrets: map[string]string{}}, check)
			if e != nil {
				return result, e
			}
			deleted, e := sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "delete", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), ExpectedGeneration: status.Generation, Secrets: map[string]string{}}, check)
			if e != nil {
				return result, e
			}
			result.Generation = deleted.Generation
			result.StorageMode = deleted.StorageMode
		}

		result.ProviderInteraction = "none"
		result.Status = "disconnected"
		result.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return result, nil
	} // Detaches only; external owner revokes/removes the token.
	token := ""
	username := ""
	var err error
	if c.Mode == "oauth2" {
		stored, e := b.oauth(ctx, action, c, client, secret, check)
		if e != nil {
			return result, e
		}
		result.Generation = stored.Generation
		result.StorageMode = stored.StorageMode
		if action == "auth logout" {
			result.Status = "disconnected"
			result.ProviderInteraction = "none"
			if c.Provider == "linear" {
				result.ProviderInteraction = "revocation"
			}
			result.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return result, nil
		}
		token = stored.Secrets["accessToken"]
		if c.Provider == "jira-cloud" {
			if e = jiraSite(ctx, client, c, token, check); e != nil {
				return result, e
			}
		}
	} else {
		if action == "auth refresh" && c.Broker == nil {
			return result, errors.New("explicit external credential owner must refresh this route")
		}
		if action == "connections test" && c.Broker != nil {
			stored, e := sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "get", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), Secrets: map[string]string{}}, check)
			if e != nil {
				return result, e
			}
			token = stored.Secrets["accessToken"]
			result.Generation = stored.Generation
			result.StorageMode = stored.StorageMode
		} else {
			token, err = secret("token")
		}
		if err != nil {
			return result, err
		}
		if c.Mode == "basic" || c.Mode == "acli-native" {
			username, err = secret("username")
			if err != nil {
				return result, err
			}
		}
	}
	if c.Mode == "gh-native" || c.Mode == "az-native" || c.Mode == "acli-native" {
		client.Transport = nativeTransport(ctx, c, token, check, client.Transport)
	}
	endpoint := c.Origin + c.BasePath
	method := "GET"
	var body []byte
	switch c.Provider {
	case "github":
		if c.Origin == "https://api.github.com" {
			endpoint = c.Origin + "/user"
		} else if c.Origin == "https://github.com" {
			endpoint = "https://api.github.com/user"
		} else {
			endpoint += "/api/v3/user"
		}
	case "azure-devops":
		if c.ProviderLookup == "" || strings.ContainsAny(c.ProviderLookup, "/\\?#%") {
			return result, errors.New("Azure organization lookup required")
		}
		endpoint = c.Origin + "/" + url.PathEscape(c.ProviderLookup) + "/_apis/connectionData?api-version=7.1"
	case "jira-cloud":
		if c.Mode == "oauth2" {
			endpoint = "https://api.atlassian.com/ex/jira/" + url.PathEscape(c.TenantID) + "/rest/api/3/myself"
		} else {
			endpoint += "/rest/api/3/myself"
		}
	case "jira-datacenter":
		endpoint += "/rest/api/2/myself"
	case "linear":
		endpoint += "/graphql"
		method = "POST"
		body = []byte(`{"query":"query WorkKitIdentity { viewer { id organization { id } } }"}`)
	default:
		return result, errors.New("unknown identity route")
	}
	if e := check(); e != nil {
		return result, e
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return result, errors.New("invalid identity request")
	}
	req.Header.Set("Accept", "application/json")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Mode == "basic" || c.Mode == "acli-native" {
		req.SetBasicAuth(username, token)
	} else if c.Mode == "pat" || c.Mode == "az-native" {
		req.SetBasicAuth("", token)
	} else if c.Provider == "linear" && c.Mode == "api-key" {
		req.Header.Set("Authorization", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := client.Do(req)
	if e != nil {
		return result, errors.New("identity request failed")
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if e != nil || len(raw) > 1<<20 || resp.StatusCode != 200 || bytes.Contains(raw, []byte(token)) {
		return result, errors.New("identity response rejected")
	}
	// Reject duplicate-key provider JSON while retaining exact integer IDs.
	var identity map[string]json.RawMessage
	if e = sdk.DecodeProviderIdentity(raw, &identity); e != nil {
		return result, errors.New("malformed identity response")
	}
	id := ""
	switch c.Provider {
	case "github":
		var n json.Number
		if json.Unmarshal(identity["id"], &n) == nil {
			id = n.String()
		}
	case "azure-devops":
		var user struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(identity["authorizedUser"], &user) == nil {
			id = user.ID
		}
	case "jira-cloud":
		_ = json.Unmarshal(identity["accountId"], &id)
	case "jira-datacenter":
		_ = json.Unmarshal(identity["key"], &id)
	case "linear":
		if e, ok := identity["errors"]; ok && string(e) != "[]" && string(e) != "null" {
			return result, errors.New("partial GraphQL identity")
		}
		var d struct {
			Viewer struct {
				ID           string `json:"id"`
				Organization struct {
					ID string `json:"id"`
				} `json:"organization"`
			} `json:"viewer"`
		}
		if json.Unmarshal(identity["data"], &d) != nil || d.Viewer.Organization.ID != c.TenantID {
			return result, errors.New("Linear workspace mismatch")
		}
		id = d.Viewer.ID
	}
	if id == "" || id != c.NativePrincipal {
		return result, errors.New("native principal mismatch")
	}
	if c.Read != nil && action == "connections test" {
		if e = b.captureWithCredentialSnapshot(ctx, c, token, check, snapshot, username); e != nil {
			return result, e
		}
	}
	if c.Broker != nil && c.Mode != "oauth2" && (action == "auth login" || action == "auth refresh") {
		status, e := sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "status", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), Secrets: map[string]string{}}, check)
		if e != nil {
			return result, e
		}
		stored, e := sdk.ExchangeCredentialBroker(ctx, *c.Broker, a.BrokerRequest{Version: a.Version, Operation: "compare-and-swap", ConnectionID: c.ConnectionID, ScopeSHA256: sdk.AuthScopeSHA256(c), ExpectedGeneration: status.Generation, Secrets: map[string]string{"accessToken": token}}, check)
		if e != nil {
			return result, e
		}
		result.Generation = stored.Generation
		result.StorageMode = stored.StorageMode
	}
	result.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}
func oauthPost(ctx context.Context, client *http.Client, endpoint string, form url.Values) (map[string]string, error) {
	payload := form.Encode()
	contentType := "application/x-www-form-urlencoded"
	if endpoint == "https://auth.atlassian.com/oauth/token" {
		values := map[string]string{}
		for k, v := range form {
			if len(v) != 1 {
				return nil, errors.New("ambiguous OAuth form")
			}
			values[k] = v[0]
		}
		raw, _ := json.Marshal(values)
		payload = string(raw)
		contentType = "application/json"
	}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(payload))
	if e != nil {
		return nil, errors.New("OAuth request")
	}
	req.Header.Set("Content-Type", contentType)
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("OAuth exchange unresolved")
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if e != nil || len(raw) > 16384 || resp.StatusCode != 200 {
		return nil, errors.New("OAuth exchange rejected")
	}
	if strings.HasSuffix(endpoint, "/revoke") {
		return map[string]string{}, nil
	}
	var values map[string]json.RawMessage
	if sdk.DecodeProviderIdentity(raw, &values) != nil {
		return nil, errors.New("OAuth response")
	}
	result := map[string]string{}
	for _, key := range []string{"access_token", "refresh_token"} {
		var v string
		if json.Unmarshal(values[key], &v) != nil || len(v) < 8 || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return nil, errors.New("OAuth token missing")
		}
		if key == "access_token" {
			result["accessToken"] = v
		} else {
			result["refreshToken"] = v
		}
	}
	var tokenType string
	_ = json.Unmarshal(values["token_type"], &tokenType)
	if !strings.EqualFold(tokenType, "Bearer") && !(endpoint == "https://auth.atlassian.com/oauth/token" && tokenType == "") {
		return nil, errors.New("OAuth token type")
	}
	var seconds json.Number
	if json.Unmarshal(values["expires_in"], &seconds) != nil {
		return nil, errors.New("OAuth expiry missing")
	}
	n, e := seconds.Int64()
	if e != nil || n < 1 || n > 7*24*3600 {
		return nil, errors.New("OAuth expiry invalid")
	}
	result["expiresAt"] = time.Now().UTC().Add(time.Duration(n) * time.Second).Format(time.RFC3339Nano)
	var scope string
	if json.Unmarshal(values["scope"], &scope) != nil || scope == "" {
		return nil, errors.New("OAuth scopes unavailable")
	}
	result["scope"] = scope
	return result, nil
}
func oauthChallenge(ctx context.Context, c *a.OAuth, show func(string) error) (string, string, error) {
	callback, e := url.Parse(c.RedirectURI)
	if e != nil || callback.Hostname() != "127.0.0.1" {
		return "", "", errors.New("loopback callback required")
	}
	listener, e := net.Listen("tcp4", callback.Host)
	if e != nil {
		return "", "", errors.New("callback unavailable")
	}
	defer listener.Close()
	nonce := make([]byte, 32)
	verifierBytes := make([]byte, 32)
	if _, e = rand.Read(nonce); e != nil {
		return "", "", e
	}
	if _, e = rand.Read(verifierBytes); e != nil {
		return "", "", e
	}
	state := hex.EncodeToString(nonce)
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	digest := sha256.Sum256([]byte(verifier))
	u, _ := url.Parse(c.AuthorizationURL)
	q := u.Query()
	q.Set("client_id", c.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("state", state)
	if c.AuthorizationURL == "https://linear.app/oauth/authorize" {
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
		q.Set("code_challenge_method", "S256")
	} else {
		q.Set("audience", "api.atlassian.com")
		q.Set("prompt", "consent")
	}
	u.RawQuery = q.Encode()
	codes := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || r.Method != "GET" || r.URL.Path != callback.Path || len(r.URL.RawQuery) > 8192 || values.Get("error") != "" || len(values["state"]) != 1 || len(values["code"]) != 1 || subtle.ConstantTimeCompare([]byte(values.Get("state")), []byte(state)) != 1 || len(values.Get("code")) < 1 || len(values.Get("code")) > 4096 {
			http.Error(w, "Rejected", 400)
			return
		}
		select {
		case codes <- values.Get("code"):
			w.Write([]byte("Authentication response received. You may close this window."))
		default:
			http.Error(w, "Already received", 409)
		}
	})
	defer server.Close()
	go server.Serve(listener)
	if e = show(u.String()); e != nil {
		return "", "", errors.New("challenge delivery failed")
	}
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case code := <-codes:
		return code, verifier, nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	case <-timer.C:
		return "", "", errors.New("challenge expired")
	}
}
