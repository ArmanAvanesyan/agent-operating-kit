package hostbridge

import (
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"path/filepath"
	"time"
)

func catalogCompatibility(profile contracts.ExecutionProfile, profileHash string, p Policy) error {
	if p.Catalog == "" && p.CatalogPolicy == nil {
		return nil
	}
	if p.Catalog == "" || p.CatalogPolicy == nil {
		return fmt.Errorf("catalog and external policy required together")
	}
	if _, e := assetBytes(p.root, *p.CatalogPolicy); e != nil {
		return e
	}
	asOf, e := time.Parse(time.RFC3339Nano, p.CatalogAsOf)
	if e != nil {
		return fmt.Errorf("catalog compatibility requires explicit RFC3339 as-of")
	}
	c, report := modelcatalog.Load(p.Catalog, filepath.Join(p.root, p.CatalogPolicy.Path))
	if c == nil || report.Status != "valid" {
		return fmt.Errorf("catalog inputs invalid")
	}
	r := modelcatalog.CheckWithProfileDigest(c, profile, profileHash, asOf)
	if r.Compatibility == "unsupported" || r.Report.Status == "invalid" {
		return fmt.Errorf("catalog explicitly incompatible")
	}
	return nil
}

func catalogDigest(p Policy) (string, error) {
	if p.Catalog == "" && p.CatalogPolicy == nil {
		return "", nil
	}
	if p.CatalogPolicy == nil || p.Catalog == "" {
		return "", fmt.Errorf("catalog and policy required together")
	}
	if _, err := assetBytes(p.root, *p.CatalogPolicy); err != nil {
		return "", err
	}
	c, report := modelcatalog.Load(p.Catalog, filepath.Join(p.root, p.CatalogPolicy.Path))
	if c == nil || report.Status != "valid" {
		return "", fmt.Errorf("catalog inputs invalid")
	}
	return Digest(struct {
		Catalog string
		Policy  string
	}{c.CatalogSHA256, c.PolicySHA256}), nil
}
