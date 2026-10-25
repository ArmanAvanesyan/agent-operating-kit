package query

import (
	"encoding/json"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"strings"
	"testing"
)

func fixtureQuery(expr string) r.Query {
	return r.Query{Version: r.Version, Dialect: "portable", Expression: expr, Fields: map[string]r.Field{"n": {Type: "number", ProviderFields: map[string]string{"c": "n"}}, "s": {Type: "string", ProviderFields: map[string]string{"c": "s"}}, "m": {Type: "string", ProviderFields: map[string]string{"c": "missing"}}, "v": {Type: "any", ProviderFields: map[string]string{"c": "null"}}}, Limit: 20, MaxAgeSeconds: 60}
}
func TestTruthTypedNumbersAndUnknown(t *testing.T) {
	row := r.Record{ConnectionID: "c", Data: readsource.Item{Complete: true, Fields: map[string]json.RawMessage{"n": json.RawMessage(`0.123456789012345678901`), "s": json.RawMessage(`"hello"`), "null": json.RawMessage(`null`)}, Provenance: map[string][]readsource.Pointer{"n": {{}}, "s": {{}}, "null": {{}}}}}
	for _, test := range []struct {
		expr string
		want Truth
	}{{`n == 0.123456789012345678901`, True}, {`n == 0.123456789012345678902`, False}, {`s contains "ell"`, True}, {`s in ["x", "hello"]`, True}, {`v == null`, True}, {`not(m == "x")`, Unknown}, {`m != null`, Unknown}, {`s == "other" and m == "x"`, False}, {`s == "hello" or m == "x"`, True}, {`(s == "other" or m == "x") and not (s == "other")`, Unknown}} {
		q := fixtureQuery(test.expr)
		node, e := Parse(q)
		if e != nil {
			t.Fatal(test.expr, e)
		}
		if got := node.Eval(row, q.Fields); got != test.want {
			t.Fatalf("%s: %v != %v", test.expr, got, test.want)
		}
	}
	q := fixtureQuery(`s == "hello"`)
	node, _ := Parse(q)
	row.Data.Complete = false
	if node.Eval(row, q.Fields) != Unknown {
		t.Fatal("conflicted/incomplete row selected arrival-order field")
	}
	row.Data.Complete = true
	row.Data.LossyFields = []string{"s"}
	if node.Eval(row, q.Fields) != Unknown {
		t.Fatal("lossy field treated as exact")
	}
}
func TestQueryRejectsUnsupportedOrExecutableInput(t *testing.T) {
	for _, expr := range []string{`unknown == "x"`, `s == 1`, `s contains 1`, `s == $(touch /tmp/x)`, `exec("s")`, `s == "x"; exit`, `s == "x" || true`, `n in [1,"x"]`, `not `, strings.Repeat("(", 33) + `s == "x"` + strings.Repeat(")", 33)} {
		if _, e := Parse(fixtureQuery(expr)); e == nil {
			t.Fatal("accepted invalid query", expr)
		}
	}
}
