package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const codexLiveMaxFrame = 1 << 20
const codexLiveMaxTotal = 4 << 20

// CodexChannel is a context-aware, already-qualified local App Server stream.
// The collector does not start processes, resolve credentials, or select paths.
type CodexChannel interface {
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
	Close() error
}

// CodexChannelFactory supplies an externally configured stdio/RPC stream for
// the exact approved Codex executable and account. Factory policy and identity
// checks remain the caller's responsibility.
type CodexChannelFactory interface {
	Open(context.Context) (CodexChannel, error)
}
type CodexChannelFactoryFunc func(context.Context) (CodexChannel, error)

func (f CodexChannelFactoryFunc) Open(ctx context.Context) (CodexChannel, error) { return f(ctx) }

// CodexAppServerTransport performs only initialize and account/rateLimits/read
// on the injected stream. It satisfies ReadOnlyTransport and intentionally does
// not expose a general JSON-RPC method surface.
type CodexAppServerTransport struct{ Factory CodexChannelFactory }

func (t CodexAppServerTransport) Request(ctx context.Context, method string) ([]byte, error) {
	if method != "account/rateLimits/read" || t.Factory == nil || ctx == nil {
		return nil, errors.New("unsupported Codex read request")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ch, err := t.Factory.Open(bounded)
	if err != nil || ch == nil {
		return nil, errors.New("Codex App Server stream unavailable")
	}
	defer ch.Close()
	var total int
	send := func(id int, m string, params any) error {
		b, e := json.Marshal(map[string]any{"id": id, "method": m, "params": params})
		if e != nil || len(b) > codexLiveMaxFrame {
			return errors.New("invalid Codex RPC frame")
		}
		total += len(b) + 1
		if total > codexLiveMaxTotal {
			return errors.New("Codex RPC byte budget exceeded")
		}
		return ch.Send(bounded, b)
	}
	receive := func() (map[string]json.RawMessage, error) {
		b, e := ch.Receive(bounded)
		if e != nil {
			return nil, e
		}
		total += len(b) + 1
		if len(b) == 0 || len(b) > codexLiveMaxFrame || total > codexLiveMaxTotal || !json.Valid(b) || contracts.ValidateJSON(b) != nil {
			return nil, errors.New("invalid Codex RPC response frame")
		}
		var frame map[string]json.RawMessage
		if json.Unmarshal(b, &frame) != nil {
			return nil, errors.New("invalid Codex RPC response")
		}
		return frame, nil
	}
	if err = send(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "aok-usage-collector", "version": AdapterVersion}, "capabilities": map[string]any{"experimentalApi": false}}); err != nil {
		return nil, err
	}
	ready := false
	for !ready {
		frame, e := receive()
		if e != nil {
			return nil, errors.New("Codex initialize handshake failed")
		}
		if string(frame["id"]) == "1" {
			if len(frame["error"]) > 0 && string(frame["error"]) != "null" {
				return nil, errors.New("Codex initialize rejected")
			}
			var result map[string]json.RawMessage
			if json.Unmarshal(frame["result"], &result) != nil || len(result) == 0 {
				return nil, errors.New("Codex initialize reply missing")
			}
			ready = true
		}
	}
	if err = ch.Send(bounded, []byte(`{"method":"initialized"}`)); err != nil {
		return nil, errors.New("Codex initialize notification failed")
	}
	if err = send(2, method, map[string]any{"refreshToken": false}); err != nil {
		return nil, err
	}
	for {
		frame, e := receive()
		if e != nil {
			return nil, errors.New("Codex rate-limit read failed")
		}
		if string(frame["id"]) != "2" {
			continue
		}
		if len(frame["error"]) > 0 && string(frame["error"]) != "null" {
			return nil, errors.New("Codex rate-limit read rejected")
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(frame["result"], &result) != nil || len(result) == 0 {
			return nil, errors.New("Codex rate-limit result missing")
		}
		// Retain only documented quota payload fields; unrelated account data is dropped.
		clean := map[string]json.RawMessage{}
		for _, key := range []string{"rateLimits", "rateLimitsByLimitId"} {
			if raw, ok := result[key]; ok {
				clean[key] = append(json.RawMessage(nil), raw...)
			}
		}
		if len(clean) == 0 {
			return nil, errors.New("Codex rate-limit fields missing")
		}
		payload, e := json.Marshal(map[string]any{"id": 2, "result": clean})
		if e != nil || len(payload) > codexLiveMaxFrame {
			return nil, fmt.Errorf("Codex quota payload invalid")
		}
		return payload, nil
	}
}
