package sdk

import (
	"context"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"path/filepath"
	"time"
)

var operationQualificationRequired = []string{"operator-scope", "credential-reference", "read-identity", "verified-binding", "operation-capability", "protected-authority", "anti-rollback-witness", "one-use-grant", "provider-receipt", "independent-readback"}

// OperationQualificationTemplate prepares only requirements. It cannot qualify a
// route by presence of fixture files or credentials.
func OperationQualificationTemplate() o.Qualification {
	return o.Qualification{Version: o.Version, Kind: "OperationQualification", ImplementationSHA256: OperationsImplementationSHA256(), Required: append([]string{}, operationQualificationRequired...), Provided: map[string]api.Asset{}, Status: "pending", Diagnostics: []api.Diagnostic{}}
}
func ValidateOperationQualification(ctx context.Context, path, asOf string) (o.Qualification, error) {
	raw, err := ReadLocal(ctx, path)
	if err != nil {
		return o.Qualification{}, err
	}
	var q o.Qualification
	if DecodeRead(raw, &q) != nil || q.Version != o.Version || q.Kind != "OperationQualification" || q.ImplementationSHA256 != OperationsImplementationSHA256() || q.AuthorizationGranted || q.ExecutionReady {
		return q, errors.New("invalid qualification preparation")
	}
	if _, err = time.Parse(time.RFC3339Nano, asOf); err != nil {
		return q, err
	}
	q.Status = "pending"
	q.Required = append([]string{}, operationQualificationRequired...)
	q.Diagnostics = []api.Diagnostic{}
	for _, name := range q.Required {
		asset, ok := q.Provided[name]
		if !ok {
			q.Diagnostics = append(q.Diagnostics, api.Diagnostic{Code: "qualification.missing", Field: name, Message: "externally issued evidence required", Severity: "error"})
			continue
		}
		p, e := ConfinedLocal(filepath.Dir(path), asset.Path)
		if e != nil {
			return q, e
		}
		b, e := ReadLocal(ctx, p)
		if e != nil || SHA256(b) != string(asset.Sha256) {
			return q, errors.New("qualification asset mismatch")
		}
	}
	q.Diagnostics = append(q.Diagnostics, api.Diagnostic{Code: "qualification.independent-validation", Field: "route", Message: "presence/integrity is not independent live qualification; protected issuer validation remains required", Severity: "warning"})
	return q, nil
}
