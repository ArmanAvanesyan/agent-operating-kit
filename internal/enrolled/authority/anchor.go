package authority

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Anchor is a separately retained, linearizable compare-and-append service.
// It must NOT restore with the authority SQLite database. A failed append or
// lost response blocks recovery; it never authorizes replay or automatic reset.
// The service implements no quota or execution logic: it witnesses opaque tips.
type Anchor interface {
	Current(context.Context, string) (string, error)
	Advance(context.Context, string, string, string) error
}

// AnchorClient implements aok-authority-anchor/v1 over pinned authenticated TLS.
// The external service owns pre-enrolled stream identity and monotonic storage.
// There is intentionally no create/reset/truncate method in this client.
type AnchorClient struct {
	Endpoint, StreamID string
	PublicKey          ed25519.PublicKey
	Token              func(context.Context) (string, error)
	Transport          http.RoundTripper
}
type AnchorRequest struct {
	Version  string `json:"version"`
	StreamID string `json:"streamId"`
	Nonce    string `json:"nonce"`
	Action   string `json:"action"`
	Expected string `json:"expected"`
	Next     string `json:"next"`
}
type AnchorResponse struct {
	Version       string `json:"version"`
	StreamID      string `json:"streamId"`
	Nonce         string `json:"nonce"`
	RequestSHA256 string `json:"requestSha256"`
	Tip           string `json:"tip"`
	ObservedAt    string `json:"observedAt"`
	ValidUntil    string `json:"validUntil"`
	Signature     string `json:"signature"`
}

const AnchorVersion = "aok-authority-anchor/v1"

func anchorPayload(r AnchorResponse) []byte {
	r.Signature = ""
	return append([]byte("aok-authority-anchor:response:v1\n"), unsigned(r)...)
}
func (a AnchorClient) call(ctx context.Context, stream, action, expected, next string) (string, error) {
	u, e := url.Parse(a.Endpoint)
	if ctx == nil || e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "/v1/anchor" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || stream != a.StreamID || !validID(stream) || a.Token == nil || len(a.PublicKey) != ed25519.PublicKeySize {
		return "", ErrBlocked
	}
	n := make([]byte, 16)
	if _, e = rand.Read(n); e != nil {
		return "", e
	}
	q := AnchorRequest{Version: AnchorVersion, StreamID: stream, Nonce: hex.EncodeToString(n), Action: action, Expected: expected, Next: next}
	b, _ := json.Marshal(q)
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	token, e := a.Token(bounded)
	if e != nil || len(token) < 24 || len(token) > 4096 || strings.ContainsAny(token, "\r\n\x00") {
		return "", ErrBlocked
	}
	req, e := http.NewRequestWithContext(bounded, "POST", a.Endpoint, strings.NewReader(string(b)))
	if e != nil {
		return "", ErrBlocked
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Transport: a.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return "", ErrBlocked
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 65537))
	var out AnchorResponse
	if e != nil || res.StatusCode != 200 || len(raw) > 65536 || strings.Contains(string(raw), token) || usage.Decode(raw, &out) != nil {
		return "", ErrBlocked
	}
	obs, oe := time.Parse(time.RFC3339Nano, out.ObservedAt)
	until, ue := time.Parse(time.RFC3339Nano, out.ValidUntil)
	sig, se := base64.StdEncoding.DecodeString(out.Signature)
	now := time.Now().UTC()
	if out.Version != AnchorVersion || out.StreamID != stream || out.Nonce != q.Nonce || out.RequestSHA256 != byteHash(b) || !validHash(out.Tip) || oe != nil || ue != nil || obs.After(now.Add(time.Second)) || now.Sub(obs) > 5*time.Second || !now.Before(until) || until.Sub(obs) > time.Minute || se != nil || !ed25519.Verify(a.PublicKey, anchorPayload(out), sig) {
		return "", ErrBlocked
	}
	return out.Tip, nil
}
func (a AnchorClient) Current(ctx context.Context, s string) (string, error) {
	return a.call(ctx, s, "current", "", "")
}
func (a AnchorClient) Advance(ctx context.Context, s, expected, next string) error {
	if !validHash(expected) || !validHash(next) || expected == next {
		return ErrBlocked
	}
	tip, e := a.call(ctx, s, "advance", expected, next)
	if e != nil {
		return e
	}
	if tip != next {
		return ErrBlocked
	}
	return nil
}
