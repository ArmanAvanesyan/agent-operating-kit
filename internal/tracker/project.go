package tracker

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sort"
	"strings"
)

//go:embed schemas/task-packet.json
var packetSchema embed.FS

type Projection struct {
	Report   Report                      `json:"report"`
	Packets  map[string]json.RawMessage  `json:"packets"`
	Bindings []contracts.WorkItemBinding `json:"bindings"`
	Files    map[string][]byte           `json:"-"`
}

func ReadMapping(path string) (Mapping, error) {
	var m Mapping
	b, e := ReadFile(path)
	if e != nil {
		return m, e
	}
	if e = Decode(b, "mapping", &m); e != nil {
		return m, e
	}
	return m, validateMapping(m)
}
func validateMapping(m Mapping) error {
	if (m.Version != Version && m.Version != Version2) || m.ID == "" || m.MappingVersion == "" || len(m.Rules) == 0 {
		return fmt.Errorf("invalid explicit mapping")
	}
	seen := map[string]bool{}
	for _, r := range m.Rules {
		if r.PacketField == "" || seen[r.PacketField] || r.Owner != "human" && r.Owner != "aok" && r.Owner != "provider" {
			return fmt.Errorf("invalid or duplicate field mapping")
		}
		seen[r.PacketField] = true
		if (r.ProviderFieldID == "") == (r.Literal == nil) {
			return fmt.Errorf("mapping must specify exactly one native field or explicit literal")
		}
		if r.Literal != nil && len(r.ValueMap) > 0 {
			return fmt.Errorf("literal rules cannot remap provider values")
		}
	}
	return nil
}
func Project(c *Capture, m Mapping) (Projection, error) {
	out := Projection{Report: BaseReport(), Packets: map[string]json.RawMessage{}, Bindings: []contracts.WorkItemBinding{}, Files: map[string][]byte{}}
	if !ValidCapture(c) {
		return out, fmt.Errorf("unvalidated or mutated capture")
	}
	if e := validateMapping(m); e != nil {
		return out, e
	}
	out.Report.Version = c.Snapshot.Version
	if m.Version != c.Snapshot.Version || m.Provider != c.Snapshot.Source.Provider || m.MappingVersion != c.Snapshot.Source.MappingVersion {
		return out, fmt.Errorf("mapping provider/version mismatch")
	}
	blocked := func(id, msg string) {
		out.Report.Status = "blocked"
		out.Report.Diagnostics = append(out.Report.Diagnostics, Diagnostic{"packet-incomplete", id, msg})
	}
	if !c.Snapshot.Complete {
		blocked("snapshot", "source capture is incomplete")
	}
	if len(c.Snapshot.Items) == 0 {
		blocked("items", "no work items available")
	}
	for _, item := range c.Snapshot.Items {
		packet := map[string]any{}
		invalid := false
		fields := []contracts.FieldMapping{}
		if !item.Complete || item.Revision == "" {
			invalid = true
			blocked(item.Identity.RemoteID, "work item is incomplete or lacks an observed revision")
		}
		for _, rule := range m.Rules {
			raw := json.RawMessage(nil)
			if rule.Literal != nil {
				raw = *rule.Literal
			} else {
				raw, _ = mappedNativeField(item, rule.ProviderFieldID)
				for _, lossy := range item.LossyFields {
					if lossy == rule.ProviderFieldID {
						invalid = true
						blocked(rule.PacketField, "mapped native field has an unsupported or lossy conversion")
					}
				}
				if len(raw) == 0 {
					invalid = true
					blocked(rule.PacketField, "required native field was not captured")
					continue
				}
				if len(item.Provenance[rule.ProviderFieldID]) == 0 {
					invalid = true
					blocked(rule.PacketField, "field provenance is unavailable")
				}
			}
			if len(rule.ValueMap) > 0 {
				var key string
				if usage.Decode(raw, &key) != nil {
					key = semantic(raw)
				}
				mapped, ok := rule.ValueMap[key]
				if !ok {
					invalid = true
					blocked(rule.PacketField, "native value has no explicit semantic mapping")
					continue
				}
				raw = mapped
			}
			var v any
			if e := usage.Decode(raw, &v); e != nil {
				return out, fmt.Errorf("invalid mapping value")
			}
			if e := setPacket(packet, rule.PacketField, v); e != nil {
				return out, e
			}
			if rule.ProviderFieldID != "" {
				owner := rule.Owner
				if owner == "human" || owner == "provider" {
					owner = "tracker"
				}
				fields = append(fields, contracts.FieldMapping{CanonicalField: rule.PacketField, ProviderFieldID: rule.ProviderFieldID, Owner: owner, Supported: true})
			}
		}
		data, e := json.MarshalIndent(packet, "", "  ")
		if e != nil {
			return out, e
		}
		id := Digest(item.Identity)
		path := "packets/" + id + ".json"
		if e = validatePacket(data); e != nil {
			invalid = true
			blocked(item.Identity.RemoteID, "projection does not satisfy the existing task-packet schema")
		}
		if out.Report.Status != "valid" || invalid {
			out.Files["drafts/"+id+".json"] = append(data, '\n')
			continue
		}
		out.Packets[item.Identity.RemoteID] = data
		out.Files[path] = append(data, '\n')
		displayName := item.DisplayKey
		if displayName == "" {
			displayName = item.Identity.RemoteID
		}
		binding := contracts.WorkItemBinding{DocumentHeader: contracts.DocumentHeader{APIVersion: workflowVersion(c.Snapshot.Source), Kind: "WorkItemBinding", Metadata: contracts.OperationalMetadata{ID: "tracker-" + id[:20], Name: displayName, Version: "1.0.0", Description: "Scoped tracker snapshot binding; no execution authorization"}}, Identity: item.Identity, DisplayKey: item.DisplayKey, URL: item.URL, Packet: contracts.AssetRef{Path: path, SHA256: Hash(out.Files[path])}, ObservedSourceRevision: item.Revision, MappingVersion: m.MappingVersion, FieldMappings: fields}
		bb, _ := json.Marshal(binding)
		if _, _, e := contracts.Parse(bb); e != nil {
			return out, fmt.Errorf("binding schema: %w", e)
		}
		out.Bindings = append(out.Bindings, binding)
		out.Files["bindings/"+id+".json"] = append(bb, '\n')
	}
	// A mixed capture never emits a partially approved set of executable packets.
	if out.Report.Status != "valid" {
		out.Bindings = nil
		out.Packets = map[string]json.RawMessage{}
		for path, data := range out.Files {
			if strings.HasPrefix(path, "packets/") {
				delete(out.Files, path)
				out.Files[strings.Replace(path, "packets/", "drafts/", 1)] = data
			}
			if strings.HasPrefix(path, "bindings/") {
				delete(out.Files, path)
			}
		}
	}
	mappingBytes, _ := json.MarshalIndent(m, "", "  ")
	out.Files["mapping.json"] = append(mappingBytes, '\n')
	report, _ := json.MarshalIndent(out.Report, "", "  ")
	out.Files["projection-report.json"] = append(report, '\n')
	return out, nil
}
func setPacket(m map[string]any, path string, v any) error {
	parts := strings.Split(path, ".")
	if len(parts) > 4 {
		return fmt.Errorf("mapping field nesting exceeds limit")
	}
	cur := m
	for i, p := range parts {
		if p == "" || p == "__proto__" {
			return fmt.Errorf("invalid packet field")
		}
		if i == len(parts)-1 {
			if _, exists := cur[p]; exists {
				return fmt.Errorf("packet mappings overlap")
			}
			cur[p] = v
			return nil
		}
		child, exists := cur[p]
		if !exists {
			next := map[string]any{}
			cur[p] = next
			cur = next
		} else {
			next, ok := child.(map[string]any)
			if !ok {
				return fmt.Errorf("packet mappings overlap")
			}
			cur = next
		}
	}
	return nil
}
func validatePacket(b []byte) error {
	raw, _ := packetSchema.ReadFile("schemas/task-packet.json")
	var schema any
	if e := usage.Decode(raw, &schema); e != nil {
		return e
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denyLoader{})
	if e := compiler.AddResource("urn:aok:packet", schema); e != nil {
		return e
	}
	compiled, e := compiler.Compile("urn:aok:packet")
	if e != nil {
		return e
	}
	var v any
	if e = usage.Decode(b, &v); e != nil {
		return e
	}
	return compiled.Validate(v)
}

