package sdk

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/auth"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/toolchain"
	"os"
	"runtime"
	"strings"
)

// ExchangeCredentialBroker uses a separately operator-pinned secure-store
// executable over bounded private pipes. WorkKit ships no credential database.
// No secret IPC bytes or native error output are returned to report writers.
func ExchangeCredentialBroker(ctx context.Context, p a.ToolPin, r a.BrokerRequest, check func() error) (a.BrokerResponse, error) {
	var response a.BrokerResponse
	scope, e := hex.DecodeString(r.ScopeSHA256)
	if e != nil || len(scope) != 32 {
		return response, errors.New("exact broker scope digest required")
	}
	if p.ID != "workkit-secret-broker" || r.Version != a.Version || !auth.ID.MatchString(r.ConnectionID) || !auth.ID.MatchString(r.ExpectedGeneration) && r.ExpectedGeneration != "" {
		return response, errors.New("invalid secure-store request")
	}
	switch r.Operation {
	case "get", "status", "compare-and-swap", "delete":
	default:
		return response, errors.New("unsupported secure-store action")
	}
	if e := check(); e != nil {
		return response, e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return response, errors.New("secure-store request")
	}
	dir, e := os.MkdirTemp("", "workkit-secret-ipc-")
	if e != nil {
		return response, e
	}
	defer os.RemoveAll(dir)
	args := []string{"exchange"}
	if p.Configuration != nil {
		data, e := ReadLocal(ctx, p.Configuration.Path)
		if e != nil || SHA256(data) != p.Configuration.SHA256 {
			return response, errors.New("broker configuration pin mismatch")
		}
		args = append(args, "--config", p.Configuration.Path, "--config-sha256", p.Configuration.SHA256)
	}
	output, e := toolchain.Run(ctx, p, args, raw, []string{}, dir, 16384, check)
	if e != nil {
		return response, errors.New("secure-store result unresolved")
	}
	if e = DecodeRead(output, &response); e != nil {
		return a.BrokerResponse{}, errors.New("invalid secure-store response")
	}
	expected := map[string]string{"darwin": "os-keychain", "linux": "secret-service", "windows": "windows-credential-manager"}[runtime.GOOS]
	if response.Version != a.Version || response.ConnectionID != r.ConnectionID || response.ScopeSHA256 != r.ScopeSHA256 || !auth.ID.MatchString(response.Generation) || response.StorageMode != expected || len(response.Secrets) > 4 {
		return a.BrokerResponse{}, errors.New("secure-store binding mismatch")
	}
	for key, value := range response.Secrets {
		if key != "accessToken" && key != "refreshToken" && key != "expiresAt" && key != "scope" || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return a.BrokerResponse{}, errors.New("invalid secure-store payload")
		}
	}
	if r.Operation == "compare-and-swap" {
		if response.Generation == r.ExpectedGeneration {
			return a.BrokerResponse{}, errors.New("credential generation did not advance")
		}
		for key, value := range r.Secrets {
			if response.Secrets[key] != value {
				return a.BrokerResponse{}, errors.New("credential persistence mismatch")
			}
		}
	}
	if r.Operation == "delete" && (len(response.Secrets) != 0 || response.Generation == r.ExpectedGeneration) {
		return a.BrokerResponse{}, errors.New("credential deletion unresolved")
	}
	if r.Operation == "status" && len(response.Secrets) != 0 {
		return a.BrokerResponse{}, errors.New("status exposed secrets")
	}
	if r.Operation == "get" && response.Secrets["accessToken"] == "" {
		return a.BrokerResponse{}, errors.New("stored credential incomplete")
	}
	return response, nil
}

// AuthScopeSHA256 namespaces secret storage by the complete declared identity,
// profile and credential route, never merely a human-readable connection label.
func AuthScopeSHA256(c a.Configuration) string {
	c.Read = nil
	if c.Native != nil {
		native := *c.Native
		native.Signature = ""
		native.ScopeSHA256 = ""
		native.OperationID = ""
		native.ObservedAt = ""
		native.ExpiresAt = ""
		native.ProcessStart = ""
		native.ProcessID = 0
		native.ProcessSHA256 = ""
		native.ImplementationSHA256 = ""
		c.Native = &native
	}
	b, _ := json.Marshal(c)
	return SHA256(append([]byte("workkit-access/v1:auth-scope\n"), b...))
}
