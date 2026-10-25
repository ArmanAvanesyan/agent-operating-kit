package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"path/filepath"
	"reflect"
	"strings"
)

// DecodeRead validates syntax and rejects unknown structural fields without JCS
// conversion: raw provider decimals must retain their original exact value.
func DecodeRead(b []byte, v any) error {
	value, e := engine.JSONValue(b)
	if e != nil {
		return e
	}
	if e = requireReadFields(value, reflect.TypeOf(v)); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func LoadReadConfiguration(ctx context.Context, p string) (r.Configuration, []byte, error) {
	var c r.Configuration
	b, e := ReadLocal(ctx, p)
	if e != nil {
		return c, nil, e
	}
	e = DecodeRead(b, &c)
	if e == nil {
		e = validateReadConfiguration(c)
	}
	return c, b, e
}
func validateReadConfiguration(c r.Configuration) error {
	if !readDigestOrEmpty(c.Source.ReadPolicySHA256) || c.Version != r.Version || c.ConnectionID == "" {
		return errors.New("unsupported read configuration")
	}
	if e := readsource.ValidateSource(c.Source); e != nil {
		return e
	}
	adapter, e := readsource.Provider(c.Source.Provider)
	if e != nil {
		return e
	}
	if _, e = adapter.Initial(c.Source, "capture", nil); e != nil {
		return e
	}
	d, e := Parse(c.Profile, "json")
	if e != nil || d.Kind() != "ConnectionProfile" {
		return errors.New("exact ConnectionProfile required")
	}
	var p map[string]any
	if e = DecodeRead(d.CanonicalBytes(), &p); e != nil {
		return e
	}
	spec := p["spec"].(map[string]any)
	meta := p["metadata"].(map[string]any)
	scope := spec["scope"].(map[string]any)
	provider := c.Source.Provider
	if provider == "jira" {
		provider = "jira-cloud"
	}
	base := ""
	if c.Source.Deployment != nil {
		base = c.Source.Deployment.BasePath
	}
	if meta["id"] != c.ConnectionID || spec["provider"] != provider || spec["origin"] != c.Source.Origin || spec["basePath"] != base || spec["apiVersion"] != c.Source.APIVersion || scope["tenantId"] != c.Source.NativeTenantID {
		return errors.New("profile/source identity mismatch")
	}
	ids := scope["containerIds"].([]any)
	if len(ids) != 1 || ids[0] != c.Source.ContainerID {
		return errors.New("exact single-container read scope required")
	}
	limits, _ := readsource.EffectiveLimits(c.Source)
	if declared, ok := spec["limits"].(map[string]any); ok {
		bounds := map[string]int{"timeoutSeconds": limits.RequestSeconds, "maxItems": limits.Items, "maxBytes": limits.TotalBytes}
		for name, actual := range bounds {
			n, ok := declared[name].(json.Number)
			if !ok {
				return errors.New("invalid profile limit")
			}
			maximum, e := n.Int64()
			if e != nil || int64(actual) > maximum {
				return errors.New("source exceeds exact profile limits")
			}
		}
	}
	if c.Source.Deployment != nil {
		dep, ok := spec["deployment"].(map[string]any)
		if !ok {
			return errors.New("missing deployment profile")
		}
		trust, _ := dep["tlsTrust"].(map[string]any)
		if dep["serverVersion"] != c.Source.Deployment.ServerVersion || trust["sha256"] != c.Source.Deployment.TLSTrustSHA256 {
			return errors.New("deployment profile/source pin mismatch")
		}
	}
	if c.Source.Auth.Mode == "basic" && c.Source.Auth.TokenEnv == c.Source.Auth.UsernameEnv {
		return errors.New("token and principal references must be distinct")
	}
	transport := spec["transport"]
	if c.Source.Provider == "github" && transport != "gh" || c.Source.Provider == "linear" && transport != "graphql" || c.Source.Provider != "github" && c.Source.Provider != "linear" && transport != "rest" {
		return errors.New("unimplemented read transport")
	}
	return nil
}
func importSource(ctx context.Context, path string) (*readsource.Capture, error) {
	b, e := ReadLocal(ctx, path)
	if e != nil {
		return nil, e
	}
	var m readsource.Manifest
	if e = readsource.Decode(b, "manifest", &m); e != nil {
		return nil, e
	}
	if len(m.Pages) > 1000 {
		return nil, errors.New("source page limit")
	}
	assets := map[string][]byte{}
	total := len(b)
	for _, p := range m.Pages {
		a, e := ConfinedLocal(filepath.Dir(path), p.Asset.Path)
		if e != nil {
			return nil, e
		}
		raw, e := ReadLocal(ctx, a)
		if e != nil {
			return nil, e
		}
		total += len(raw)
		if total > engine.MaxTotalBytes || SHA256(raw) != p.Asset.SHA256 {
			return nil, errors.New("source asset integrity/budget failure")
		}
		if old, ok := assets[p.Asset.Path]; ok && !bytes.Equal(old, raw) {
			return nil, errors.New("source collision")
		}
		assets[p.Asset.Path] = raw
	}
	c, e := readsource.ReconstructBytes(b, assets)
	if e != nil {
		return nil, e
	}
	again, e := ReadLocal(ctx, path)
	if e != nil || !bytes.Equal(again, b) {
		return nil, errors.New("source changed during capture import")
	}
	for name, raw := range assets {
		p, e := ConfinedLocal(filepath.Dir(path), name)
		if e != nil {
			return nil, e
		}
		now, e := ReadLocal(ctx, p)
		if e != nil || !bytes.Equal(raw, now) {
			return nil, errors.New("source asset changed")
		}
	}
	return c, nil
}
func readFailure(e error) error {
	if e == nil {
		return nil
	}
	var known *Error
	if errors.As(e, &known) {
		return e
	}
	return failure("read.invalid", "read inputs, scope, assets or capture could not be validated", 2, e)
}
func providerName(s string) string {
	if s == "jira" {
		return "jira-cloud"
	}
	return s
}
func NativeIdentity(c r.Configuration, i readsource.Item) r.Identity {
	base := ""
	if c.Source.Deployment != nil {
		base = c.Source.Deployment.BasePath
	}
	return r.Identity{providerName(c.Source.Provider), c.Source.Origin, base, c.Source.NativeTenantID, c.Source.ContainerID, i.Identity.ItemKind, i.Identity.RemoteID}
}
func zeroDigest(s string) bool { return s == strings.Repeat("0", 64) }

func requireReadFields(v any, t reflect.Type) error {
	if t == nil {
		return errors.New("typed decode target required")
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	if v == nil && t.Kind() != reflect.Pointer && t.Kind() != reflect.Map && t.Kind() != reflect.Slice && t.Kind() != reflect.Interface {
		return errors.New("null for nonnullable read field")
	}
	for t.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("record object required")
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			parts := strings.Split(f.Tag.Get("json"), ",")
			if parts[0] == "-" {
				continue
			}
			name := parts[0]
			if name == "" {
				name = f.Name
			}
			child, ok := m[name]
			optional := len(parts) > 1 && parts[1] == "omitempty"
			if !ok && !optional {
				return errors.New("required read record field missing")
			}
			if ok {
				if e := requireReadFields(child, f.Type); e != nil {
					return e
				}
			}
		}
	case reflect.Slice:
		if rows, ok := v.([]any); ok {
			for _, row := range rows {
				if e := requireReadFields(row, t.Elem()); e != nil {
					return e
				}
			}
		}
	case reflect.Map:
		if m, ok := v.(map[string]any); ok {
			for _, child := range m {
				if e := requireReadFields(child, t.Elem()); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
