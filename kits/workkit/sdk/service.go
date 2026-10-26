// Package sdk exposes WorkKit application services. It imports no AOK legacy
// internals, credential readers or SQLite stores; live transports are injected.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"

	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
)

const Version = "0.4.0-setup-offline"

type Error struct {
	Code, Message string
	ExitCode      int
	cause         error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }
func failure(code, message string, exit int, e error) error {
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return &Error{"context.cancelled", "offline operation cancelled", 1, e}
	}
	return &Error{code, message, exit, e}
}

type Document struct{ document *engine.Document }

func Parse(b []byte, format string) (Document, error) {
	d, e := engine.Parse(b, format)
	if e != nil {
		return Document{}, failure("document.invalid", "document failed strict parsing or validation", 2, e)
	}
	return Document{d}, nil
}
func (d Document) Bytes() []byte          { return append([]byte(nil), d.document.Raw...) }
func (d Document) CanonicalBytes() []byte { return append([]byte(nil), d.document.Canonical...) }
func (d Document) SHA256() string         { return engine.Hash(d.document.Raw) }
func (d Document) ContentSHA256() string  { return engine.Hash(d.document.Canonical) }
func (d Document) Kind() string           { return d.document.Kind }
func (d Document) Decode(target any) error {
	decoder := json.NewDecoder(bytes.NewReader(d.document.Canonical))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

type WorkspaceRequest struct{ Workspace, Root string }
type ResourceSummary struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Revision      string `json:"revision"`
	SHA256        string `json:"sha256"`
	ContentSHA256 string `json:"contentSha256"`
}
type WorkspaceReport struct {
	Resources            []ResourceSummary `json:"resources"`
	AssetCount           int               `json:"assetCount"`
	AuthorizationGranted bool              `json:"authorizationGranted"`
	ExecutionReady       bool              `json:"executionReady"`
	LiveQualification    string            `json:"liveQualification"`
}
type Service struct {
	legacy      LegacyReader
	reads       ReadBackend
	access      AccessBackend
	setupReader SetupMetadataReader
	setupTrust  SetupTrust
}

func New(legacy LegacyReader) *Service { return &Service{legacy: legacy} }
func (s *Service) Resolve(ctx context.Context, r WorkspaceRequest) (WorkspaceReport, error) {
	snapshot, e := engine.Resolve(ctx, r.Workspace, r.Root)
	if e != nil {
		exit := 2
		switch e.Error() {
		case "missing referenced resource", "missing operation dependency":
			exit = 3
		case "duplicate resource identity", "reference byte hash mismatch", "asset hash mismatch", "duplicate operation ID", "resource changed during resolution", "asset changed during resolution":
			exit = 4
		}
		return WorkspaceReport{}, failure("workspace.invalid", "workspace references, identities or assets could not be validated", exit, e)
	}
	report := WorkspaceReport{Resources: []ResourceSummary{}, AssetCount: len(snapshot.Assets), LiveQualification: "not-established"}
	for _, d := range snapshot.Documents {
		report.Resources = append(report.Resources, ResourceSummary{d.Kind, d.ID, string(d.Revision), engine.Hash(d.Raw), engine.Hash(d.Canonical)})
	}
	sort.Slice(report.Resources, func(i, j int) bool {
		return report.Resources[i].Kind+"/"+report.Resources[i].ID+"/"+report.Resources[i].Revision < report.Resources[j].Kind+"/"+report.Resources[j].ID+"/"+report.Resources[j].Revision
	})
	return report, nil
}
func ReadLocal(ctx context.Context, p string) ([]byte, error) { return engine.ReadFile(ctx, p) }
func ConfinedLocal(root, rel string) (string, error)          { return engine.Confined(root, rel) }
func SHA256(b []byte) string                                  { return engine.Hash(b) }
func Result(command, status string, data any, diagnostics []api.Diagnostic, localEffect string) api.CommandResult {
	if diagnostics == nil {
		diagnostics = []api.Diagnostic{}
	}
	return api.CommandResult{APIVersion: api.Version, Kind: "CommandResult", Metadata: api.Metadata{Id: "command-result", Revision: 1}, Spec: api.CommandResultSpec{Command: command, Status: status, Data: json.RawMessage(engine.Encode(data)), Diagnostics: diagnostics, Effects: api.CommandResultSpecEffects{Local: localEffect, Provider: "none", Native: "none"}, OperationIds: []string{}, NextActions: []string{}}}

}

// SuiteResult is a rendering projection; domain validation remains unchanged.
func SuiteResult(result api.CommandResult, exit int) any {
	effect := "none"
	for _, e := range []string{result.Spec.Effects.Local, result.Spec.Effects.Provider, result.Spec.Effects.Native} {
		if e == "possible" {
			effect = "possible"
			break
		}
		if e == "confirmed" {
			effect = "confirmed"
		}
	}
	return map[string]any{"apiVersion": "aok-suite/v1alpha1", "kind": "CommandResult", "metadata": map[string]any{"id": "workkit-command-result", "revision": 1}, "spec": map[string]any{"kitId": "workkit", "command": result.Spec.Command, "status": result.Spec.Status, "domainExitCode": exit, "data": result, "diagnostics": result.Spec.Diagnostics, "effects": effect}}
}
