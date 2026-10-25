package sdk

import (
	"context"
	"database/sql"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/accessjournal"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"os"
	"path/filepath"
	"runtime"
)

func persistAccessState(ctx context.Context, root, out string, c a.Configuration, r a.Receipt) error {
	if r.Binding == nil {
		return nil
	}
	state := a.AccessState{Version: a.Version, Kind: "LocalAccessState", ConfigurationSHA256: AuthScopeSHA256(c), OperationID: r.OperationID, Status: r.Binding.Status, Generation: r.Binding.Generation, ReceiptPath: filepath.Join(out, "bundle.json"), ReceiptSHA256: SHA256(engine.Encode(r))}
	target := filepath.Join(root, "auth-state-"+AuthScopeSHA256(c)+".json")
	if _, e := os.Lstat(target); e == nil {
		if _, e = engine.SafePath(target); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	tmp := target + "." + r.OperationID
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(engine.Encode(state))
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, target); e != nil {
		return e
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return syncDir(root)
}
func checkAccessState(ctx context.Context, root string, c a.Configuration) error {
	p := filepath.Join(root, "auth-state-"+AuthScopeSHA256(c)+".json")
	var raw []byte
	var e error
	if runtime.GOOS == "windows" {
		raw, e = accessjournal.State(ctx, root, AuthScopeSHA256(c))
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
	} else {
		raw, e = ReadLocal(ctx, p)
	}
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var state a.AccessState
	if DecodeRead(raw, &state) != nil || state.Version != a.Version || state.Kind != "LocalAccessState" || state.ConfigurationSHA256 != AuthScopeSHA256(c) {
		return errors.New("access state invalid")
	}
	receipt, e := ReadLocal(ctx, state.ReceiptPath)
	if e != nil || SHA256(receipt) != state.ReceiptSHA256 {
		return errors.New("access state receipt incomplete")
	}
	if state.Status != "authenticated" {
		return errors.New("connection detached; explicit login required")
	}
	return nil
}
