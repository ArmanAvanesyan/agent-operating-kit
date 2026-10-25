package tracker

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

func compatConvert[T any](v any) T {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	var t T
	if e = json.Unmarshal(b, &t); e != nil {
		panic(e)
	}
	return t
}
func fromReadCapture(c *readsource.Capture, e error) (*Capture, error) {
	if e != nil {
		return nil, e
	}
	out := &Capture{Snapshot: compatConvert[Snapshot](c.Snapshot), Files: c.Files}
	out.seal = captureSeal(out)
	return out, nil
}
func githubRepo(s Source) (string, string, error)   { return readsource.SharedGithubRepo(s) }
func validAzureID(id string) bool                   { return readsource.SharedValidAzureID(id) }
func workflowVersion(s Source) string               { return readsource.SharedWorkflowVersion(s) }
func adfText(v any) (string, bool)                  { return readsource.SharedAdfText(v) }
func ghGraph(k, q string, v map[string]any) Request { return readsource.SharedGhGraph(k, q, v) }
func githubProjectsRequest(o, r, a string) Request {
	return readsource.SharedGithubProjectsRequest(o, r, a)
}

const ghProjectFields = readsource.GhProjectFields

func marshalRaw(v any) json.RawMessage            { return readsource.SharedMarshalRaw(v) }
func azureScope(s Source) (string, string, error) { return readsource.SharedAzureScope(s) }
func adoPath(o, p, t string) string               { return readsource.SharedAdoPath(o, p, t) }
func dcSearch(s Source, n int) Request            { return readsource.SharedDcSearch(s, n) }

type projectFieldValue = readsource.ProjectFieldValue
