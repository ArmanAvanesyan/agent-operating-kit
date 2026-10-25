package credentialbroker

import (
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type memoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
	fail   bool
}

func (s *memoryStore) Read(_ context.Context, k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.values[k]
	if !ok {
		return nil, ErrAbsent
	}
	return append([]byte{}, b...), nil
}
func (s *memoryStore) Write(_ context.Context, k string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[k] = append([]byte{}, b...)
	if s.fail {
		return errors.New("lost reply")
	}
	return nil
}
func TestCreationCASDeletionReloginAndUnknownPreservation(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(dir, 0700)
	c := a.BrokerConfig{Version: a.Version, Kind: "CredentialBrokerConfig", Namespace: "fixture", LedgerRoot: dir, Store: "synthetic"}
	s := &memoryStore{values: map[string][]byte{}}
	r := a.BrokerRequest{Version: a.Version, ConnectionID: "fixture", ScopeSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Operation: "status", Secrets: map[string]string{}}
	call := func() (a.BrokerResponse, error) { return Exchange(context.Background(), c, s, r) }
	status, e := call()
	if e != nil || status.Generation != "absent" {
		t.Fatal(e)
	}
	r.Operation = "get"
	if _, e = call(); !errors.Is(e, ErrAbsent) {
		t.Fatal(e)
	}
	r.Operation = "compare-and-swap"
	r.Secrets = map[string]string{"accessToken": "synthetic-token"}
	first, e := call()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = call(); e == nil {
		t.Fatal("creation overwrite")
	}
	r.Operation = "delete"
	r.Secrets = map[string]string{}
	r.ExpectedGeneration = first.Generation
	deleted, e := call()
	if e != nil || len(deleted.Secrets) != 0 {
		t.Fatal(e)
	}
	r.Operation = "status"
	status, e = call()
	if e != nil || status.Generation != deleted.Generation {
		t.Fatal("tombstone lost")
	}
	r.Operation = "compare-and-swap"
	r.ExpectedGeneration = status.Generation
	r.Secrets = map[string]string{"accessToken": "replacement"}
	if _, e = call(); e != nil {
		t.Fatal("relogin blocked", e)
	}
	s.fail = true
	r.Operation = "get"
	r.Secrets = map[string]string{}
	current, e := call()
	if e != nil {
		t.Fatal(e)
	}
	r.Operation = "compare-and-swap"
	r.ExpectedGeneration = current.Generation
	r.Secrets = map[string]string{"accessToken": "uncertain"}
	if _, e = call(); e == nil {
		t.Fatal("lost reply accepted")
	}
	r.Operation = "status"
	r.Secrets = map[string]string{}
	if _, e = call(); e == nil {
		t.Fatal("ambiguous operation unlocked")
	}
	entries, _ := os.ReadDir(dir)
	for _, f := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, f.Name()))
		if len(b) != 0 {
			t.Fatal("secret stored in lock ledger")
		}
	}
}
