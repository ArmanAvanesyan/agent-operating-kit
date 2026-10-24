package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostprotocol"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// NativeV2Run executes one documented machine-output CLI protocol. It is
// called only from RunControlled, so claims, fences, P3 holds and start hooks
// stay owned by the P7 controller. Resume and native task dispatch are denied.
func NativeV2Run(ctx context.Context, p Prepared, policy Policy, runner Runner, attemptID string) (Transcript, error) {
	var zero Transcript
	if ctx == nil || runner == nil || attemptID == "" || policy.Version != VersionV2 || p.Version != VersionV2 || p.Host != policy.Host || p.HostVersion != policy.HostVersion || p.HostDriverSHA256 != policy.HostDriverSHA256 || p.HostProtocol != policy.HostProtocol || p.PolicySHA256 != PolicyHash(policy) || p.Model == "" || !json.Valid(p.Input) || !json.Valid(p.OutputSchema) || len(p.NativeTasks) != 0 || policy.AllowNativeTasks || !v2Representable(policy) {
		return zero, errors.New("invalid v2 host protocol execution")
	}
	for _, name := range policy.Environment {
		if isCredentialEnv(name) {
			return zero, errors.New("credential environment passthrough is forbidden")
		}
	}
	launch, err := V2Launch(p, policy)
	if err != nil {
		return zero, err
	}
	proc, err := runner.Start(ctx, launch)
	if err != nil {
		return zero, errors.New("v2 host process could not start")
	}
	defer proc.Close()
	frames := make([][]byte, 0, 32)
	total := 0
	for {
		frame, receiveErr := proc.Receive(ctx)
		if receiveErr != nil {
			if ctx.Err() != nil {
				_ = proc.Stop()
				_ = proc.Wait()
				return zero, ctx.Err()
			}
			if errors.Is(receiveErr, io.EOF) {
				break
			}
			return zero, errors.New("v2 host output could not be read")
		}
		total += len(frame)
		if len(frame) == 0 || len(frame) > MaxFrameBytes || total > MaxCaptureBytes || len(frames) >= hostprotocol.MaxFrames {
			_ = proc.Stop()
			return zero, errors.New("v2 host output exceeded capture bounds")
		}
		frames = append(frames, append([]byte(nil), frame...))
	}
	if err := proc.Wait(); err != nil {
		return zero, errors.New("v2 host process exited unsuccessfully")
	}
	o, err := hostprotocol.ReplayScoped(p.Host, p.AssignmentSHA256, attemptID, frames)
	if err != nil {
		return zero, fmt.Errorf("host protocol parse failed: %w", err)
	}
	t := Transcript{Events: []Event{}, Output: nil, SessionID: observationString(o.Session), Model: observationString(o.Model), HostOutcome: "unknown", ObservedTools: []string{}, UsageFrames: []json.RawMessage{}, Diagnostics: []Diagnostic{}}
	if o.Outcome.Status == "observed" {
		var outcome struct {
			State string `json:"state"`
		}
		if json.Unmarshal(o.Outcome.Value, &outcome) == nil {
			switch outcome.State {
			case "completed":
				t.HostOutcome = "completed"
			case "failed":
				t.HostOutcome = "failed"
			}
		}
	}
	if o.Output.Status == "observed" {
		var output string
		if json.Unmarshal(o.Output.Value, &output) == nil {
			t.Output = json.RawMessage(output)
		}
	}
	for _, event := range o.Events {
		sequence := 0
		if event.Sequence != nil {
			sequence = int(*event.Sequence)
		}
		t.Events = append(t.Events, Event{Sequence: sequence, NativeID: event.ID, Type: event.Kind, SessionID: event.SessionID, Data: append(json.RawMessage(nil), event.Raw...)})
		if strings.Contains(event.Kind, "tool") {
			t.ObservedTools = append(t.ObservedTools, event.Kind)
		}
	}
	if o.Usage.Status == "observed" {
		t.UsageFrames = append(t.UsageFrames, append(json.RawMessage(nil), o.Usage.Value...))
	}
	for _, d := range o.Diagnostics {
		t.Diagnostics = append(t.Diagnostics, Diagnostic{Code: "host-event", Field: "events", Message: d})
	}
	return t, nil
}

func V2Launch(p Prepared, policy Policy) (Launch, error) {
	if policy.Version != VersionV2 || p.Version != VersionV2 || p.Host != policy.Host || p.HostVersion != policy.HostVersion || p.HostDriverSHA256 != policy.HostDriverSHA256 || p.HostProtocol != policy.HostProtocol || p.PolicySHA256 != PolicyHash(policy) || policy.TimeoutSeconds < 1 || policy.TimeoutSeconds > 60 || !v2Representable(policy) {
		return Launch{}, errors.New("v2 launch policy mismatch")
	}
	var task any
	if err := usage.Decode(p.Input, &task); err != nil {
		return Launch{}, errors.New("invalid captured task input")
	}
	promptBytes, _ := json.Marshal(struct {
		Instructions string          `json:"instructions"`
		Task         any             `json:"task"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	}{p.Instructions, task, p.OutputSchema})
	prompt := string(promptBytes)
	args, err := hostdriver.Arguments(p.Host, p.Workspace, p.Model, prompt)
	if err != nil {
		return Launch{}, err
	}
	return Launch{Executable: policy.Executable, Args: args, Dir: p.Workspace, Env: AllowedEnvironment(policy), TimeoutSeconds: policy.TimeoutSeconds, MaxFrameBytes: MaxFrameBytes, MaxTotalBytes: MaxCaptureBytes}, nil
}

func observationString(d hostprotocol.Dimension) string {
	if d.Status != "observed" {
		return ""
	}
	var value string
	_ = json.Unmarshal(d.Value, &value)
	return value
}

// v2Representable rejects every profile/policy dimension these fixed argv
// adapters cannot apply. Silence is not evidence that a requested control ran.
func v2Representable(p Policy) bool {
	c := p.Configuration
	return len(p.Tools) == 0 && len(p.CodexConfig) == 0 && (len(p.Settings) == 0 || string(p.Settings) == "null") &&
		c.Effort == nil && c.ReasoningBudgetTokens == nil && c.Generation == nil && c.ServiceTier == nil && c.ModelRevision == nil && c.Deployment == nil && c.ToolProfile == nil &&
		(c.ReasoningMode == nil || *c.ReasoningMode == "provider-default")
}