type Change struct {
	Identity contracts.RemoteIdentity `json:"identity"`
	Kind     string                   `json:"kind"`
	Fields   []string                 `json:"fields"`
}
type DiffReport struct {
	Report             Report   `json:"report"`
	Changes            []Change `json:"changes"`
	SourceBytesChanged bool     `json:"sourceBytesChanged"`
}

func Diff(a, b *Capture) (DiffReport, error) {
	out := DiffReport{Report: BaseReport(), Changes: []Change{}}
	if !ValidCapture(a) || !ValidCapture(b) {
		return out, fmt.Errorf("validated captures required")
	}
	if ScopeDigest(a.Snapshot.Source) != ScopeDigest(b.Snapshot.Source) {
		return out, fmt.Errorf("cannot diff different source scopes")
	}
	if !a.Snapshot.Complete || !b.Snapshot.Complete {
		out.Report.Status = "blocked"
		out.Report.Diagnostics = append(out.Report.Diagnostics, Diagnostic{"incomplete-diff", "snapshot", "absence and semantic changes are unknown until both requested captures complete"})
		out.SourceBytesChanged = sourceBytesIdentity(a.Snapshot.Pages) != sourceBytesIdentity(b.Snapshot.Pages)
		return out, nil
	}
	old := map[string]Item{}
	next := map[string]Item{}
	for _, i := range a.Snapshot.Items {
		old[i.Identity.RemoteID] = i
	}
	for _, i := range b.Snapshot.Items {
		next[i.Identity.RemoteID] = i
	}
	for id, item := range old {
		n, ok := next[id]
		if !ok {
			out.Changes = append(out.Changes, Change{item.Identity, "absent-from-snapshot", []string{}})
			continue
		}
		changed := []string{}
		for f, v := range item.Fields {
			if semantic(v) != semantic(n.Fields[f]) {
				changed = append(changed, f)
			}
		}
		for f := range n.Fields {
			if _, ok := item.Fields[f]; !ok {
				changed = append(changed, f)
			}
		}
		for f, v := range item.ProjectFields {
			if semantic(v) != semantic(n.ProjectFields[f]) {
				changed = append(changed, "projectFields."+f)
			}
		}
		for f := range n.ProjectFields {
			if _, ok := item.ProjectFields[f]; !ok {
				changed = append(changed, "projectFields."+f)
			}
		}
		sort.Strings(changed)
		kind := "changed"
		if len(changed) == 0 && Digest(item.Relations) == Digest(n.Relations) && Digest(item.Comments) == Digest(n.Comments) && semantic(marshalRaw(item.ProjectFields)) == semantic(marshalRaw(n.ProjectFields)) {
			continue
		}
		out.Changes = append(out.Changes, Change{item.Identity, kind, changed})
	}
	for id, item := range next {
		if _, ok := old[id]; !ok {
			out.Changes = append(out.Changes, Change{item.Identity, "added", []string{}})
		}
	}
	sort.Slice(out.Changes, func(i, j int) bool { return Digest(out.Changes[i].Identity) < Digest(out.Changes[j].Identity) })
	out.SourceBytesChanged = sourceBytesIdentity(a.Snapshot.Pages) != sourceBytesIdentity(b.Snapshot.Pages)
	return out, nil
}

// Observation dates and response headers describe capture context, not source bytes.
func sourceBytesIdentity(pages []Page) string {
	rows := map[string]string{}
	for _, p := range pages {
		rows[requestKey(p.Request)] = Digest(struct {
			Status      int
			AssetSHA256 string
		}{p.Status, p.Asset.SHA256})
	}
	return Digest(rows)
}
