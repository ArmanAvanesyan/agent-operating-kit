package anchorserver

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const fixtureToken = "fixture-anchor-token-long-enough-0123456789"

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func fixture(t *testing.T) (*Server, string, ed25519.PublicKey, string) {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	tip := digest("genesis")
	b, _ := json.Marshal(Genesis{Version: Version, StreamID: "test-stream", Sequence: 0, Tip: tip})
	if e = os.WriteFile(filepath.Join(dir, recordName("test-stream", 0)), b, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := New(Config{Directory: dir, StreamID: "test-stream", InitialTipSHA256: tip, ResponsePrivateKey: key, TokenRef: "anchor/token", ResolveToken: func(_ context.Context, ref string) (string, error) {
		if ref != "anchor/token" {
			return "", blocked
		}
		return fixtureToken, nil
	}, TestOnlyAllowHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	return s, tip, pub, dir
}
func send(s *Server, action, expected, next string) (int, Response) {
	q := Request{Version: Version, StreamID: "test-stream", Nonce: "00112233445566778899aabbccddeeff", Action: action, Expected: expected, Next: next}
	raw, _ := json.Marshal(q)
	r := httptest.NewRequest("POST", "/v1/anchor", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+fixtureToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out Response
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestPreenrolledGenesisAndSignedAdvanceReopen(t *testing.T) {
	s, gen, pub, dir := fixture(t)
	code, out := send(s, "current", "", "")
	if code != 200 || out.Tip != gen {
		t.Fatalf("current %d %#v", code, out)
	}
	raw, _ := json.Marshal(out)
	var signed Response
	if json.Unmarshal(raw, &signed) != nil || signed.Signature == "" {
		t.Fatal("missing signature")
	}
	sig := mustB64(t, signed.Signature)
	if !ed25519.Verify(pub, SignPayload(signed), sig) {
		t.Fatal("invalid response signature")
	}
	code, advanced := send(s, "advance", gen, digest("next"))
	if code != 200 || advanced.Tip != digest("next") {
		t.Fatalf("advance %d %#v", code, advanced)
	}
	s2, e := New(s.cfg)
	if e != nil {
		t.Fatal(e)
	}
	code, again := send(s2, "current", "", "")
	if code != 200 || again.Tip != advanced.Tip {
		t.Fatalf("reopen did not preserve tip: status=%d response=%#v want=%s", code, again, advanced.Tip)
	}
	if _, e = os.Stat(filepath.Join(dir, recordName("test-stream", 1))); e != nil {
		t.Fatal(e)
	}
}
func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestConcurrentCompareAppendOneWinner(t *testing.T) {
	s, gen, _, _ := fixture(t)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, v := range []string{"a", "b"} {
		wg.Add(1)
		go func(v string) { defer wg.Done(); code, _ := send(s, "advance", gen, digest(v)); codes <- code }(v)
	}
	wg.Wait()
	close(codes)
	n := 0
	for c := range codes {
		if c == 200 {
			n++
		} else if c != 409 {
			t.Fatalf("unexpected status %d", c)
		}
	}
	if n != 1 {
		t.Fatalf("winner count %d", n)
	}
}

func TestSequentialAdvancesAndReopen(t *testing.T) {
	s, tip, _, _ := fixture(t)
	for i := 0; i < 7; i++ {
		next := digest(fmt.Sprintf("next-%d", i))
		code, out := send(s, "advance", tip, next)
		if code != 200 || out.Tip != next {
			t.Fatalf("advance %d status=%d tip=%s", i, code, out.Tip)
		}
		tip = next
	}
	reopened, err := New(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	code, out := send(reopened, "current", "", "")
	if code != 200 || out.Tip != tip {
		t.Fatalf("reopened tip status=%d got=%s want=%s", code, out.Tip, tip)
	}
}

func TestParallelCASAcrossIndependentServerInstances(t *testing.T) {
	s1, tip, _, _ := fixture(t)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	c := s1.cfg
	c.TestOnlyAfterReadForTests = func() { ready <- struct{}{}; <-release }
	s1, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i, srv := range []*Server{s1, s2} {
		wg.Add(1)
		go func(i int, srv *Server) {
			defer wg.Done()
			code, _ := send(srv, "advance", tip, digest(fmt.Sprintf("cross-process-%d", i)))
			codes <- code
		}(i, srv)
	}
	<-ready
	<-ready
	close(release)
	wg.Wait()
	close(codes)
	winners := 0
	for code := range codes {
		if code == 200 {
			winners++
		} else if code != 409 {
			t.Fatalf("unexpected CAS status %d", code)
		}
	}
	if winners != 1 {
		t.Fatalf("independent server winner count %d", winners)
	}
	reopened, err := New(s1.cfg)
	if err != nil {
		t.Fatal(err)
	}
	code, current := send(reopened, "current", "", "")
	if code != 200 {
		t.Fatal("chain failed to reopen after CAS")
	}
	if current.Tip != digest("cross-process-0") && current.Tip != digest("cross-process-1") {
		t.Fatalf("unexpected winning tip %s", current.Tip)
	}
}

func TestCorruptOrPartialAppendFailsClosedNoHeal(t *testing.T) {
	s, gen, _, dir := fixture(t)
	p := filepath.Join(dir, recordName("test-stream", 1))
	if e := os.WriteFile(p, []byte(`{"version":`), 0600); e != nil {
		t.Fatal(e)
	}
	code, _ := send(s, "advance", gen, digest("next"))
	if code != 409 {
		t.Fatalf("corrupt record accepted %d", code)
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{"version":` {
		t.Fatal("server modified corrupt append")
	}
}
func TestAuthenticationTLSAndSymlinkFailClosed(t *testing.T) {
	s, _, _, dir := fixture(t)
	q, _ := json.Marshal(Request{Version: Version, StreamID: "test-stream", Nonce: "00112233445566778899aabbccddeeff", Action: "current"})
	r := httptest.NewRequest("POST", "/v1/anchor", bytes.NewReader(q))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated status %d", w.Code)
	}
	if e := os.Symlink(filepath.Join(dir, recordName("test-stream", 0)), filepath.Join(dir, recordName("test-stream", 1))); e != nil {
		t.Fatal(e)
	}
	code, _ := send(s, "current", "", "")
	if code != 409 {
		t.Fatalf("symlink accepted %d", code)
	}
}
func TestNoGenesisProvisionAndTamperRejected(t *testing.T) {
	s, _, _, dir := fixture(t)
	_ = s
	st, e := os.Stat(filepath.Join(dir, recordName("test-stream", 0)))
	if e != nil || !st.Mode().IsRegular() {
		t.Fatal("fixture genesis")
	}
	b, _ := os.ReadFile(filepath.Join(dir, recordName("test-stream", 0)))
	b = append(b, ' ')
	_ = os.WriteFile(filepath.Join(dir, recordName("test-stream", 0)), b, 0600)
	if _, e = New(s.cfg); e == nil {
		t.Fatal("tampered genesis accepted")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	c := s.cfg
	c.Directory = missing
	if _, e = New(c); e == nil {
		t.Fatal("missing store provisioned implicitly")
	}
}
func TestStrictRequestRejectsDuplicatesAndUnknown(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, b := range []string{`{"version":"aok-authority-anchor/v1","version":"aok-authority-anchor/v1"}`, `{"version":"aok-authority-anchor/v1","streamId":"test-stream","nonce":"00112233445566778899aabbccddeeff","action":"current","extra":1}`} {
		r := httptest.NewRequest("POST", "/v1/anchor", bytes.NewBufferString(b))
		r.Header.Set("Authorization", "Bearer "+fixtureToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("malformed status %d", w.Code)
		}
	}
}

var _ = time.Second
