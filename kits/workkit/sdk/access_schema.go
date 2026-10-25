package sdk

import (
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"reflect"
)

func AccessContractSchema(kind string) (map[string]any, error) {
	records := map[string]any{"configuration": a.Configuration{}, "policy": a.Policy{}, "grant": a.Grant{}, "receipt": a.Receipt{}, "install-plan": a.InstallPlan{}, "artifact": a.Artifact{}, "tool-pin": a.ToolPin{}, "tool-observation": a.Observation{}, "binding": a.Binding{}, "broker-config": a.BrokerConfig{}, "registry": a.ConnectionRegistry{}, "discovery": a.DiscoveryRequest{}, "native-enrollment": a.NativeEnrollment{}, "access-state": a.AccessState{}}
	v, ok := records[kind]
	if !ok {
		return nil, errors.New("unknown access contract")
	}
	s := readSchemaType(reflect.TypeOf(v))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["$id"] = "https://workkit.example/spec/access/v1/" + kind + ".schema.json"
	props := s["properties"].(map[string]any)
	if _, ok := props["version"]; ok {
		props["version"] = map[string]any{"const": a.Version}
	}
	for _, name := range []string{"authorizationGranted", "executionReady", "migrationActivated", "productionActivated"} {
		if _, ok := props[name]; ok {
			props[name] = map[string]any{"const": false}
		}
	}
	return s, nil
}
