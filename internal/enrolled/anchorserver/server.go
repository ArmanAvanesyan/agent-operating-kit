// Package anchorserver implements the separately administered append-only
// witness used by enrolled authority clients. Storage qualification remains
// an operator/deployment responsibility; this package never provisions trust.
package anchorserver

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Version = "aok-authority-anchor/v1"
const maxRequest = 16 << 10
const maxRecord = 64 << 10
const maxRecords = 10000

var blocked = errors.New("anchor request denied")
var streamPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,127}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Request struct {
	Version  string `json:"version"`
	StreamID string `json:"streamId"`
	Nonce    string `json:"nonce"`
	Action   string `json:"action"`
	Expected string `json:"expected"`
	Next     string `json:"next"`
}
type Response struct {
	Version       string `json:"version"`
	StreamID      string `json:"streamId"`
	Nonce         string `json:"nonce"`
	RequestSHA256 string `json:"requestSha256"`
	Tip           string `json:"tip"`
	ObservedAt    string `json:"observedAt"`
	ValidUntil    string `json:"validUntil"`
	Signature     string `json:"signature"`
}

// SignPayload uses the same domain and canonical JSON as authority.AnchorClient.
func SignPayload(r Response) []byte {
	r.Signature = ""
	b, _ := json.Marshal(r)
	return append([]byte("aok-authority-anchor:response:v1\n"), b...)
}

// Genesis is prepared by the separately controlled provisioning process. This
// server only validates and consumes it; there is no create/reset API.
type Genesis struct {
	Version  string `json:"version"`
	StreamID string `json:"streamId"`
	Sequence uint64 `json:"sequence"`
	Tip      string `json:"tip"`
}
type Record struct {
	Version       string `json:"version"`
	StreamID      string `json:"streamId"`
	Sequence      uint64 `json:"sequence"`
	Prior         string `json:"prior"`
	Tip           string `json:"tip"`
	RequestSHA256 string `json:"requestSha256"`
	ObservedAt    string `json:"observedAt"`
	Signature     string `json:"signature"`
}

type Config struct {
	Directory          string
	StreamID           string
	InitialTipSHA256   string
	ResponsePrivateKey ed25519.PrivateKey
	TokenRef           string
	ResolveToken       func(context.Context, string) (string, error)
	// TestOnlyAllowHTTP is solely for in-process tests. Production handlers require TLS.
	TestOnlyAllowHTTP bool
	// TestOnlyAfterReadForTests makes the cross-instance CAS race deterministic in tests.
	TestOnlyAfterReadForTests func()
	Clock                     func() time.Time
}
type Server struct {
	cfg     Config
	mu      sync.Mutex
	root    string
	genesis Genesis
}

func New(c Config) (*Server, error) {
	if !streamPattern.MatchString(c.StreamID) || !hashPattern.MatchString(c.InitialTipSHA256) || len(c.ResponsePrivateKey) != ed25519.PrivateKeySize || c.TokenRef == "" || c.ResolveToken == nil {
		return nil, blocked
	}
	root, err := filepath.EvalSymlinks(c.Directory)
	if err != nil || root != filepath.Clean(c.Directory) {
		return nil, blocked
	}
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
		return nil, blocked
	}
	gp := filepath.Join(root, recordName(c.StreamID, 0))
	gs, err := os.Lstat(gp)
	if err != nil || !gs.Mode().IsRegular() || gs.Mode()&os.ModeSymlink != 0 || gs.Size() > maxRecord {
		return nil, blocked
	}
	b, err := os.ReadFile(gp)
	if err != nil {
		return nil, blocked
	}
	var g Genesis
	if strictJSON(b, &g) != nil || g.Version != Version || g.StreamID != c.StreamID || g.Sequence != 0 || g.Tip != c.InitialTipSHA256 {
		return nil, blocked
	}
	canonical, _ := json.Marshal(g)
	if !bytes.Equal(b, canonical) {
		return nil, blocked
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	c.ResponsePrivateKey = append(ed25519.PrivateKey(nil), c.ResponsePrivateKey...)
	return &Server{cfg: c, root: root, genesis: g}, nil
}

