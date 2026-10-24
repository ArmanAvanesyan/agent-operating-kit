package verifier

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/artifacts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

const uploadTimeout = 30 * time.Second

type UploadingControls struct {
	remote    *verification.RemoteControls
	route     verification.RemoteRoute
	candidate verification.Candidate
}

// NewUploadingControls binds the transfer source to the externally signed
// proof and pinned command. It does not create or modify control evidence.
func NewUploadingControls(route verification.RemoteRoute, proof verification.ControlProof, spec verification.VerifierSpec, candidate verification.Candidate) (verification.Controls, error) {
	if candidate.BundlePath == "" || candidate.Digest == "" || candidate.SnapshotSHA256 == "" || proof.CandidateSHA256 != candidate.Digest || proof.SnapshotSHA256 != candidate.SnapshotSHA256 {
		return nil, errors.New("candidate transfer does not match signed control proof")
	}
	fresh, err := verification.LoadCandidate(candidate.BundlePath)
	if err != nil || fresh.Digest != candidate.Digest || fresh.SnapshotSHA256 != candidate.SnapshotSHA256 {
		return nil, errors.New("candidate bundle failed immutable identity check")
	}
	remote, err := verification.NewRemoteControls(route, proof, spec)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(route.Endpoint)
	if err != nil {
		return nil, errors.New("invalid verifier route")
	}
	u.Path = path.Join(path.Dir(u.Path), "candidates")
	if u.Path != "/v1/candidates" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid candidate upload endpoint")
	}
	return &UploadingControls{remote: remote.(*verification.RemoteControls), route: route, candidate: fresh}, nil
}

func (u *UploadingControls) Proof() verification.ControlProof {
	if u == nil || u.remote == nil {
		return verification.ControlProof{}
	}
	return u.remote.Proof()
}

func (u *UploadingControls) Execute(ctx context.Context, argv []string, env map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	if u == nil || u.remote == nil || ctx == nil {
		return 0, nil, errors.New("invalid uploading verifier controls")
	}
	if err := u.upload(ctx); err != nil {
		return 0, nil, err
	}
	return u.remote.Execute(ctx, argv, env, timeout, limit)
}

func (u *UploadingControls) upload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fresh, err := verification.LoadCandidate(u.candidate.BundlePath)
	if err != nil || fresh.Digest != u.candidate.Digest || fresh.SnapshotSHA256 != u.candidate.SnapshotSHA256 {
		return errors.New("candidate changed before immutable transfer")
	}
	body, err := artifacts.Encode(fresh)
	if err != nil {
		return errors.New("candidate transfer encoding failed")
	}
	bounded, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	secret, err := u.route.Resolver.Resolve(bounded, u.route.AuthReference)
	if err != nil || secret == "" || strings.ContainsAny(secret, "\r\n") {
		return errors.New("verifier transfer credential unavailable")
	}
	endpoint, err := url.Parse(u.route.Endpoint)
	if err != nil {
		return errors.New("invalid verifier endpoint")
	}
	endpoint.Path = "/v1/candidates"
	req, err := http.NewRequestWithContext(bounded, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid candidate upload request")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/x-tar")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-AOK-Candidate-SHA256", fresh.Digest)
	req.Header.Set("X-AOK-Snapshot-SHA256", fresh.SnapshotSHA256)
	proofBytes, err := json.Marshal(u.remote.Proof())
	if err != nil || len(proofBytes) > 16<<10 {
		return errors.New("candidate control proof exceeds bound")
	}
	req.Header.Set("X-AOK-Control-Proof", base64.StdEncoding.EncodeToString(proofBytes))
	client := http.Client{Transport: u.route.RoundTripper, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("candidate transfer request failed")
	}
	defer resp.Body.Close()
	const responseMax = 4096
	response, err := io.ReadAll(io.LimitReader(resp.Body, responseMax+1))
	if err != nil || len(response) > responseMax {
		return errors.New("candidate transfer response exceeded bound")
	}
	// Do not permit a broker/service to echo either the secret or authorization header.
	if containsConstant(response, []byte(secret)) || containsConstant(response, []byte(req.Header.Get("Authorization"))) {
		return errors.New("candidate transfer response reflected credentials")
	}
	var identity struct {
		CandidateSHA256 string `json:"candidateSha256"`
		SnapshotSHA256  string `json:"snapshotSha256"`
	}
	if strictJSON(response, &identity) != nil || identity.CandidateSHA256 != fresh.Digest || identity.SnapshotSHA256 != fresh.SnapshotSHA256 {
		return errors.New("candidate transfer identity response mismatch")
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return errors.New("candidate transfer rejected")
	}
	// A conflict is reusable only when the service proves that the immutable object
	// already stored at this digest has the exact expected snapshot.
	return nil
}

func containsConstant(haystack, needle []byte) bool {
	return len(needle) > 0 && bytes.Contains(haystack, needle)
}
