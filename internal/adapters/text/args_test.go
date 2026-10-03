package text

import (
	"strings"
	"testing"

	dom "ai-server/internal/text"
)

func TestArgsIncludesProjectorAsSeparateArgument(t *testing.T) {
	args := Args(dom.RuntimeConfig{
		Files:  dom.ModelFiles{Model: "/models/model.gguf", MMProj: "/models/mmproj.gguf"},
		Device: "cpu",
	}, 43210)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "model.gguf--host") {
		t.Fatalf("model and host arguments were concatenated: %q", joined)
	}
	want := []string{"--model", "/models/model.gguf", "--host", "127.0.0.1", "--port", "43210", "--no-webui", "--jinja", "--device", "none", "--mmproj", "/models/mmproj.gguf"}
	if len(args) < len(want) {
		t.Fatalf("args = %q, want prefix %q", args, want)
	}
	for i, value := range want {
		if args[i] != value {
			t.Fatalf("args[%d] = %q, want %q; full args = %q", i, args[i], value, args)
		}
	}
}
