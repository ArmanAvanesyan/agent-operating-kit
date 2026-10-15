package contracts

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestTaskPacketSchemaTracksExistingContract(t *testing.T) {
	source, err := os.ReadFile("../../schemas/task-packet.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := schemaFS.ReadFile("schemas/task-packet.json")
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	if err = json.Unmarshal(source, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(embedded, &b); err != nil {
		t.Fatal(err)
	}
	delete(a, "$id")
	delete(b, "$id")
	if !reflect.DeepEqual(a, b) {
		t.Fatal("embedded task packet schema drifted from existing contract")
	}
}
func TestValidateTaskPacket(t *testing.T) {
	data, err := os.ReadFile("../../docs/proposals/agent-spec/examples/task-packet.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateTaskPacket(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{[]byte(`{"title":"missing fields"}`), []byte(`{"title":"a","title":"b"}`)} {
		if err = ValidateTaskPacket(bad); err == nil {
			t.Fatal("accepted invalid task packet")
		}
	}
}
