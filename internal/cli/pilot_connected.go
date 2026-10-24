package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	workflow "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// PilotExecutionService is one protected enrollment. Task arguments cannot
// choose its runtime, keys, witness, route, trial or cumulative budget authority.
// The embedding operator qualifies these implementations before installing it.
type PilotExecutionService struct {
	Controller *workflow.Controller
	Enrollment workflow.PilotDispatch
}

func phase9PilotExecute(campaignPath, authorityPath, output string, service *PilotExecutionService, out io.Writer) (int, error) {
	if service == nil || service.Controller == nil {
		return 1, fmt.Errorf("qualified pilot enrollment unavailable")
	}
	path := campaignPath
	if st, e := os.Lstat(path); e == nil && st.IsDir() {
		path = filepath.Join(path, "manifest.json")
	}
	m, e := pilot.ReadManifest(path)
	if e != nil {
		return 1, e
	}
	var grant pilot.PromotionGrant
	if e = phase8Read(authorityPath, &grant); e != nil {
		return 1, e
	}
	enrollment := service.Enrollment
	if usage.Digest(m) != usage.Digest(enrollment.Manifest) || usage.Digest(grant) != usage.Digest(enrollment.Grant) {
		return 1, fmt.Errorf("campaign or signed authority differs from protected enrollment")
	}
	writer, e := evidence.Reserve(output, campaignPath, authorityPath, service.Controller.Root)
	if e != nil {
		return 1, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, runErr := service.Controller.DispatchPilotOnce(ctx, enrollment)
	if e = writer.Write(map[string][]byte{"pilot-execution.json": mustPhase8JSON(report)}); e != nil {
		return 1, e
	}
	_, e = phase8Print(out, report)
	if runErr != nil {
		return 1, runErr
	}
	if report.Status == "blocked" || report.Status == "invalid" {
		return 1, e
	}
	return 0, e
}
