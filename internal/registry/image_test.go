package registry

import (
	"fmt"
	"strings"
	"testing"
)

func TestImageSafetensorsAuxiliaries(t *testing.T) {
	for _, test := range []struct {
		name, kind, role, filename string
		wantError                  bool
	}{
		{"image VAE", "image", "vae", "vae.safetensors", false},
		{"image encoder", "image", "text-encoder", "encoder.safetensors", false},
		{"GGUF projector", "image", "mmproj", "vision.gguf", false},
		{"reject projector safetensors", "image", "mmproj", "vision.safetensors", true},
		{"reject pickle VAE", "image", "vae", "vae.pt", true},
		{"no text exception", "text", "mmproj", "vision.safetensors", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := fmt.Sprintf(`version: 1
models:
  test:7b:
    description: Test pipeline
    readme: readmes/test/7b.md
    type: %s
    default: q4
    q4:
      adapter: sd-server
      repo: test/model
      files:
        - {file: model.gguf, size: 1, sha256: %s}
        - {file: %s, role: %s, size: 1, sha256: %s}
`, test.kind, strings.Repeat("a", 64), test.filename, test.role, strings.Repeat("b", 64))
			_, err := Parse([]byte(doc))
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v wantError=%v", err, test.wantError)
			}
		})
	}
}
