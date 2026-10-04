package registry

import (
	"strings"
	"testing"
)

func TestLegacyPublishedIDs(t *testing.T) {
	for legacy, canonical := range legacyModelIDs {
		doc := strings.ReplaceAll(sample, "kev:4b", legacy)
		reg, err := Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if reg.Models[canonical].ID != canonical {
			t.Errorf("%s did not normalize to %s", legacy, canonical)
		}
	}
}

func TestLegacyCanonicalCollision(t *testing.T) {
	_, err := Parse([]byte("version: 1\nmodels:\n  gemma4:e4b: {type: future}\n  gemma4:4b: {type: future}\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicate canonical") {
		t.Fatalf("want duplicate canonical error, got %v", err)
	}
}
