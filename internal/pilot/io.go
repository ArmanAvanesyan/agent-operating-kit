package pilot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

func read(path string) ([]byte, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		info, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink path component forbidden")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return modelcatalog.ReadFile(abs)
}
func ReadManifest(path string) (Manifest, error) {
	var v Manifest
	b, e := read(path)
	if e != nil {
		return v, e
	}
	e = modelcatalog.Decode(b, &v)
	if e == nil {
		e = ValidateManifest(v)
	}
	return v, e
}
func ReadPolicy(path string) (Policy, error) {
	var v Policy
	b, e := read(path)
	if e != nil {
		return v, e
	}
	e = modelcatalog.Decode(b, &v)
	if e == nil {
		e = ValidatePolicy(v)
	}
	return v, e
}
func ReadBundle(path string) (Bundle, error) {
	var v Bundle
	b, e := read(path)
	if e != nil {
		return v, e
	}
	e = modelcatalog.Decode(b, &v)
	if e == nil && v.Version != Version {
		e = errors.New("invalid pilot bundle version")
	}
	return v, e
}
func ReadScenario(path string) (ReplayScenario, error) {
	var v ReplayScenario
	b, e := read(path)
	if e != nil {
		return v, e
	}
	e = modelcatalog.Decode(b, &v)
	if e == nil {
		_, e = Replay(v)
	}
	return v, e
}

func ValidatePolicy(p Policy) error {
	if p.Version != "aok-pilot-policy/v1" || !validSHA256(p.ManifestSHA256) {
		return errors.New("invalid externally supplied pilot policy")
	}
	if _, e := time.Parse(time.RFC3339, p.ExpiresAt); e != nil {
		return errors.New("policy expiry must be RFC3339")
	}
	for _, r := range p.Evidence {
		if r.Kind == "" || !validSHA256(r.SHA256) || !validSHA256(r.ScopeSHA256) {
			return errors.New("invalid policy evidence reference")
		}
	}
	return nil
}

// WriteJSON creates a new evidence file and refuses to replace existing data.
func WriteJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(b)
	return e
}
