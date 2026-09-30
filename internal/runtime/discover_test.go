package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchDirsOverrideWins(t *testing.T) {
	got := SearchDirs("/custom/engines")
	if len(got) != 1 || got[0] != "/custom/engines" {
		t.Fatalf("SearchDirs(override) = %v, want only the override", got)
	}
}

func TestSearchDirsSourceTreeIsLastResort(t *testing.T) {
	src := sourceTreeBundle()
	if src == "" {
		t.Skip("built with -trimpath: no source tree to point at")
	}
	if _, err := os.Stat(filepath.Join(src, "..", "..", "..", "go.mod")); err != nil {
		t.Fatalf("sourceTreeBundle %q is not inside the module root: %v", src, err)
	}
	dirs := SearchDirs("")
	if len(dirs) < 3 || dirs[len(dirs)-1] != src {
		t.Fatalf("SearchDirs(\"\") = %v, want source tree %q last", dirs, src)
	}
}
