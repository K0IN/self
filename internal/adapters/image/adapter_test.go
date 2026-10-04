package image

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dom "ai-server/internal/image"
	"ai-server/internal/settings"
)

func TestGenerationExtension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		prompt, _ := body["prompt"].(string)
		_, payload, ok := strings.Cut(prompt, "<sd_cpp_extra_args>")
		var extra map[string]any
		if !ok || json.Unmarshal([]byte(strings.TrimSuffix(payload, "</sd_cpp_extra_args>")), &extra) != nil {
			t.Errorf("invalid extension: %q", prompt)
		}
		if extra["seed"] != float64(42) || extra["negative_prompt"] != "blur" || extra["sample_steps"] != float64(4) {
			t.Errorf("extension = %v", extra)
		}
		if _, ok := body["seed"]; ok {
			t.Error("seed must be embedded in the prompt extension")
		}
		_, _ = w.Write([]byte(`{"output_format":"png","data":[]}`))
	}))
	defer server.Close()
	adapter := &Adapter{client: server.Client(), baseURL: server.URL}
	seed := int64(42)
	extra := map[string]any{"seed": 1, "sample_steps": 4}
	_, err := adapter.Generate(context.Background(), dom.GenerateRequest{Prompt: "test", Seed: &seed, NegativePrompt: "blur", Extra: extra})
	if err != nil {
		t.Fatal(err)
	}
	if extra["seed"] != 1 {
		t.Fatal("request extra map was mutated")
	}
}

func TestEditOmitsUnspecifiedSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if _, ok := r.MultipartForm.Value["size"]; ok {
			t.Error("unspecified size must be omitted")
		}
		_, _ = w.Write([]byte(`{"output_format":"png","data":[]}`))
	}))
	defer server.Close()
	adapter := &Adapter{client: server.Client(), baseURL: server.URL}
	if _, err := adapter.Edit(context.Background(), dom.EditRequest{Prompt: "edit"}); err != nil {
		t.Fatal(err)
	}
}

func TestArgsAuxiliariesAndBooleans(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		args := strings.Join(Args(dom.RuntimeConfig{Files: dom.ModelFiles{Model: "model.gguf", VAE: "vae.safetensors", TextEncoder: "text.gguf", MMProj: "vision.gguf"}, Settings: settings.Values{"offload_to_cpu": enabled}}, 8081), " ")
		for _, want := range []string{"--vae vae.safetensors", "--llm text.gguf", "--llm_vision vision.gguf"} {
			if !strings.Contains(args, want) {
				t.Errorf("missing %q in %s", want, args)
			}
		}
		if strings.Contains(args, "--offload-to-cpu") != enabled || strings.Contains(args, " true") || strings.Contains(args, " false") {
			t.Errorf("incorrect boolean flags: %s", args)
		}
	}
}

func TestOfficialServerEagerLoadAndDevice(t *testing.T) {
	for device, expected := range map[string]string{"cpu": "--backend cpu --params-backend cpu", "cuda": "--backend cuda0", "cuda:1": "--backend cuda1"} {
		args := strings.Join(Args(dom.RuntimeConfig{Device: device}, 8081), " ")
		if !strings.Contains(args, "--eager-load") || !strings.Contains(args, expected) {
			t.Errorf("device %s: %s", device, args)
		}
	}
}
