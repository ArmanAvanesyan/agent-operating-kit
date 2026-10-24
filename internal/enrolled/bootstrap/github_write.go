package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

const maxGitHubCommentAsset = 1 << 20

// githubIssueNumberMapping verifies the root-signed source mapping against
// both the exact single-item scope and the operation identity before exposing
// it to tracker.ProviderWriter.
func githubIssueNumberMapping(source tracker.Source, immutableRemoteID, issueNumber, operationRemoteID string) (map[string]string, error) {
	if tracker.ValidateSource(source) != nil || source.Provider != "github" || len(source.Scope.ItemIDs) != 1 || immutableRemoteID == "" || immutableRemoteID != operationRemoteID {
		return nil, errors.New("GitHub immutable issue mapping is outside the signed operation scope")
	}
	scoped := strings.TrimPrefix(source.Scope.ItemIDs[0], "issue:")
	if scoped == source.Scope.ItemIDs[0] || scoped != issueNumber || !positiveGitHubIssueNumber(issueNumber) {
		return nil, errors.New("GitHub immutable issue mapping is invalid")
	}
	return map[string]string{immutableRemoteID: issueNumber}, nil
}

func positiveGitHubIssueNumber(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0 && strconv.Itoa(n) == value
}

// loadGitHubComment confines the signed comment asset to its configured root,
// rejects symlinks in every path component, and verifies the expected digest
// before returning bytes to the typed writer.
func loadGitHubComment(root, name, wantSHA256 string) ([]byte, error) {
	if root == "" || evidence.CheckPath(name) != nil || len(wantSHA256) != 64 {
		return nil, errors.New("invalid GitHub comment asset reference")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = requireRealDirectory(absRoot); err != nil {
		return nil, err
	}
	current := absRoot
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("comment asset path is unavailable")
		}
	}
	path := filepath.Join(absRoot, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > maxGitHubCommentAsset {
		return nil, errors.New("comment asset must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() > maxGitHubCommentAsset {
		return nil, errors.New("comment asset changed while opening")
	}
	body, err := io.ReadAll(io.LimitReader(f, maxGitHubCommentAsset+1))
	if err != nil || len(body) > maxGitHubCommentAsset || tracker.Hash(body) != strings.ToLower(wantSHA256) {
		return nil, errors.New("comment asset digest or size mismatch")
	}
	return body, nil
}

func requireRealDirectory(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := abs; ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("configured root path must not contain symlinks")
		}
		if current == abs && !info.IsDir() {
			return errors.New("configured root must be an existing directory")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

// githubWriteEvidenceSink creates durable, immutable evidence assets. It never
// replaces an existing response file, including after a restart.
func githubWriteEvidenceSink(root string) tracker.EvidenceSink {
	return func(stage string, body []byte, observedAt string) (contracts.EvidenceRef, error) {
		purpose := ""
		switch stage {
		case "dispatch":
			purpose = "provider-dispatch-response"
		case "readback":
			purpose = "provider-readback-observation"
		default:
			return contracts.EvidenceRef{}, errors.New("unsupported GitHub evidence stage")
		}
		if len(body) > 4<<20 || observedAt == "" || requireRealDirectory(root) != nil {
			return contracts.EvidenceRef{}, errors.New("GitHub evidence root or response is invalid")
		}
		digest := sha256.Sum256(body)
		hexDigest := hex.EncodeToString(digest[:])
		observedDigest := sha256.Sum256([]byte(observedAt))
		name := "github-write/" + stage + "-" + hexDigest + "-" + hex.EncodeToString(observedDigest[:8]) + ".json"
		if err := evidence.CheckPath(name); err != nil {
			return contracts.EvidenceRef{}, err
		}
		dir := filepath.Join(root, "github-write")
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return contracts.EvidenceRef{}, err
		}
		if err := requireRealDirectory(dir); err != nil {
			return contracts.EvidenceRef{}, err
		}
		f, err := os.OpenFile(filepath.Join(root, filepath.FromSlash(name)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return contracts.EvidenceRef{}, fmt.Errorf("create-only evidence write failed: %w", err)
		}
		written, writeErr := f.Write(body)
		if writeErr == nil && written != len(body) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return contracts.EvidenceRef{}, writeErr
		}
		if closeErr != nil {
			return contracts.EvidenceRef{}, closeErr
		}
		directory, err := os.Open(dir)
		if err != nil {
			return contracts.EvidenceRef{}, err
		}
		syncErr := directory.Sync()
		closeErr = directory.Close()
		if syncErr != nil {
			return contracts.EvidenceRef{}, syncErr
		}
		if closeErr != nil {
			return contracts.EvidenceRef{}, closeErr
		}
		return contracts.EvidenceRef{Asset: contracts.AssetRef{Path: name, SHA256: hexDigest}, Purpose: purpose, ObservedAt: observedAt}, nil
	}
}
