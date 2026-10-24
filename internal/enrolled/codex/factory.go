// Package codex implements the enrolled, read-only Codex App Server boundary
// used by usage collectors. It intentionally has no model invocation methods.
package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

const (
	PinnedVersion = hostbridge.CodexPinnedVersion
	maxFrameBytes = 1 << 20
	maxTotalBytes = 4 << 20
	maxFrames     = 256
	openTimeout   = 5 * time.Second
)

// Config is protected operator configuration. AccountRef is a pseudonymous
// enrollment reference; it is never interpreted as a credential or path.
type Config struct {
	Executable       string
	ExecutableSHA256 string
	Version          string
	AccountRef       string
	SourceID         string
	Home             string
	Path             string
	// These opaque measurements are supplied by a protected native-account
	// observer. A caller-provided account label alone is not identity proof.
	NativeAccountProofRef    string
	NativeAccountProofSHA256 string
	HomeSHA256               string
	EnvironmentCensusSHA256  string
	UsageBindingID           string
	UsageStreamID            string
}

// Scope is the exact enrolled collector route presented to the external
// authority. Ordinal and CaptureProofRef originate with CaptureAuthority.
type Scope struct {
	ExecutableSHA256         string
	Version                  string
	AccountRef               string
	SourceID                 string
	Ordinal                  uint64
	CaptureProofRef          string
	LeaseID                  string
	Revision                 int64
	ValidUntil               time.Time
	ProofSHA256              string
	NativeAccountProofRef    string
	NativeAccountProofSHA256 string
	HomeSHA256               string
	EnvironmentCensusSHA256  string
}

// Authority is an injected protected admission boundary. Implementations must
// resolve current enrollment, revocation, account binding and launch policy.
type Authority interface {
	Check(context.Context, Scope) error
}

// CaptureGrant comes from an external serialized capture authority. A local
// counter is deliberately not used because it cannot establish global order.
type CaptureGrant struct {
	Ordinal     uint64
	LeaseID     string
	Revision    int64
	ValidUntil  time.Time
	AccountRef  string
	SourceID    string
	ProofRef    string
	ProofSHA256 string
}

type CaptureAuthority interface {
	Acquire(context.Context, string, string) (CaptureGrant, error)
	Complete(context.Context, CaptureGrant, CaptureResult) error
}

type CaptureResult struct {
	Status string
	Bytes  int
	Frames int
	SHA256 string
}

// New validates fixed operator configuration and returns a collector factory.
func New(cfg Config, authority Authority, capture CaptureAuthority) (*Factory, error) {
	if authority == nil || capture == nil || !processGroupSupported() || cfg.Version != PinnedVersion || cfg.AccountRef == "" || cfg.SourceID == "" || cfg.Path == "" || !filepath.IsAbs(cfg.Executable) || !filepath.IsAbs(cfg.Home) || len(cfg.ExecutableSHA256) != 64 || cfg.NativeAccountProofRef == "" || !validDigest(cfg.NativeAccountProofSHA256) || !validDigest(cfg.HomeSHA256) || !validDigest(cfg.EnvironmentCensusSHA256) {
		return nil, errors.New("invalid enrolled Codex collector configuration")
	}
	if _, err := hex.DecodeString(cfg.ExecutableSHA256); err != nil {
		return nil, errors.New("invalid enrolled Codex executable digest")
	}
	if filepath.Clean(cfg.Executable) != cfg.Executable || filepath.Clean(cfg.Home) != cfg.Home || strings.ContainsAny(cfg.Path, "\r\n\x00") {
		return nil, errors.New("non-canonical Codex launch configuration")
	}
	return &Factory{cfg: cfg, authority: authority, capture: capture}, nil
}

type Factory struct {
	cfg         Config
	authority   Authority
	capture     CaptureAuthority
	mu          sync.Mutex
	active      bool
	collecting  bool
	collectOpen bool
	last        CaptureObservation
	completeErr error
}

