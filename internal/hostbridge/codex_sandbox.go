package hostbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CodexExactWriteControlCheck = "codex-workspace-write-exact-permissions/v1"

type codexSandboxPlan struct {
	writeIntent bool
	threadMode  string
	turnPolicy  map[string]any
}

// codexSandbox translates the operator's filesystem intent into the narrowest
// native Codex mode. Codex workspaceWrite is directory scoped, so the signed
// external filesystem control remains responsible for enforcing narrower
// paths from Policy.Permissions.Filesystem.Write.
func codexSandbox(p Prepared, policy Policy, at time.Time) (codexSandboxPlan, error) {
	readOnly := codexSandboxPlan{threadMode: "read-only", turnPolicy: map[string]any{"type": "readOnly", "networkAccess": false}}
	if len(policy.Permissions.Filesystem.Write) == 0 {
		return readOnly, nil
	}
	if p.Workspace != policy.Workspace {
		return codexSandboxPlan{}, fmt.Errorf("Codex write workspace differs from operator policy")
	}
	if !policy.AllowExecution {
		return codexSandboxPlan{}, fmt.Errorf("Codex write is not authorized for execution")
	}
	if err := ValidatePolicy(policy); err != nil {
		return codexSandboxPlan{}, fmt.Errorf("invalid Codex write policy: %w", err)
	}
	if err := verifyWorkspace(policy); err != nil {
		return codexSandboxPlan{}, fmt.Errorf("Codex write workspace is not approved: %w", err)
	}
	if policy.Permissions.Network.Mode != "denied" {
		return codexSandboxPlan{}, fmt.Errorf("Codex write requires denied network intent")
	}
	// A fresh proof over the exact policy scope must exist before the runner can
	// start. runAttempt also verifies controls and fences the execution lifecycle.
	if err := verifyControls(policy, at.UTC()); err != nil {
		return codexSandboxPlan{}, fmt.Errorf("Codex write requires fresh signed controls: %w", err)
	}
	if err := requireCodexExactWriteControl(policy); err != nil {
		return codexSandboxPlan{}, err
	}
	root, err := canonicalExistingDirectory(policy.Workspace)
	if err != nil || root != policy.Workspace {
		return codexSandboxPlan{}, fmt.Errorf("invalid Codex write workspace")
	}
	for _, requested := range policy.Permissions.Filesystem.Write {
		if err := validateCodexWritePath(root, requested); err != nil {
			return codexSandboxPlan{}, err
		}
	}
	return codexSandboxPlan{
		writeIntent: true,
		threadMode:  "workspace-write",
		turnPolicy: map[string]any{
			"type": "workspaceWrite", "writableRoots": []string{root},
			"networkAccess": false, "excludeSlashTmp": true, "excludeTmpdirEnvVar": true,
		},
	}, nil
}

func requireCodexExactWriteControl(policy Policy) error {
	for _, control := range policy.Controls {
		if control.Name != "filesystem" {
			continue
		}
		data, err := assetBytes(policy.root, control.Evidence)
		if err != nil {
			break
		}
		var proof ControlProof
		if json.Unmarshal(data, &proof) == nil {
			for _, check := range proof.Checks {
				if check == CodexExactWriteControlCheck {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("Codex write requires filesystem proof of exact permission enforcement")
}

func canonicalExistingDirectory(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("workspace must be a canonical absolute path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace is not an existing directory")
	}
	return filepath.Clean(resolved), nil
}

func validateCodexWritePath(root, requested string) error {
	if requested == "" || !filepath.IsAbs(requested) || filepath.Clean(requested) != requested || strings.ContainsAny(requested, "*?[]{}") {
		return fmt.Errorf("Codex write permission must be a canonical absolute non-glob path")
	}
	rel, err := filepath.Rel(root, requested)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Codex write permission escapes the approved workspace")
	}
	// Reject symlinks in every existing component. The final target may be a
	// not-yet-created file, as in the pilot's exact-file write intent.
	for cur := requested; ; cur = filepath.Dir(cur) {
		info, statErr := os.Lstat(cur)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("Codex write permission traverses a symlink")
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("Codex write permission path cannot be inspected")
		}
		if cur == root || cur == filepath.Dir(cur) {
			break
		}
	}
	return nil
}
