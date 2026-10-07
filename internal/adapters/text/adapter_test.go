package text

import (
	"context"
	"reflect"
	"testing"

	"ai-server/internal/errs"
	dom "ai-server/internal/text"
)

func TestArgsProjector(t *testing.T) {
	cfg := dom.RuntimeConfig{Files: dom.ModelFiles{Model: "/models/model.gguf", MMProj: "/models/mmproj.gguf"}, Device: "cpu"}
	want := []string{"--model", cfg.Files.Model, "--host", "127.0.0.1", "--port", "12345", "--no-webui", "--jinja", "--device", "none", "--mmproj", cfg.Files.MMProj}
	if got := Args(cfg, 12345); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestMissingProjector(t *testing.T) {
	adapter := New()
	err := adapter.Start(context.Background(), dom.RuntimeConfig{ModelID: "qwen3.5:9b", Files: dom.ModelFiles{MMProj: t.TempDir() + "/missing.gguf"}})
	if errs.KindOf(err) != errs.UnsupportedModel {
		t.Fatalf("missing projector error = %v", err)
	}
}