type CaptureObservation struct {
	Ordinal     uint64
	Revision    int64
	LeaseID     string
	ProofRef    string
	AccountRef  string
	SourceID    string
	ProofSHA256 string
	Result      CaptureResult
}

// UsageCapture is a normalized usage export plus its detached capture
// metadata asset. The raw App Server response is never returned or persisted.
type UsageCapture struct {
	Export usage.Export
	Report usage.Report
	Assets map[string][]byte
}

type usageCaptureMetadata struct {
	Version       string `json:"version"`
	AccountRef    string `json:"accountRef"`
	SourceID      string `json:"sourceId"`
	BindingID     string `json:"bindingId"`
	StreamID      string `json:"streamId"`
	Ordinal       uint64 `json:"ordinal"`
	Revision      int64  `json:"revision"`
	ProofRef      string `json:"proofRef"`
	ProofSHA256   string `json:"proofSha256"`
	PayloadSHA256 string `json:"payloadSha256"`
	PayloadBytes  int    `json:"payloadBytes"`
	Frames        int    `json:"frames"`
	Status        string `json:"status"`
	ReceivedAt    string `json:"receivedAt"`
}

// CollectUsage captures and normalizes the exact enrolled Codex account and
// usage stream. asOf is caller supplied and is never replaced with wall clock.
// The authority-issued capture revision is attached through NormalizeCapture.
func (f *Factory) CollectUsage(ctx context.Context, binding usage.Binding, policy usage.Policy, asOf time.Time) (UsageCapture, error) {
	if f == nil || ctx == nil || asOf.IsZero() || binding.ID != f.cfg.UsageBindingID || binding.StreamID != f.cfg.UsageStreamID || binding.SourceID != f.cfg.SourceID || binding.Host != "codex" || binding.HostVersion != f.cfg.Version || usage.ValidateBinding(binding) != nil || usage.ValidatePolicy(policy) != nil || !codexCollectionAllowed(binding, policy) {
		return UsageCapture{}, errors.New("Codex usage binding or live collection policy mismatch")
	}
	observation, payload, err := f.Collect(ctx)
	if err != nil {
		return UsageCapture{}, err
	}
	if observation.AccountRef != f.cfg.AccountRef || observation.SourceID != binding.SourceID || observation.Ordinal == 0 || observation.Revision != int64(observation.Ordinal) || observation.Result.Status != "complete" || !validDigest(observation.ProofSHA256) {
		return UsageCapture{}, errors.New("Codex capture does not match exact account and source")
	}
	export, report := collectors.NormalizeCapture("codex", payload, binding, policy, asOf, int64(observation.Ordinal))
	metadata := usageCaptureMetadata{Version: "aok-codex-capture/v1", AccountRef: observation.AccountRef, SourceID: observation.SourceID, BindingID: binding.ID, StreamID: binding.StreamID, Ordinal: observation.Ordinal, Revision: observation.Revision, ProofRef: observation.ProofRef, ProofSHA256: observation.ProofSHA256, PayloadSHA256: hashBytes(payload), PayloadBytes: len(payload), Frames: observation.Result.Frames, Status: observation.Result.Status, ReceivedAt: asOf.UTC().Format(time.RFC3339Nano)}
	asset, err := json.Marshal(metadata)
	if err != nil {
		return UsageCapture{}, errors.New("Codex capture metadata encoding failed")
	}
	path := fmt.Sprintf("evidence/codex-capture-%d.json", observation.Ordinal)
	export.Evidence = append(export.Evidence, usage.Asset{Path: path, SHA256: hashBytes(asset)})
	files, err := usage.Files(export)
	if err != nil {
		return UsageCapture{}, fmt.Errorf("normalized Codex usage export invalid: %w", err)
	}
	files[path] = asset
	return UsageCapture{Export: export, Report: report, Assets: files}, nil
}

