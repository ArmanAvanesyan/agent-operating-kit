package sdk

import (
	"context"
	"encoding/base64"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/auth"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"sort"
	"time"
)

// ChangeConnections creates a new immutable configuration version. It does not
// move a workspace pointer, revoke credentials, or activate a binding/sender.
func ChangeConnections(ctx context.Context, old, config []byte, expected, id, action, asOf, out string) (a.ConnectionRegistry, error) {
	r := a.ConnectionRegistry{Version: a.Version, Kind: "ConnectionRegistry", Connections: []a.ConnectionEntry{}}
	if len(old) > 0 {
		if SHA256(old) != expected || DecodeRead(old, &r) != nil || r.Version != a.Version || r.Kind != "ConnectionRegistry" || r.Revision < 1 || r.MigrationActivated || r.ProductionActivated {
			return a.ConnectionRegistry{}, failure("registry.conflict", "registry revision/hash mismatch", 4, nil)
		}
	} else if expected != "" {
		return r, failure("registry.conflict", "missing prior registry", 4, nil)
	}
	at, e := time.Parse(time.RFC3339Nano, asOf)
	if e != nil {
		return r, failure("time.invalid", "explicit as-of required", 2, e)
	}
	entries := map[string]a.ConnectionEntry{}
	for _, entry := range r.Connections {
		original, decodeErr := base64.StdEncoding.DecodeString(entry.ConfigurationBase64)
		if decodeErr != nil {
			return r, errors.New("registry config encoding")
		}
		if c, validationErr := ValidateAuthConfiguration(original); validationErr != nil || c.ConnectionID != entry.ID || !auth.ID.MatchString(entry.ID) {
			return r, errors.New("registry identity mismatch")
		}
		if _, e = ValidateAuthConfiguration(original); e != nil || entry.ConfigurationSHA256 != SHA256(original) || entries[entry.ID].ID != "" {
			return r, errors.New("invalid registry entry")
		}
		entries[entry.ID] = entry
	}
	switch action {
	case "add":
		c, e := ValidateAuthConfiguration(config)
		if e != nil {
			return r, e
		}
		if c.ConnectionID != id {
			return r, errors.New("connection identity mismatch")
		}
		if _, exists := entries[id]; exists {
			return r, failure("connection.conflict", "connection already configured", 4, nil)
		}
		entries[id] = a.ConnectionEntry{ID: id, ConfigurationSHA256: SHA256(config), ConfigurationBase64: base64.StdEncoding.EncodeToString(config)}
	case "remove":
		if _, exists := entries[id]; !exists {
			return r, failure("connection.missing", "connection absent", 4, nil)
		}
		delete(entries, id)
	default:
		return r, errors.New("unsupported registry operation")
	}
	if len(entries) > 128 {
		return r, failure("registry.limit", "connection count bound exceeded", 2, nil)
	}
	r.Revision++
	r.PreviousSHA256 = SHA256(old)
	if len(old) == 0 {
		r.PreviousSHA256 = ""
	}
	r.ObservedAt = at.UTC().Format(time.RFC3339Nano)
	r.Connections = []a.ConnectionEntry{}
	for _, entry := range entries {
		r.Connections = append(r.Connections, entry)
	}
	sort.Slice(r.Connections, func(i, j int) bool { return r.Connections[i].ID < r.Connections[j].ID })
	encoded := engine.Encode(r)
	if len(encoded) > engine.MaxDocumentBytes {
		return r, failure("registry.limit", "encoded registry exceeds bounded reader", 2, nil)
	}
	e = engine.WriteBundle(ctx, out, map[string][]byte{"registry.json": encoded}, "")
	return r, e
}

func InspectConnections(raw []byte) (a.ConnectionRegistry, error) {
	var registry a.ConnectionRegistry
	if e := DecodeRead(raw, &registry); e != nil {
		return registry, e
	}
	if registry.Version != a.Version || registry.Kind != "ConnectionRegistry" || registry.Revision < 1 || registry.MigrationActivated || registry.ProductionActivated {
		return a.ConnectionRegistry{}, errors.New("invalid registry")
	}
	seen := map[string]bool{}
	for _, entry := range registry.Connections {
		b, e := base64.StdEncoding.DecodeString(entry.ConfigurationBase64)
		if e != nil || SHA256(b) != entry.ConfigurationSHA256 {
			return registry, errors.New("registry hash mismatch")
		}
		c, e := ValidateAuthConfiguration(b)
		if e != nil || c.ConnectionID != entry.ID || seen[entry.ID] {
			return registry, errors.New("registry identity mismatch")
		}
		seen[entry.ID] = true
	}
	return registry, nil
}
