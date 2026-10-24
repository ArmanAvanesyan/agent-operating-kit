package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

// phase9VerifyRun delegates pinned commands to externally installed isolation.
// It records invocation facts only. A zero exit code is not signed acceptance.
func phase9VerifyRun(c verification.Candidate, p verification.VerificationPolicy, output, bundle, policyPath string, services Phase9Services, out io.Writer) (int, error) {
	count := 0
	for _, spec := range p.Verifiers {
		if spec.Kind == "command" {
			count++
		}
	}
	if count == 0 || count > 100 {
		return 1, fmt.Errorf("bounded policy command inventory required")
	}
	writer, e := evidence.Reserve(output, bundle, policyPath)
	if e != nil {
		return 1, e
	}
	parent := services.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	results := []map[string]any{}
	code := 0
	for _, spec := range p.Verifiers {
		if spec.Kind != "command" {
			continue
		}
		controls, err := services.VerifierControls(ctx, c, p, spec)
		if err != nil || controls == nil {
			code = 1
			results = append(results, map[string]any{"verifier": spec.ID, "status": "blocked", "reason": "qualified verifier controls unavailable"})
			break
		}
		data, exit, err := verification.RunCommand(ctx, c, p, spec.ID, controls)
		status := "invocation-observed"
		if err != nil {
			status = "unknown"
			code = 1
		}
		if exit != 0 {
			status = "failed"
			code = 1
		}
		results = append(results, map[string]any{"verifier": spec.ID, "status": status, "exitCode": exit, "outputSha256": usage.Hash(data), "outputBytes": len(data)})
		if err != nil {
			break
		}
	}
	report := map[string]any{"version": verification.Version, "scope": "injected-control-transport", "candidateSha256": c.Digest, "results": results, "authorizationGranted": false, "executionReady": false, "acceptance": "independent signed receipts required"}
	if e = writer.Write(map[string][]byte{"verification-report.json": mustPhase8JSON(report)}); e != nil {
		return 1, e
	}
	_, e = phase8Print(out, report)
	return code, e
}