func codexCollectionAllowed(b usage.Binding, p usage.Policy) bool {
	for _, rule := range p.Sources {
		if rule.ID != b.SourceID || rule.Host != b.Host || rule.HostVersion != b.HostVersion || rule.AdapterVersion != collectors.AdapterVersion || !rule.CollectionAllowed {
			continue
		}
		for _, mode := range rule.AuthModes {
			if mode == b.AuthMode && mode != "" {
				return true
			}
		}
	}
	return false
}

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Collect runs only the existing quota read protocol and binds its payload to
// the external ordinal and revision returned by the capture authority.
func (f *Factory) Collect(ctx context.Context) (CaptureObservation, []byte, error) {
	if ctx == nil {
		return CaptureObservation{}, nil, errors.New("Codex collector context required")
	}
	f.mu.Lock()
	if f.active || f.collecting {
		f.mu.Unlock()
		return CaptureObservation{}, nil, errors.New("Codex capture already active")
	}
	f.collecting = true
	f.last = CaptureObservation{}
	f.completeErr = nil
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.collecting = false; f.collectOpen = false; f.mu.Unlock() }()
	payload, err := (collectors.CodexAppServerTransport{Factory: f}).Request(ctx, "account/rateLimits/read")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		return f.last, nil, err
	}
	if f.completeErr != nil || f.last.Result.Status != "complete" {
		return f.last, nil, errors.New("Codex capture completion was not confirmed")
	}
	return f.last, payload, nil
}

func (f *Factory) Open(ctx context.Context) (collectors.CodexChannel, error) {
	if ctx == nil {
		return nil, errors.New("Codex collector context required")
	}
	f.mu.Lock()
	if f.active || f.collecting && f.collectOpen {
		f.mu.Unlock()
		return nil, errors.New("Codex capture already active")
	}
	if f.collecting {
		f.collectOpen = true
	}
	f.active = true
	f.mu.Unlock()
	opened := false
	defer func() {
		if !opened {
			f.mu.Lock()
			f.active = false
			f.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	keepProcess := false
	defer func() {
		if !keepProcess {
			cancel()
		}
	}()
	grant, err := f.capture.Acquire(ctx, f.cfg.AccountRef, f.cfg.SourceID)
	if err != nil {
		return nil, errors.New("external Codex capture grant unavailable")
	}
	leaseOwned := false
	defer func() {
		if !leaseOwned {
			completeCtx, completeCancel := context.WithTimeout(context.Background(), time.Second)
			defer completeCancel()
			_ = f.capture.Complete(completeCtx, grant, CaptureResult{Status: "uncertain"})
		}
	}()
	if grant.Ordinal == 0 || grant.Revision < 1 || grant.LeaseID == "" || grant.AccountRef != f.cfg.AccountRef || grant.SourceID != f.cfg.SourceID || grant.ProofRef == "" || len(grant.ProofSHA256) != 64 || !time.Now().Before(grant.ValidUntil) {
		return nil, errors.New("external Codex capture grant unavailable")
	}
	if _, err := hex.DecodeString(grant.ProofSHA256); err != nil {
		return nil, errors.New("invalid external Codex capture proof")
	}
	f.mu.Lock()
	f.last = CaptureObservation{Ordinal: grant.Ordinal, Revision: grant.Revision, LeaseID: grant.LeaseID, ProofRef: grant.ProofRef, AccountRef: grant.AccountRef, SourceID: grant.SourceID, ProofSHA256: grant.ProofSHA256}
	f.mu.Unlock()
	scope := Scope{ExecutableSHA256: f.cfg.ExecutableSHA256, Version: f.cfg.Version, AccountRef: f.cfg.AccountRef, SourceID: f.cfg.SourceID, Ordinal: grant.Ordinal, CaptureProofRef: grant.ProofRef, LeaseID: grant.LeaseID, Revision: grant.Revision, ValidUntil: grant.ValidUntil, ProofSHA256: grant.ProofSHA256, NativeAccountProofRef: f.cfg.NativeAccountProofRef, NativeAccountProofSHA256: f.cfg.NativeAccountProofSHA256, HomeSHA256: f.cfg.HomeSHA256, EnvironmentCensusSHA256: f.cfg.EnvironmentCensusSHA256}
	if err = f.authority.Check(ctx, scope); err != nil {
		return nil, errors.New("Codex collector admission denied")
	}
	digest, err := executableDigest(f.cfg.Executable)
	if err != nil || digest != f.cfg.ExecutableSHA256 {
		return nil, errors.New("Codex executable digest mismatch")
	}
	if err = f.checkVersion(ctx); err != nil {
		return nil, errors.New("Codex executable version mismatch")
	}
	// Recheck current external authority immediately before creating the process.
	if err = f.authority.Check(ctx, scope); err != nil {
		return nil, errors.New("Codex launch admission denied")
	}
	digest, err = executableDigest(f.cfg.Executable)
	if err != nil || digest != f.cfg.ExecutableSHA256 {
		return nil, errors.New("Codex executable changed before launch")
	}
	args := []string{"app-server", "--listen", "stdio://", "--strict-config"}
	for _, setting := range []string{"features.apps=false", "features.hooks=false", "features.multi_agent=false", "features.memories=false", "features.shell_snapshot=false", "notify=[]", `web_search="disabled"`} {
		args = append(args, "-c", setting)
	}
	cmd := exec.CommandContext(ctx, f.cfg.Executable, args...)
	if err = configureProcessGroup(cmd); err != nil {
		return nil, errors.New("Codex process isolation unavailable")
	}
	cmd.Dir = f.cfg.Home
	cmd.Env = []string{"HOME=" + f.cfg.Home, "PATH=" + f.cfg.Path}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("Codex process pipe setup failed")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, errors.New("Codex process pipe setup failed")
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, errors.New("Codex App Server failed to start")
	}
	ch := &channel{cmd: cmd, in: stdin, reader: bufio.NewReaderSize(stdout, maxFrameBytes+1), done: make(chan struct{}), ctx: ctx, cancel: cancel, read: make(chan frameResult, 1), grant: grant, capture: f.capture, owner: f, responseHash: sha256.New()}
	processDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessGroup(cmd)
		case <-processDone:
		}
	}()
	ch.processDone = processDone
	keepProcess = true
	opened = true
	leaseOwned = true
	go ch.readLoop()
	return ch, nil
}

