package sdk

import (
	"encoding/hex"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"strings"
	"time"
)

func readDigestOrEmpty(s string) bool {
	if s == "" {
		return true
	}
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func readParentEvidence(b *ReadBundle) map[string][]byte {
	out := map[string][]byte{}
	if b == nil {
		return out
	}
	for name, raw := range b.lineage {
		out[name] = raw
	}
	prefix := "assets/lineage/" + b.digest + "/"
	out[prefix+"bundle.json"] = b.manifestRaw
	out[prefix+"source.json"] = b.sourceRaw
	out[prefix+"policy.json"] = b.policyRaw
	out[prefix+"enrollment.json"] = b.enrollmentRaw
	return out
}
func validateReadLineage(config []byte, m r.Manifest, source readsource.Manifest, policy, enrollment []byte, assets map[string][]byte) error {
	if len(assets) > 64 {
		return errors.New("lineage hop bound exceeded")
	}
	used := map[string]bool{}
	seen := map[string]bool{}
	var visit func(r.Manifest, readsource.Manifest, []byte, []byte, int) (map[string]bool, error)
	visit = func(manifest r.Manifest, src readsource.Manifest, p, en []byte, depth int) (map[string]bool, error) {
		if depth > 16 {
			return nil, errors.New("lineage depth bound exceeded")
		}
		var grant r.ReadEnrollment
		if DecodeRead(en, &grant) != nil {
			return nil, errors.New("invalid lineage enrollment")
		}
		issued, e := time.Parse(time.RFC3339Nano, grant.IssuedAt)
		if e != nil {
			return nil, e
		}
		until, _ := time.Parse(time.RFC3339Nano, grant.ValidUntil)
		if _, _, _, e = validateReadEnrollment(config, p, en, issued, false); e != nil {
			return nil, e
		}
		if grant.ImplementationSHA256 != manifest.ImplementationSHA256 || grant.Action != manifest.Mode || grant.PreviousBundleSHA256 != manifest.PreviousBundleSHA256 || grant.ReadPolicySHA256 != manifest.ReadPolicySHA256 || readsource.Digest(grant.Transport) != readsource.Digest(manifest.Transport) {
			return nil, errors.New("lineage enrollment/input binding mismatch")
		}
		inherited := map[string]bool{}
		if parent := manifest.PreviousBundleSHA256; parent != "" {
			if !readDigestOrEmpty(parent) || seen[parent] {
				return nil, errors.New("lineage identity/cycle mismatch")
			}
			seen[parent] = true
			prefix := "assets/lineage/" + parent + "/"
			for _, part := range []string{"bundle.json", "source.json", "policy.json", "enrollment.json"} {
				if len(assets[prefix+part]) == 0 {
					return nil, errors.New("missing lineage asset")
				}
				used[prefix+part] = true
			}
			parentRaw := assets[prefix+"bundle.json"]
			if SHA256(parentRaw) != parent {
				return nil, errors.New("parent bundle digest mismatch")
			}
			var previous r.Manifest
			var priorSource readsource.Manifest
			if DecodeRead(parentRaw, &previous) != nil || readsource.Decode(assets[prefix+"source.json"], "manifest", &priorSource) != nil || previous.Version != r.Version || previous.AdapterVersion != r.AdapterVersion || previous.ConfigurationSHA256 != SHA256(config) || previous.SourceSHA256 != SHA256(assets[prefix+"source.json"]) || previous.ReadPolicySHA256 != SHA256(assets[prefix+"policy.json"]) || previous.AuthorizationGranted || previous.ExecutionReady || previous.ProductionActivated || previous.MigrationActivated || previous.LiveQualification != "not-established" || readsource.Digest(previous.Transport) != readsource.Digest(manifest.Transport) || readsource.Digest(priorSource.Source) != readsource.Digest(src.Source) {
				return nil, errors.New("invalid parent read evidence")
			}
			pins := map[string]string{}
			for _, a := range previous.Assets {
				pins[a.Path] = a.SHA256
			}
			if pins["assets/read-policy.json"] != SHA256(assets[prefix+"policy.json"]) || pins["assets/enrollment.json"] != SHA256(assets[prefix+"enrollment.json"]) || pins["assets/source/manifest.json"] != SHA256(assets[prefix+"source.json"]) {
				return nil, errors.New("parent proof assets not pinned")
			}
			var err error
			inherited, err = visit(previous, priorSource, assets[prefix+"policy.json"], assets[prefix+"enrollment.json"], depth+1)
			if err != nil {
				return nil, err
			}
		}
		pages := map[string]bool{}
		for _, page := range src.Pages {
			if page.Status < 200 || page.Status >= 300 {
				continue
			}
			key := SHA256(encodedRead(page))
			obs, e := time.Parse(time.RFC3339Nano, page.ObservedAt)
			if e != nil || !inherited[key] && (obs.Before(issued) || !obs.Before(until)) {
				return nil, errors.New("source observation outside originating signed read interval")
			}
			pages[key] = true
		}
		return pages, nil
	}
	_, e := visit(m, source, policy, enrollment, 0)
	if e != nil {
		return e
	}
	if len(used) != len(assets) {
		return errors.New("unreferenced lineage asset")
	}
	return nil
}
