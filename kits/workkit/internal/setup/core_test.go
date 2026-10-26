package setup

import (
	"encoding/json"
	"testing"
)

func TestPointerStrictAndExact(t *testing.T) {
	raw := []byte(`{"a/b":{"~x":[9007199254740993.00000000000001]}}`)
	v, e := Pointer(raw, "/a~1b/~0x/0")
	if e != nil || string(v.(json.Number)) != "9007199254740993.00000000000001" {
		t.Fatal(v, e)
	}
	for _, p := range []string{"a", "/a~2b", "/a~1b/~0x/00", "/a~1b/~0x/1"} {
		if _, e := Pointer(raw, p); e == nil {
			t.Fatal("unsafe pointer", p)
		}
	}
}
