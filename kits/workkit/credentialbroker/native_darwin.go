//go:build darwin

package credentialbroker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/toolchain"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
)

type keychainStore struct{ config a.BrokerConfig }
type helperRequest struct {
	Operation string `json:"operation"`
	Service   string `json:"service"`
	Key       string `json:"key"`
	Data      string `json:"data"`
}
type helperResponse struct {
	Found bool   `json:"found"`
	Data  string `json:"data"`
}

func openNative(c a.BrokerConfig) (Store, error) {
	if c.Helper == nil || c.Helper.ID != "workkit-keychain-helper" {
		return nil, errors.New("pinned native Keychain helper required")
	}
	return keychainStore{c}, nil
}
func (s keychainStore) call(ctx context.Context, op, key string, b []byte) (helperResponse, error) {
	var result helperResponse
	r := helperRequest{op, "org.workkit." + s.config.Namespace, key, base64.StdEncoding.EncodeToString(b)}
	raw, _ := json.Marshal(r)
	output, e := toolchain.RunNested(ctx, *s.config.Helper, []string{"exchange"}, raw, []string{}, "/", 32768)
	if e != nil || sdk.DecodeRead(output, &result) != nil {
		return result, errors.New("native Keychain result unresolved")
	}
	return result, nil
}
func (s keychainStore) Read(ctx context.Context, key string) ([]byte, error) {
	r, e := s.call(ctx, "get", key, nil)
	if e != nil {
		return nil, e
	}
	if !r.Found {
		return nil, ErrAbsent
	}
	b, e := base64.StdEncoding.DecodeString(r.Data)
	if e != nil || len(b) > 16384 {
		return nil, errors.New("native Keychain value bound")
	}
	return b, nil
}
func (s keychainStore) Write(ctx context.Context, key string, b []byte) error {
	_, e := s.call(ctx, "set", key, b)
	return e
}
