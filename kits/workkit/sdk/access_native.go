package sdk

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/platformfs"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/toolchain"
	"os"
	"time"
)

type nativeAuthorityKey struct{}
type nativeAuthority struct {
	policy    a.Policy
	operation string
}

func NativeSigningBytes(e a.NativeEnrollment) []byte {
	e.Signature = ""
	b, _ := json.Marshal(e)
	return append([]byte("workkit-access/v1:native-control\n"), b...)
}
func ValidateNativeControl(ctx context.Context, c a.Configuration) error {
	if c.Native != nil && c.Mode == "acli-native" && (c.Native.CredentialMode != "jira-basic" || len(c.Native.StatusSHA256) != 64 || c.SecretReferences["username"] == "") {
		return errors.New("explicit acli status plus Jira basic route required")
	}
	authority, ok := ctx.Value(nativeAuthorityKey{}).(nativeAuthority)
	if !ok || c.Native == nil || c.Tool == nil {
		return errors.New("external native containment enrollment required")
	}
	n := c.Native
	start, e := platformfs.ProcessStart()
	if e != nil || start == "" || n.ProcessStart != start {
		return errors.New("native process incarnation mismatch")
	}
	if c.Mode == "gh-native" && n.CredentialMode != "token" || c.Mode == "az-native" && n.CredentialMode != "pat" {
		return errors.New("native credential mode mismatch")
	}
	if n.ProcessID != os.Getpid() || n.OperationID != authority.operation || n.ToolSHA256 != c.Tool.SHA256 || n.ScopeSHA256 != AuthScopeSHA256(c) || n.ImplementationSHA256 != AccessImplementationSHA256() {
		return errors.New("native enrollment binding mismatch")
	}
	observed, e := time.Parse(time.RFC3339Nano, n.ObservedAt)
	if e != nil {
		return e
	}
	expiry, e := time.Parse(time.RFC3339Nano, n.ExpiresAt)
	now := time.Now().UTC()
	if e != nil || observed.After(now) || !expiry.After(now) || expiry.Sub(observed) > 5*time.Minute {
		return errors.New("native controls stale")
	}
	for _, control := range []string{"filesystem", "network", "environment", "credential-store", "no-implicit-refresh", "stop-monitor"} {
		if n.Controls[control] != "independently-observed" {
			return errors.New("native controls unverified")
		}
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	sha, e := toolchain.HashFile(ctx, executable, toolchain.MaxBinaryBytes)
	if e != nil || sha != n.ProcessSHA256 {
		return errors.New("native parent identity mismatch")
	}
	valid := false
	for _, issuer := range authority.policy.Issuers {
		key, e := base64.StdEncoding.DecodeString(issuer.PublicKey)
		sig, se := base64.StdEncoding.DecodeString(n.Signature)
		if issuer.ID == n.Issuer && e == nil && se == nil && len(key) == 32 && ed25519.Verify(key, NativeSigningBytes(*n), sig) {
			valid = true
		}
	}
	if !valid {
		return errors.New("native control signature invalid")
	}
	if _, e = engine.SafePath(n.ProfileRoot); e != nil {
		return e
	}
	st, e := os.Stat(n.ProfileRoot)
	if e != nil || !st.IsDir() || platformfs.PrivateDirectory(n.ProfileRoot) != nil {
		return errors.New("private native profile required")
	}
	return nil
}
func NativeToolRun(ctx context.Context, p a.ToolPin, args []string, input []byte, env []string, dir string, check func() error) ([]byte, error) {
	return toolchain.Run(ctx, p, args, input, env, dir, 1<<20, check)
}
func VerifyNativeProfile(ctx context.Context, c a.Configuration) error {
	if e := ValidateNativeControl(ctx, c); e != nil {
		return e
	}
	hash, e := toolchain.HashTree(ctx, c.Native.ProfileRoot)
	if e != nil || hash != c.Native.ProfileSHA256 {
		return errors.New("native profile drift")
	}
	return toolchain.Verify(ctx, *c.Tool)
}
func DiscoverTools(ctx context.Context, raw []byte) ([]a.DiscoveryFinding, error) {
	var request a.DiscoveryRequest
	if e := DecodeRead(raw, &request); e != nil {
		return nil, e
	}
	return toolchain.Discover(ctx, request)
}
func HashLocalRuntime(ctx context.Context, path string) (string, error) {
	return toolchain.HashTree(ctx, path)
}

func CurrentProcessStart() (string, error) { return platformfs.ProcessStart() }
