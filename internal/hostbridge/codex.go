package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"io"
	"sort"
	"strings"
	"time"
)

const CodexPinnedVersion = "0.162.1"

type rpcFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func rpcMessage(id int, method string, params any) []byte {
	b, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	return b
}
func object(raw []byte) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	_ = usage.Decode(raw, &m)
	return m
}
func text(m map[string]json.RawMessage, k string) string {
	var s string
	_ = usage.Decode(m[k], &s)
	return s
}
func CodexRun(ctx context.Context, p Prepared, policy Policy, runner Runner, previous *Receipt, broker ApprovalBroker) (Transcript, error) {
	t := Transcript{Events: []Event{}, ObservedTools: []string{}, UsageFrames: []json.RawMessage{}, Diagnostics: []Diagnostic{}, HostOutcome: "unknown"}
	if p.Host != "codex" || policy.Host != "codex" || p.HostVersion != CodexPinnedVersion || policy.HostVersion != CodexPinnedVersion || p.Version != Version || policy.Version != Version || !contains(policy.Models, p.Model) {
		return t, fmt.Errorf("unsupported Codex protocol version")
	}
	if e := auditCodexAmbient(policy); e != nil {
		return t, e
	}
	sandbox, e := codexSandbox(p, policy, time.Now().UTC())
	if e != nil {
		return t, e
	}
	args := []string{"app-server", "--listen", "stdio://", "--strict-config"}
	// Disable ambient continuations/integrations without changing the user's files.
	for _, v := range []string{"features.apps=false", "features.hooks=false", "features.multi_agent=false", "features.memories=false", "features.shell_snapshot=false", "notify=[]", `web_search="disabled"`} {
		args = append(args, "-c", v)
	}
	keys := []string{}
	for k := range policy.CodexConfig {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-c", k+"="+string(policy.CodexConfig[k]))
	}
	proc, e := runner.Start(ctx, Launch{Executable: policy.Executable, Args: args, Dir: p.Workspace, Env: AllowedEnvironment(policy), TimeoutSeconds: policy.TimeoutSeconds, MaxFrameBytes: MaxFrameBytes, MaxTotalBytes: MaxCaptureBytes})
	if e != nil {
		return t, e
	}
	defer proc.Close()
	if e = proc.Send(rpcMessage(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "aok-host-bridge", "title": "Agent Operating Kit", "version": "1.0.0"}, "capabilities": map[string]any{"experimentalApi": false}})); e != nil {
		return t, e
	}
	frames := [][]byte{}
	ready := false
	turnSent := false
	for {
		raw, e := proc.Receive(ctx)
		if e != nil {
			if ctx.Err() != nil {
				_ = proc.Send(rpcMessage(99, "turn/interrupt", map[string]any{"threadId": t.ThreadID, "turnId": t.TurnID}))
				_ = proc.Stop()
				t.HostOutcome = "interrupted"
				return t, ctx.Err()
			}
			if errors.Is(e, io.EOF) {
				return ParseCodexFrames(p, previous, frames)
			}
			return t, e
		}
		var f rpcFrame
		if usage.Decode(raw, &f) != nil {
			return t, fmt.Errorf("malformed Codex frame")
		}
		if len(f.Error) > 0 && string(f.Error) != "null" {
			return t, fmt.Errorf("Codex protocol request failed")
		}
		id := string(f.ID)
		switch {
		case id == "1" && f.Method == "":
			_ = proc.Send([]byte(`{"method":"initialized"}`))
			if e = proc.Send(rpcMessage(2, "account/read", map[string]any{"refreshToken": false})); e != nil {
				return t, e
			}
		case id == "2" && f.Method == "":
			acc := object(object(f.Result)["account"])
			if text(acc, "type") != "chatgpt" {
				return t, fmt.Errorf("native subscription login required; no billing fallback")
			}
			ready = true
			params := map[string]any{"model": p.Model, "cwd": p.Workspace, "approvalPolicy": "never", "sandbox": sandbox.threadMode, "baseInstructions": p.Instructions, "config": map[string]any{"features.multi_agent": false, "features.hooks": false}}
			method := "thread/start"
			if previous != nil {
				method = "thread/resume"
				params["threadId"] = previous.ThreadID
				params["excludeTurns"] = true
			} else {
				params["ephemeral"] = false
			}
			if e = proc.Send(rpcMessage(3, method, params)); e != nil {
				return t, e
			}
		case id == "3" && f.Method == "" && ready:
			result := object(f.Result)
			thread := object(result["thread"])
			t.ThreadID = text(thread, "id")
			t.SessionID = text(thread, "sessionId")
			t.Model = text(result, "model")
			if policy.Configuration.Provider != nil && text(result, "modelProvider") != *policy.Configuration.Provider {
				return t, fmt.Errorf("reported provider mismatch")
			}
			if t.ThreadID == "" || t.Model != p.Model || (previous != nil && t.ThreadID != previous.ThreadID) {
				return t, fmt.Errorf("Codex thread/model identity mismatch")
			}
			if t.SessionID == "" {
				return t, fmt.Errorf("Codex session identity unavailable")
			}
			// Handshake and account lookup may consume most of the five-minute
			// proof window. Recheck the write boundary immediately before turn/start.
			if sandbox.writeIntent {
				if _, e = codexSandbox(p, policy, time.Now().UTC()); e != nil {
					return t, e
				}
			}
			// Redact all unrelated thread/account metadata before persisting.
			safe, _ := json.Marshal(map[string]any{"id": 3, "result": map[string]any{"model": t.Model, "thread": map[string]any{"id": t.ThreadID, "sessionId": t.SessionID}}})
			frames = append(frames, safe)
			start := map[string]any{"threadId": t.ThreadID, "model": p.Model, "input": []any{map[string]any{"type": "text", "text": string(p.Input)}}, "approvalPolicy": "never", "sandboxPolicy": sandbox.turnPolicy, "outputSchema": json.RawMessage(p.OutputSchema)}
			if v, ok := policy.CodexConfig["model_reasoning_effort"]; ok {
				var effort string
				_ = json.Unmarshal(v, &effort)
				start["effort"] = effort
			}
			if e = proc.Send(rpcMessage(4, "turn/start", start)); e != nil {
				return t, e
			}
			turnSent = true
		case f.Method != "" && len(f.ID) > 0 && string(f.ID) != "null":
			result := json.RawMessage(`{"decision":"decline"}`)
			switch f.Method {
			case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
			case "item/permissions/requestApproval":
				result = json.RawMessage(`{"permissions":{}}`)
			default:
				_ = proc.Stop()
				return t, fmt.Errorf("unknown server request blocked")
			}
			if broker != nil && !sandbox.writeIntent {
				result, e = broker.Decide(ctx, Approval{ThreadID: t.ThreadID, TurnID: t.TurnID, RequestID: f.ID, Method: f.Method, Params: f.Params})
				if e != nil {
					return t, e
				}
			}
			reply, _ := json.Marshal(map[string]any{"id": f.ID, "result": result})
			if e = proc.Send(reply); e != nil {
				return t, e
			}
		default:
			if !turnSent {
				continue
			}
			frames = append(frames, append([]byte(nil), raw...))
			if id == "4" && f.Method == "" {
				t.TurnID = text(object(object(f.Result)["turn"]), "id")
			}
			if f.Method == "turn/completed" {
				parsed, e := ParseCodexFrames(p, previous, frames)
				return parsed, e
			}
		}
	}
}
func ParseCodexFrames(p Prepared, previous *Receipt, frames [][]byte) (Transcript, error) {
	t := Transcript{HostOutcome: "unknown", Events: []Event{}, UsageFrames: []json.RawMessage{}, ObservedTools: []string{}, Diagnostics: []Diagnostic{}}
	seen := map[string]string{}
	terminal := false
	itemText := map[string]string{}
	for _, raw := range frames {
		if len(raw) > MaxFrameBytes {
			return t, fmt.Errorf("oversized Codex frame")
		}
		var f rpcFrame
		if usage.Decode(raw, &f) != nil {
			return t, fmt.Errorf("malformed Codex frame")
		}
		if len(f.Error) > 0 && string(f.Error) != "null" {
			return t, fmt.Errorf("Codex request failed")
		}
		params := object(f.Params)
		if string(f.ID) == "3" && f.Method == "" {
			res := object(f.Result)
			thread := object(res["thread"])
			t.ThreadID = text(thread, "id")
			t.SessionID = text(thread, "sessionId")
			t.Model = text(res, "model")
			if t.Model != p.Model || t.ThreadID == "" || t.SessionID == "" {
				return t, fmt.Errorf("Codex exact identity unavailable")
			}
			if previous != nil && (previous.ThreadID != t.ThreadID || previous.SessionID != t.SessionID) {
				return t, fmt.Errorf("Codex resume identity mismatch")
			}
			continue
		}
		if f.Method == "" && string(f.ID) == "4" {
			t.TurnID = text(object(object(f.Result)["turn"]), "id")
			continue
		}
		if f.Method == "" {
			continue
		}
		thread := text(params, "threadId")
		turn := text(params, "turnId")
		if thread != "" && thread != t.ThreadID {
			return t, fmt.Errorf("Codex event outside bound thread")
		}
		if turn != "" {
			if t.TurnID == "" {
				t.TurnID = turn
			}
			if turn != t.TurnID {
				return t, fmt.Errorf("Codex event outside bound turn")
			}
		}
		if terminal && f.Method != "turn/completed" {
			return t, fmt.Errorf("Codex event after terminal observation")
		}
		key := ""
		switch f.Method {
		case "item/completed":
			item := object(params["item"])
			id := text(item, "id")
			if id == "" {
				return t, fmt.Errorf("Codex item identity absent")
			}
			key = f.Method + ":" + id
			if text(item, "type") == "agentMessage" {
				itemText[id] = text(item, "text")
			}
		case "turn/completed":
			turnObj := object(params["turn"])
			id := text(turnObj, "id")
			if id == "" || t.TurnID != "" && id != t.TurnID {
				return t, fmt.Errorf("Codex terminal turn identity mismatch")
			}
			t.TurnID = id
			key = f.Method + ":" + id
			t.HostOutcome = text(turnObj, "status")
			terminal = true
		case "thread/tokenUsage/updated":
			if thread == "" {
				return t, fmt.Errorf("unscoped usage event")
			}
			t.UsageFrames = append(t.UsageFrames, sanitizeCodexFrame(f))
		case "account/rateLimits/updated":
			continue
		case "item/agentMessage/delta":
			continue
		case "turn/started", "item/started", "thread/started":
		default:
			continue
		}
		if key != "" {
			h := Hash(raw)
			if old, ok := seen[key]; ok {
				if old != h {
					return t, fmt.Errorf("conflicting duplicate Codex event")
				}
				continue
			}
			seen[key] = h
		}
		safe := sanitizeCodexFrame(f)
		if f.Method == "thread/started" {
			continue
		}
		t.Events = append(t.Events, Event{Sequence: len(t.Events) + 1, NativeID: key, Type: f.Method, SessionID: t.SessionID, ThreadID: t.ThreadID, TurnID: t.TurnID, Data: append(json.RawMessage(nil), safe...)})
	}
	if t.SessionID == "" {
		return t, fmt.Errorf("Codex initialized session absent")
	}
	ids := []string{}
	for id := range itemText {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 1 {
		t.Output = json.RawMessage(strings.TrimSpace(itemText[ids[0]]))
	} else if len(ids) > 1 {
		return t, fmt.Errorf("multiple final outputs ambiguous")
	}
	return t, nil
}

// Retain protocol identity and supported observations, never arbitrary host fields.
func sanitizeCodexFrame(f rpcFrame) json.RawMessage {
	p := object(f.Params)
	keep := func(src map[string]json.RawMessage, keys ...string) map[string]json.RawMessage {
		dst := map[string]json.RawMessage{}
		for _, k := range keys {
			if v, ok := src[k]; ok {
				dst[k] = v
			}
		}
		return dst
	}
	safe := keep(p, "threadId", "turnId")
	switch f.Method {
	case "item/completed", "item/started":
		item := keep(object(p["item"]), "id", "type")
		safe["item"], _ = json.Marshal(item)
	case "turn/completed", "turn/started":
		safe["turn"], _ = json.Marshal(keep(object(p["turn"]), "id", "status"))
	case "thread/tokenUsage/updated":
		u := object(p["tokenUsage"])
		clean := keep(u, "modelContextWindow")
		for _, k := range []string{"total", "last"} {
			clean[k], _ = json.Marshal(keep(object(u[k]), "inputTokens", "cachedInputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens"))
		}
		safe["tokenUsage"], _ = json.Marshal(clean)
	}
	b, _ := json.Marshal(map[string]any{"method": f.Method, "params": safe})
	return b
}
