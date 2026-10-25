// Package credentialbroker stores private token bundles in native OS stores.
// The ledger contains locks only, never credential payloads. No file fallback.
package credentialbroker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/accessjournal"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/platformfs"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var ErrAbsent = errors.New("credential absent")
var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type Store interface {
	Read(context.Context, string) ([]byte, error)
	Write(context.Context, string, []byte) error
}
type record struct {
	Version    string            `json:"version"`
	Scope      string            `json:"scope"`
	Generation string            `json:"generation"`
	Deleted    bool              `json:"deleted"`
	Secrets    map[string]string `json:"secrets"`
}

func Validate(c a.BrokerConfig) error {
	if c.Version != a.Version || c.Kind != "CredentialBrokerConfig" || !safeID.MatchString(c.Namespace) || !filepath.IsAbs(c.LedgerRoot) {
		return errors.New("invalid broker configuration")
	}
	p, e := engine.SafePath(c.LedgerRoot)
	if e != nil {
		return errors.New("protected broker ledger required")
	}
	st, e := os.Stat(p)
	if e != nil || !st.IsDir() || platformfs.PrivateDirectory(p) != nil {
		return errors.New("private broker ledger required")
	}
	return nil
}
func Exchange(ctx context.Context, c a.BrokerConfig, s Store, r a.BrokerRequest) (response a.BrokerResponse, resultErr error) {
	response = a.BrokerResponse{Version: a.Version, ConnectionID: r.ConnectionID, ScopeSHA256: r.ScopeSHA256, StorageMode: c.Store, Secrets: map[string]string{}}
	scope, e := hex.DecodeString(r.ScopeSHA256)
	if e != nil || len(scope) != 32 || strings.ToLower(r.ScopeSHA256) != r.ScopeSHA256 || r.Version != a.Version || !safeID.MatchString(r.ConnectionID) || r.ExpectedGeneration != "" && !safeID.MatchString(r.ExpectedGeneration) {
		return response, errors.New("invalid broker request")
	}

	if e = Validate(c); e != nil {
		return response, e
	}
	if s == nil {
		return response, errors.New("native credential store unavailable")
	}
	if r.Operation != "get" && r.Operation != "status" && r.Operation != "compare-and-swap" && r.Operation != "delete" {
		return response, errors.New("invalid broker operation")
	}
	if len(r.Secrets) > 4 {
		return response, errors.New("credential bound")
	}
	for k, v := range r.Secrets {
		if k != "accessToken" && k != "refreshToken" && k != "expiresAt" && k != "scope" || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return response, errors.New("invalid private payload")
		}
	}
	if r.Operation != "compare-and-swap" && len(r.Secrets) != 0 {
		return response, errors.New("unexpected secrets")
	}
	key := sdk.SHA256([]byte(c.Namespace + "\x00" + r.ScopeSHA256))
	lock := filepath.Join(c.LedgerRoot, key+".lock")
	if runtime.GOOS == "windows" {
		if e = accessjournal.Hold(ctx, c.LedgerRoot, key); e != nil {
			return response, e
		}
	}
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return response, errors.New("broker scope unresolved or busy")
	}
	e = f.Sync()
	f.Close()
	if e != nil {
		return response, errors.New("broker lock durability uncertain")
	}
	if runtime.GOOS != "windows" {
		if e = syncDirectory(c.LedgerRoot); e != nil {
			return response, errors.New("broker directory durability uncertain")
		}
	}
	// A crash or ambiguous write leaves the lock. Never infer stale from a PID.
	done := false
	defer func() {
		if done {
			if runtime.GOOS == "windows" {
				if e := accessjournal.Complete(context.Background(), c.LedgerRoot, "", key, "", nil, nil); e != nil {
					resultErr = errors.New("broker completion durability uncertain")
					return
				}
			}
			os.Remove(lock)
		}
	}()
	old := record{Version: a.Version, Scope: r.ScopeSHA256, Generation: "absent", Deleted: true, Secrets: map[string]string{}}
	data, e := s.Read(ctx, key)
	if e != nil && !errors.Is(e, ErrAbsent) {
		return response, errors.New("native read failed")
	}
	if e == nil {
		if sdk.DecodeRead(data, &old) != nil || old.Version != a.Version || old.Scope != r.ScopeSHA256 || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(old.Generation) {
			return response, errors.New("native record integrity")
		}
	}
	if (old.Deleted && len(old.Secrets) != 0) || (!old.Deleted && old.Secrets["accessToken"] == "") {
		return response, errors.New("native record invariant")
	}
	for k, v := range old.Secrets {
		if (k != "accessToken" && k != "refreshToken" && k != "expiresAt" && k != "scope") || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return response, errors.New("native record invalid")
		}
	}
	response.Generation = old.Generation
	switch r.Operation {
	case "status":
		done = true
		return response, nil
	case "get":
		if old.Deleted {
			done = true
			return response, ErrAbsent
		}
		response.Secrets = old.Secrets
		done = true
		return response, nil
	}
	expected := r.ExpectedGeneration
	if expected == "" {
		expected = "absent"
	}
	if expected != old.Generation {
		done = true
		return response, errors.New("credential generation conflict")
	}
	next := record{Version: a.Version, Scope: r.ScopeSHA256, Deleted: r.Operation == "delete", Secrets: map[string]string{}}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return response, e
	}
	next.Generation = hex.EncodeToString(nonce)
	if !next.Deleted {
		if r.Secrets["accessToken"] == "" {
			return response, errors.New("access credential required")
		}
		next.Secrets = r.Secrets
	}
	raw, e := json.Marshal(next)
	if e != nil || len(raw) > 16384 {
		return response, errors.New("credential record bound")
	}
	if e = s.Write(ctx, key, raw); e != nil {
		return response, errors.New("native persistence uncertain")
	}
	got, e := s.Read(ctx, key)
	if e != nil || string(got) != string(raw) {
		return response, errors.New("native readback uncertain")
	}
	response.Generation = next.Generation
	response.Secrets = next.Secrets
	done = true
	return response, nil
}
func Open(c a.BrokerConfig) (Store, error) {
	if e := Validate(c); e != nil {
		return nil, e
	}
	want := map[string]string{"darwin": "os-keychain", "linux": "secret-service", "windows": "windows-credential-manager"}[runtime.GOOS]
	if c.Store != want {
		return nil, errors.New("native store/platform mismatch")
	}
	return openNative(c)
}

func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
