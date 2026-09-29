package app

import (
	"testing"

	"ai-server/internal/adapters"
	"ai-server/internal/registry"
)

func TestBundledRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kev:0.5b", "kev:4b", "laya:english", "decider:2b-vision"} {
		if _, err := reg.Resolve(id, registry.ResolveOptions{AdapterKnown: adapters.Known}); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for id, m := range reg.Models {
		for q, v := range m.Variants {
			if !adapters.Known(v.Adapter, m.Type) {
				t.Errorf("%s/%s: unknown adapter %s", id, q, v.Adapter)
			}
		}
	}
}
