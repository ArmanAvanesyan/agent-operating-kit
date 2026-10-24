package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

func Decode(data []byte, target any) error {
	if err := contracts.ValidateJSON(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) == nil {
		return errors.New("trailing JSON value")
	}
	return nil
}
func ReadPolicy(path string) (Policy, []byte, error) {
	b, e := modelcatalog.ReadFile(path)
	if e != nil {
		return Policy{}, nil, e
	}
	if e = ValidateJSON(b, "policy.json"); e != nil {
		return Policy{}, b, e
	}
	var p Policy
	if e = Decode(b, &p); e != nil {
		return p, b, e
	}
	if e = ValidatePolicy(p); e != nil {
		return p, b, e
	}
	p.rawSHA256 = Hash(b)
	p.canonicalSHA256 = Digest(p)
	return p, b, nil
}

// Load reads usage.json (or an explicit file) and a separately trusted policy.
func Load(path, policyPath string) (Export, Policy, error) {
	var e Export
	p, pb, err := ReadPolicy(policyPath)
	if err != nil {
		return e, p, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return e, p, err
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		info, checkErr := os.Lstat(cur)
		if checkErr != nil {
			return e, p, checkErr
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return e, p, errors.New("usage path has a symlink ancestor")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	st, err := os.Lstat(path)
	if err != nil {
		return e, p, err
	}
	root, file := path, "usage.json"
	if st.Mode()&fs.ModeSymlink != 0 {
		return e, p, errors.New("symlink input is forbidden")
	}
	if st.IsDir() {
		// Bundle directory: read the canonical usage.json member.
	} else if st.Mode().IsRegular() {
		root = filepath.Dir(path)
		file = filepath.Base(path)
	} else {
		return e, p, errors.New("usage input must be a regular file or directory")
	}
	b, err := modelcatalog.ReadAsset(root, file)
	if err != nil {
		return e, p, err
	}
	if err = ValidateJSON(b, "export.json"); err != nil {
		return e, p, err
	}
	if err = Decode(b, &e); err != nil {
		return e, p, err
	}
	if e.PolicySHA256 != Hash(pb) {
		return e, p, errors.New("export policy digest does not match supplied policy bytes")
	}
	if err = ValidateExport(e, p); err != nil {
		return e, p, err
	}
	for _, a := range e.Evidence {
		data, x := modelcatalog.ReadAsset(root, a.Path)
		if x != nil {
			return e, p, x
		}
		if Hash(data) != a.SHA256 {
			return e, p, fmt.Errorf("evidence hash mismatch: %s", a.Path)
		}
	}
	return e, p, nil
}

// Files returns canonical sanitized JSON payloads suitable for a new bundle.
func Files(e Export) (map[string][]byte, error) {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if err = ValidateJSON(b, "export.json"); err != nil {
		return nil, err
	}
	return map[string][]byte{"usage.json": b}, nil
}