func recordName(stream string, seq uint64) string { return fmt.Sprintf("%s.%020d.json", stream, seq) }
func hashBytes(b []byte) string                   { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/anchor", s.handle)
	return mux
}
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != "/v1/anchor" || r.URL.RawQuery != "" || r.URL.Fragment != "" || (!s.cfg.TestOnlyAllowHTTP && r.TLS == nil) {
		http.Error(w, "denied", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "denied", http.StatusBadRequest)
		return
	}
	var q Request
	if strictJSON(raw, &q) != nil || q.Version != Version || q.StreamID != s.cfg.StreamID || !streamPattern.MatchString(q.StreamID) || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(q.Nonce) || (q.Action != "current" && q.Action != "advance") || (q.Action == "current" && (q.Expected != "" || q.Next != "")) || (q.Action == "advance" && (!hashPattern.MatchString(q.Expected) || !hashPattern.MatchString(q.Next) || q.Expected == q.Next)) {
		http.Error(w, "denied", http.StatusBadRequest)
		return
	}
	if !s.auth(r) {
		http.Error(w, "denied", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	tip, lastSequence, err := s.currentLocked(ctx)
	if err == nil && q.Action == "advance" {
		if tip != q.Expected {
			err = blocked
		} else {
			if s.cfg.TestOnlyAfterReadForTests != nil {
				s.cfg.TestOnlyAfterReadForTests()
			}
			err = s.appendLocked(q, raw, tip, lastSequence+1)
			if err == nil {
				tip = q.Next
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		http.Error(w, "denied", http.StatusConflict)
		return
	}
	now := s.cfg.Clock().UTC()
	out := Response{Version: Version, StreamID: q.StreamID, Nonce: q.Nonce, RequestSHA256: hashBytes(raw), Tip: tip, ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(30 * time.Second).Format(time.RFC3339Nano)}
	out.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.cfg.ResponsePrivateKey, SignPayload(out)))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
func (s *Server) auth(r *http.Request) bool {
	if s.cfg.ResolveToken == nil {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == r.Header.Get("Authorization") || len(got) < 24 || len(got) > 4096 || strings.ContainsAny(got, "\r\n\x00") {
		return false
	}
	want, e := s.cfg.ResolveToken(r.Context(), s.cfg.TokenRef)
	if e != nil || len(want) < 24 || len(want) > 4096 {
		return false
	}
	a := sha256.Sum256([]byte(got))
	b := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func (s *Server) currentLocked(ctx context.Context) (string, uint64, error) {
	if ctx.Err() != nil {
		return "", 0, blocked
	}
	genesisPath := filepath.Join(s.root, recordName(s.cfg.StreamID, 0))
	gs, err := os.Lstat(genesisPath)
	if err != nil || !gs.Mode().IsRegular() || gs.Mode()&os.ModeSymlink != 0 || gs.Size() > maxRecord {
		return "", 0, blocked
	}
	gb, err := os.ReadFile(genesisPath)
	var currentGenesis Genesis
	if err != nil || strictJSON(gb, &currentGenesis) != nil || currentGenesis != s.genesis {
		return "", 0, blocked
	}
	canonicalGenesis, _ := json.Marshal(currentGenesis)
	if !bytes.Equal(gb, canonicalGenesis) {
		return "", 0, blocked
	}
	entries, err := os.ReadDir(s.root)
	if err != nil || len(entries) > maxRecords+1 {
		return "", 0, blocked
	}
	files := make(map[uint64]os.DirEntry, len(entries))
	prefix := s.cfg.StreamID + "."
	for _, entry := range entries {
		if ctx.Err() != nil {
			return "", 0, blocked
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			return "", 0, blocked
		}
		suffix := strings.TrimPrefix(name, prefix)
		if len(suffix) != 25 || !strings.HasSuffix(suffix, ".json") {
			return "", 0, blocked
		}
		seqText := strings.TrimSuffix(suffix, ".json")
		seq, e := strconv.ParseUint(seqText, 10, 64)
		if e != nil || recordName(s.cfg.StreamID, seq) != name || seq > maxRecords {
			return "", 0, blocked
		}
		if _, duplicate := files[seq]; duplicate {
			return "", 0, blocked
		}
		files[seq] = entry
	}
	if _, ok := files[0]; !ok {
		return "", 0, blocked
	}
	tip := s.genesis.Tip
	for seq := uint64(1); seq <= uint64(len(files)-1); seq++ {
		if ctx.Err() != nil {
			return "", 0, blocked
		}
		entry, ok := files[seq]
		if !ok {
			return "", 0, blocked
		}
		p := filepath.Join(s.root, recordName(s.cfg.StreamID, seq))
		st, e := os.Lstat(p)
		if e != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() > maxRecord || entry.IsDir() {
			return "", 0, blocked
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return "", 0, blocked
		}
		var rec Record
		if strictJSON(b, &rec) != nil || rec.Version != Version || rec.StreamID != s.cfg.StreamID || rec.Sequence != seq || rec.Prior != tip || !hashPattern.MatchString(rec.Tip) || !hashPattern.MatchString(rec.RequestSHA256) || rec.Signature == "" {
			return "", 0, blocked
		}
		canonical, _ := json.Marshal(rec)
		if !bytes.Equal(b, canonical) {
			return "", 0, blocked
		}
		// Prior links each signed append to the preceding externally selected tip.
		unsigned := rec
		unsigned.Signature = ""
		payload, _ := json.Marshal(unsigned)
		sig, se := base64.StdEncoding.DecodeString(rec.Signature)
		pub := s.cfg.ResponsePrivateKey.Public().(ed25519.PublicKey)
		if se != nil || !ed25519.Verify(pub, append([]byte("aok-authority-anchor:record:v1\n"), payload...), sig) {
			return "", 0, blocked
		}
		tip = rec.Tip
	}
	return tip, uint64(len(files) - 1), nil
}
func (s *Server) appendLocked(q Request, raw []byte, prior string, seq uint64) error {
	if seq == 0 || seq > maxRecords {
		return blocked
	}
	now := s.cfg.Clock().UTC()
	rec := Record{Version: Version, StreamID: q.StreamID, Sequence: seq, Prior: prior, RequestSHA256: hashBytes(raw), ObservedAt: now.Format(time.RFC3339Nano)}
	// The signed record links the caller-selected next tip to the prior tip.
	unsigned, _ := json.Marshal(rec)
	rec.Tip = q.Next
	unsigned, _ = json.Marshal(rec)
	sig := ed25519.Sign(s.cfg.ResponsePrivateKey, append([]byte("aok-authority-anchor:record:v1\n"), unsigned...))
	rec.Signature = base64.StdEncoding.EncodeToString(sig)
	b, err := json.Marshal(rec)
	if err != nil || len(b) > maxRecord {
		return blocked
	}
	path := filepath.Join(s.root, recordName(s.cfg.StreamID, seq))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return blocked
	}
	n, we := f.Write(b)
	if we == nil && n != len(b) {
		we = io.ErrShortWrite
	}
	if we == nil {
		we = f.Sync()
	}
	ce := f.Close()
	if we != nil || ce != nil {
		return blocked
	}
	d, err := os.Open(s.root)
	if err != nil {
		return blocked
	}
	err = d.Sync()
	ce = d.Close()
	if err != nil || ce != nil {
		return blocked
	}
	return nil
}

func strictJSON(b []byte, out any) error {
	if len(b) == 0 || len(b) > maxRequest {
		return blocked
	}
	if duplicate(b) != nil {
		return blocked
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return blocked
	}
	if d.Decode(new(any)) != io.EOF {
		return blocked
	}
	return nil
}
func duplicate(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		x, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if x == '{' {
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return blocked
				}
				seen[key] = true
				if e = walk(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		}
		if x == '[' {
			for d.More() {
				if e := walk(); e != nil {
					return e
				}
			}
			_, e := d.Token()
			return e
		}
		return blocked
	}
	if e := walk(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return blocked
	}
	return nil
}
