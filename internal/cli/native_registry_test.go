package cli

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
	"testing"
)

func TestNativeTargetCatalogHasRegisteredAdapters(t *testing.T) {
	engine := nativeEngine()
	seen := map[string]bool{}
	if len(kit.NativeTargets) != 8 {
		t.Fatal("expected eight native targets")
	}
	for _, target := range kit.NativeTargets {
		if seen[target] {
			t.Fatalf("duplicate native target %s", target)
		}
		seen[target] = true
		descriptor, ok := engine.Describe(target)
		if !ok || descriptor.Target != target || len(descriptor.Scopes) == 0 {
			t.Fatalf("missing native adapter %s", target)
		}
	}
	for _, target := range kit.SupportedTargets {
		if target == "agy" {
			t.Fatal("native registration must not claim agy pack render support")
		}
	}
}
