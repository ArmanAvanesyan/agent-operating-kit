package toolisolation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const (
	Version         = "aok-tool-isolation/v1"
	maxRequestBytes = 4 << 20
	maxReadBytes    = 4 << 20
	maxCreateBytes  = 21
	maxReadPaths    = 32
	createFixture   = "AOK pilot fixture v1\n"
)

type Policy struct {
	RuntimeProfileSHA256 string   `json:"runtime_profile_sha256,omitempty"`
	Version              string   `json:"version"`
	Workspace            string   `json:"workspace"`
	Target               string   `json:"target"`
	WorkerExecutable     string   `json:"worker_executable"`
	WorkerSHA256         string   `json:"worker_sha256"`
	TimeoutSeconds       int      `json:"timeout_seconds"`
	WorkspaceDevice      uint64   `json:"workspace_device,omitempty"`
	WorkspaceInode       uint64   `json:"workspace_inode,omitempty"`
	ReadPaths            []string `json:"read_paths,omitempty"`
}

type Request struct {
	Operation string `json:"operation"`
	Path      string `json:"path,omitempty"`
	Data      []byte `json:"data,omitempty"`
}

type Envelope struct {
	Policy  Policy  `json:"policy"`
	Request Request `json:"request"`
}

type Result struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
	Data      []byte `json:"data,omitempty"`
}

func ValidatePolicy(p Policy) error {
	if p.Version != Version || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 30 || p.Target != "pilot-output.txt" {
		return errors.New("invalid policy")
	}
	if !filepath.IsAbs(p.Workspace) || filepath.Clean(p.Workspace) != p.Workspace || p.Workspace == string(filepath.Separator) {
		return errors.New("invalid policy")
	}
	if p.WorkerExecutable == "" || !filepath.IsAbs(p.WorkerExecutable) || filepath.Clean(p.WorkerExecutable) != p.WorkerExecutable {
		return errors.New("invalid policy")
	}
	if (p.WorkspaceDevice == 0) != (p.WorkspaceInode == 0) {
		return errors.New("invalid policy")
	}
	if len(p.ReadPaths) > maxReadPaths {
		return errors.New("invalid policy")
	}
	seenPaths := make(map[string]bool, len(p.ReadPaths))
	for _, name := range p.ReadPaths {
		if !validRelative(name) || seenPaths[name] {
			return errors.New("invalid policy")
		}
		seenPaths[name] = true
	}
	if rel, err := filepath.Rel(p.Workspace, p.WorkerExecutable); err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("invalid policy")
	}
	b, err := hex.DecodeString(p.WorkerSHA256)
	if err != nil || len(b) != sha256.Size || strings.ToLower(p.WorkerSHA256) != p.WorkerSHA256 {
		return errors.New("invalid policy")
	}
	return nil
}

func DecodeEnvelope(r io.Reader) (Policy, Request, error) {
	var e Envelope
	b, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil || len(b) > maxRequestBytes {
		return e.Policy, e.Request, errors.New("invalid request")
	}
	if _, err := contracts.DecodeJSON(b); err != nil {
		return e.Policy, e.Request, errors.New("invalid request")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e.Policy, e.Request, errors.New("invalid request")
	}
	if d.Decode(new(any)) != io.EOF {
		return e.Policy, e.Request, errors.New("invalid request")
	}
	return e.Policy, e.Request, nil
}

func ValidateRequest(q Request) error {
	switch q.Operation {
	case "read":
		if q.Path == "" || q.Data != nil || !validRelative(q.Path) {
			return errors.New("invalid request")
		}
	case "create-target":
		if q.Path != "" || (q.Data != nil && (!bytes.Equal(q.Data, []byte(createFixture)) || len(q.Data) > maxCreateBytes)) {
			return errors.New("invalid request")
		}
	default:
		return errors.New("invalid request")
	}
	return nil
}

func validRelative(s string) bool {
	return s != "" && !strings.Contains(s, "\\") && !path.IsAbs(s) && path.Clean(s) == s && s != "." && !strings.HasPrefix(s, "../")
}

// Execute is the filesystem primitive for trusted callers and tests. It does
// not provide an OS sandbox; production callers must use the launcher and the
// worker CLI's inherited-root-descriptor path.
func Execute(p Policy, q Request) (Result, error) {
	var out Result
	if ValidatePolicy(p) != nil || ValidateRequest(q) != nil || !readAllowed(p, q) {
		return out, errors.New("request denied")
	}
	return executePlatform(p, q)
}

// ValidateReadRequest verifies a request against the operator-owned path allowlist.
func ValidateReadRequest(p Policy, q Request) error {
	if ValidatePolicy(p) != nil || ValidateRequest(q) != nil || !readAllowed(p, q) {
		return errors.New("request denied")
	}
	return nil
}

// ExecuteWithRootFD executes using descriptor 3 (or the supplied inherited
// descriptor) as the policy workspace. The caller must be the trusted launcher.
func ExecuteWithRootFD(p Policy, q Request, fd int) (Result, error) {
	var out Result
	if ValidatePolicy(p) != nil || ValidateRequest(q) != nil || !readAllowed(p, q) || p.WorkspaceDevice == 0 || p.WorkspaceInode == 0 {
		return out, errors.New("request denied")
	}
	return executePlatformFD(p, q, fd)
}

func readAllowed(p Policy, q Request) bool {
	if q.Operation != "read" {
		return true
	}
	for _, allowed := range p.ReadPaths {
		if q.Path == allowed {
			return true
		}
	}
	return false
}
