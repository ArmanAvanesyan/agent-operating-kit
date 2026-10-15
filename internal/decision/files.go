package decision

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

type Report struct {
	Status      string                 `json:"status"`
	Diagnostics []contracts.Diagnostic `json:"diagnostics"`
}

// ReadDocument reads one bounded operational resource without executing it.
func ReadDocument(path, kind string) (map[string]any, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("input must be a bounded regular non-symlink file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("input changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, contracts.MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	parsed, _, err := contracts.Parse(b)
	if err != nil {
		return nil, err
	}
	r := parsed.(contracts.Resource)
	if r.Kind != kind {
		return nil, fmt.Errorf("expected %s, got %s", kind, r.Kind)
	}
	value, err := contracts.DecodeJSON(b)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func ValidateFiles(contractPath, resultPath string) Report {
	r := Report{Status: "invalid", Diagnostics: []contracts.Diagnostic{}}
	c, err := ReadDocument(contractPath, "DecisionContract")
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: "decision.contract", Source: contractPath, Severity: "error", Message: err.Error()})
		return r
	}
	result, err := ReadDocument(resultPath, "DecisionResult")
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: "decision.result", Source: resultPath, Severity: "error", Message: err.Error()})
		return r
	}
	r.Diagnostics = Check(c, result)
	for i := range r.Diagnostics {
		r.Diagnostics[i].Source = resultPath
	}
	contractAbs, _ := filepath.Abs(contractPath)
	resultAbs, _ := filepath.Abs(resultPath)
	root := filepath.Dir(contractAbs)
	if err := agentvalidate.CheckSchemaFile(root, filepath.Base(contractAbs), text(c["inputSchema"])); err != nil {
		r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: "decision.input-schema", Source: contractPath, Field: "inputSchema", Severity: "error", Message: err.Error()})
	}
	checkFile := func(owner, field, reference string) {
		corpus := &agentvalidate.Corpus{Root: filepath.Dir(owner)}
		if _, err := corpus.ResolveFile(filepath.Base(owner), reference); err != nil {
			r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: "decision.evidence.file", Source: owner, Field: field, Severity: "error", Message: err.Error()})
		}
	}
	for _, e := range list(result["evidence"]) {
		checkFile(resultAbs, "evidence", text(e))
	}
	if calibration := text(object(c["uncertainty"])["calibrationRef"]); calibration != "" {
		checkFile(contractAbs, "uncertainty.calibrationRef", calibration)
	}
	if len(r.Diagnostics) == 0 {
		r.Status = "valid"
	}
	return r
}