func (f *Factory) checkVersion(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.cfg.Executable, "--version")
	if err := configureProcessGroup(cmd); err != nil {
		return err
	}
	cmd.Dir = f.cfg.Home
	cmd.Env = []string{"HOME=" + f.cfg.Home, "PATH=" + f.cfg.Path}
	var output limitedOutput
	output.limit = 256
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
		killProcessGroup(cmd)
	}
	out := output.Bytes()
	if err != nil || output.exceeded || len(out) > 256 {
		return errors.New("version probe failed")
	}
	fields := strings.Fields(string(out))
	for _, field := range fields {
		if strings.TrimPrefix(field, "v") == f.cfg.Version {
			return nil
		}
	}
	return errors.New("version differs from enrollment")
}

func executableDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 || st.Size() > 512<<20 {
		return "", errors.New("invalid executable file")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, (512<<20)+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type frameResult struct {
	data []byte
	err  error
}
type channel struct {
	cmd          *exec.Cmd
	in           io.WriteCloser
	reader       *bufio.Reader
	done         chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	closed       bool
	total        int
	frames       int
	read         chan frameResult
	grant        CaptureGrant
	capture      CaptureAuthority
	owner        *Factory
	responseHash hash.Hash
	responseOK   bool
	processDone  chan struct{}
}

func (c *channel) readLoop() {
	defer close(c.done)
	for {
		line, err := c.reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			c.publish(nil, errors.New("Codex frame exceeds limit"))
			return
		}
		if len(line) > 0 {
			if len(line) > maxFrameBytes {
				c.publish(nil, errors.New("Codex frame exceeds limit"))
				return
			}
			line = bytesTrimLine(line)
			c.publish(line, nil)
		}
		if err != nil {
			c.publish(nil, err)
			return
		}
	}
}

