package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCodexChannel struct {
	sent   [][]byte
	frames [][]byte
	wait   bool
	closed bool
}

func (f *fakeCodexChannel) Send(_ context.Context, b []byte) error {
	f.sent = append(f.sent, append([]byte(nil), b...))
	return nil
}
func (f *fakeCodexChannel) Receive(ctx context.Context) ([]byte, error) {
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(f.frames) == 0 {
		return nil, errors.New("unexpected receive")
	}
	b := f.frames[0]
	f.frames = f.frames[1:]
	return b, nil
}
func (f *fakeCodexChannel) Close() error { f.closed = true; return nil }
func TestCodexAppServerTransportUsesFixedReadOnlyHandshake(t *testing.T) {
	ch := &fakeCodexChannel{frames: [][]byte{[]byte(`{"id":1,"result":{"protocolVersion":"v2","capabilities":{}}}`), []byte(`{"id":2,"result":{"rateLimitsByLimitId":{"codex":{"limitId":"codex","primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":1792700000}}},"account":{"email":"private"}}}`)}}
	tr := CodexAppServerTransport{Factory: CodexChannelFactoryFunc(func(context.Context) (CodexChannel, error) { return ch, nil })}
	got, err := tr.Request(context.Background(), "account/rateLimits/read")
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.sent) != 3 || !ch.closed {
		t.Fatalf("expected initialize, initialized, read; sent=%d closed=%v", len(ch.sent), ch.closed)
	}
	var methods []string
	for _, raw := range ch.sent {
		var m struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(raw, &m)
		methods = append(methods, m.Method)
	}
	if strings.Join(methods, ",") != "initialize,initialized,account/rateLimits/read" {
		t.Fatalf("unexpected RPC methods %v", methods)
	}
	if strings.Contains(string(got), "private") || !strings.Contains(string(got), "rateLimitsByLimitId") {
		t.Fatalf("response was not reduced to quota fields: %s", got)
	}
	if _, err = tr.Request(context.Background(), "account/read"); err == nil {
		t.Fatal("arbitrary app-server method accepted")
	}
}
func TestCodexAppServerTransportTimeoutAndFrameBudget(t *testing.T) {
	waiting := &fakeCodexChannel{wait: true}
	tr := CodexAppServerTransport{Factory: CodexChannelFactoryFunc(func(context.Context) (CodexChannel, error) { return waiting, nil })}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tr.Request(ctx, "account/rateLimits/read"); err == nil {
		t.Fatal("canceled request accepted")
	}
	blocked := &fakeCodexChannel{frames: [][]byte{[]byte(`{"id":1,"result":{"protocolVersion":"v2"}}`)}, wait: true}
	tr.Factory = CodexChannelFactoryFunc(func(context.Context) (CodexChannel, error) { return blocked, nil })
	short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if _, err := tr.Request(short, "account/rateLimits/read"); err == nil || !blocked.closed {
		t.Fatal("blocked stream did not cancel and close")
	}
	bad := &fakeCodexChannel{frames: [][]byte{[]byte(`{"id":1,"result":{}}`), []byte(`{"id":2,"result":{"rateLimits":{}}}`)}}
	tr.Factory = CodexChannelFactoryFunc(func(context.Context) (CodexChannel, error) { return bad, nil })
	if _, err := tr.Request(context.Background(), "account/rateLimits/read"); err == nil {
		t.Fatal("incomplete quota data accepted")
	}
}

func TestCodexAppServerTransportRejectsDuplicateJSONKeys(t *testing.T) {
	ch := &fakeCodexChannel{frames: [][]byte{
		[]byte(`{"id":1,"result":{"protocolVersion":"v2"}}`),
		[]byte(`{"id":2,"result":{"rateLimits":{},"rateLimits":{"primary":{}}}}`),
	}}
	tr := CodexAppServerTransport{Factory: CodexChannelFactoryFunc(func(context.Context) (CodexChannel, error) { return ch, nil })}
	if _, err := tr.Request(context.Background(), "account/rateLimits/read"); err == nil {
		t.Fatal("duplicate JSON keys accepted")
	}
}
