package tracker

import (
	"context"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

func Provider(n string) (Adapter, error) {
	switch n {
	case "github":
		return NewGitHub(), nil
	case "azure-devops":
		return NewAzure(), nil
	case "linear":
		return NewLinear(), nil
	case "jira":
		return NewJira(), nil
	case "jira-datacenter":
		return JiraDataCenter{}, nil
	}
	_, e := readsource.Provider(n)
	return nil, e
}
func ScopeDigest(s Source) string                     { return readsource.ScopeDigest(s) }
func ValidateCheckpoint(c Checkpoint, s Source) error { return readsource.ValidateCheckpoint(c, s) }
func finishCheckpoint(c Checkpoint) Checkpoint        { return readsource.SharedFinishCheckpoint(c) }
func Import(p string) (*Capture, error)               { return fromReadCapture(readsource.Import(p)) }
func Load(p string) (*Capture, error)                 { return fromReadCapture(readsource.Load(p)) }
func CaptureSource(ctx context.Context, s Source, m string, c *Checkpoint, t Transport) (*Capture, error) {
	return fromReadCapture(readsource.CaptureSource(ctx, s, m, c, t))
}
func CaptureWithCheckpoint(ctx context.Context, s Source, m, p string, t Transport) (*Capture, error) {
	return fromReadCapture(readsource.CaptureWithCheckpoint(ctx, s, m, p, t))
}
func build(m Manifest, a map[string][]byte) (*Capture, error) {
	return fromReadCapture(readsource.SharedBuild(compatConvert[readsource.Manifest](m), a))
}
func captureSeal(c *Capture) string {
	return Digest(struct {
		Snapshot Snapshot
		Files    map[string][]byte
	}{c.Snapshot, c.Files})
}
func ValidCapture(c *Capture) bool   { return c != nil && c.seal != "" && c.seal == captureSeal(c) }
func statusDisposition(s int) string { return readsource.SharedStatusDisposition(s) }
func sanitizeHeaders(h map[string]string) map[string]string {
	return readsource.SharedSanitizeHeaders(h)
}
func safeHeaders(h map[string]string) bool { return readsource.SharedSafeHeaders(h) }