func bytesTrimLine(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return append([]byte(nil), b...)
}

func (c *channel) publish(b []byte, err error) {
	select {
	case c.read <- frameResult{data: b, err: err}:
	case <-c.ctx.Done():
	}
}

func (c *channel) Send(ctx context.Context, b []byte) error {
	if ctx == nil || len(b) == 0 || len(b) > maxFrameBytes {
		return errors.New("invalid Codex request frame")
	}
	if !allowedRequest(b) {
		return errors.New("Codex collector method is not allowed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("Codex channel closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.frames++
	c.total += len(b) + 1
	if c.frames > maxFrames || c.total > maxTotalBytes {
		return errors.New("Codex capture budget exceeded")
	}
	_, err := c.in.Write(append(append([]byte(nil), b...), '\n'))
	return err
}

func allowedRequest(b []byte) bool {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(b, &request) != nil {
		return false
	}
	switch request.Method {
	case "initialize":
		return string(request.ID) == "1" && json.Valid(request.Params)
	case "initialized":
		return len(request.ID) == 0 && len(request.Params) == 0
	case "account/rateLimits/read":
		if string(request.ID) != "2" {
			return false
		}
		var params map[string]json.RawMessage
		if json.Unmarshal(request.Params, &params) != nil || len(params) != 1 {
			return false
		}
		var refresh bool
		raw, ok := params["refreshToken"]
		return ok && json.Unmarshal(raw, &refresh) == nil && !refresh
	default:
		return false
	}
}

func (c *channel) Receive(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("Codex receive context required")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	case r, ok := <-c.read:
		if !ok {
			return nil, io.EOF
		}
		if r.err != nil {
			return nil, r.err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.frames++
		c.total += len(r.data) + 1
		if len(r.data) == 0 || len(r.data) > maxFrameBytes || c.frames > maxFrames || c.total > maxTotalBytes {
			return nil, errors.New("Codex capture budget exceeded")
		}
		_, _ = c.responseHash.Write(r.data)
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(r.data, &response) == nil && string(response.ID) == "2" && len(response.Result) > 0 && (len(response.Error) == 0 || string(response.Error) == "null") {
			c.responseOK = true
		}
		return append([]byte(nil), r.data...), nil
	}
}

func (c *channel) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	_ = c.in.Close()
	killProcessGroup(c.cmd)
	if c.cmd.Process != nil {
		_ = c.cmd.Wait()
	}
	close(c.processDone)
	status := "uncertain"
	if c.responseOK {
		status = "complete"
	}
	digest := hex.EncodeToString(c.responseHash.Sum(nil))
	completeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := c.capture.Complete(completeCtx, c.grant, CaptureResult{Status: status, Bytes: c.total, Frames: c.frames, SHA256: digest})
	c.mu.Lock()
	c.owner.mu.Lock()
	c.owner.last.Result = CaptureResult{Status: status, Bytes: c.total, Frames: c.frames, SHA256: digest}
	c.owner.completeErr = err
	c.owner.active = false
	c.owner.mu.Unlock()
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("capture lease completion failed: %w", err)
	}
	return nil
}

var _ collectors.CodexChannelFactory = (*Factory)(nil)
var _ collectors.CodexChannel = (*channel)(nil)

func (s Scope) String() string { return fmt.Sprintf("codex:%s:%d", s.SourceID, s.Ordinal) }

func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == strings.ToLower(s)
}

type limitedOutput struct {
	data     []byte
	limit    int
	exceeded bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if remaining < 0 {
		remaining = 0
	}
	if len(p) > remaining {
		if remaining > 0 {
			b.data = append(b.data, p[:remaining]...)
		}
		b.exceeded = true
	} else {
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *limitedOutput) Bytes() []byte { return b.data }
