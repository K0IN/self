package clef

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-server/internal/decision"
	"ai-server/internal/settings"
)

func TestArgs(t *testing.T) {
	cfg := decision.RuntimeConfig{
		Device:   "cuda:0",
		Files:    decision.ModelFiles{Model: "/m/model.gguf", MMProj: "/m/mmproj.gguf", Head: "/m/joint_head.safetensors"},
		Settings: settings.Values{"context_size": int64(8192), "gpu_layers": int64(-1)},
	}
	got := strings.Join(Spec.Args(cfg), " ")
	want := "--model /m/model.gguf --head /m/joint_head.safetensors --device cuda:0 --mmproj /m/mmproj.gguf --ctx 8192 --gpu-layers -1"
	if got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestArgsWithoutProjector(t *testing.T) {
	got := strings.Join(Spec.Args(decision.RuntimeConfig{
		Device: "cpu",
		Files:  decision.ModelFiles{Model: "/m/model.gguf", Head: "/m/head.safetensors"},
	}), " ")
	if strings.Contains(got, "--mmproj") {
		t.Fatalf("args = %q", got)
	}
}

func TestCheckRejectsNonGGUF(t *testing.T) {
	model := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(model, []byte("not a gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Spec.Check(decision.RuntimeConfig{Files: decision.ModelFiles{Model: model, Head: "/m/head.safetensors"}})
	if err == nil || !strings.Contains(err.Error(), "GGUF") {
		t.Fatalf("a file that is not a GGUF must be rejected, got %v", err)
	}
}
