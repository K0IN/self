package adapters

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"ai-server/internal/adapters/customdecider"
	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/localconf"
	"ai-server/internal/registry"
)

const reg = `version: 1
models:
  d:1b:
    description: D
    readme: readmes/d/1b.md
    type: decision
    capabilities:
      input: [text]
      output: [choice]
    info: {family: d, parameters: 1B, context_length: 32768, max_options: 10, languages: [en]}
    settings: {context_size: 8192, temperature: 1.5}
    default: q4
    q4:
      adapter: ggmlc-custom-decider
      repo: a/b
      settings: {context_size: 4096}
      files:
        - {file: d.gguf, size: 1, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
    q8:
      adapter: ggmlc-custom-decider
      repo: a/b
      files:
        - {file: d8.gguf, size: 1, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
`

func resolve(t *testing.T, doc, quant string) registry.Resolved {
	t.Helper()
	r, err := registry.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Resolve("d:1b", registry.ResolveOptions{Quant: quant})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Precedence: model settings < quant settings < --set overrides.
func TestResolveSettingsPrecedence(t *testing.T) {
	res := resolve(t, reg, "q4")
	if res.Model.Info.ContextLength != 32768 || res.Model.Info.MaxOptions != 10 || res.Model.Info.Parameters != "1B" {
		t.Fatalf("info = %+v", res.Model.Info)
	}
	v, err := ResolveSettings(res, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v["context_size"] != int64(4096) || v["temperature"] != 1.5 {
		t.Fatalf("quant override: %v", v)
	}
	v, _ = ResolveSettings(resolve(t, reg, "q8"), nil)
	if v["context_size"] != int64(8192) {
		t.Fatalf("model level: %v", v)
	}
	v, _ = ResolveSettings(res, map[string]any{"context_size": "2048", "flash_attn": "on"})
	if v["context_size"] != int64(2048) || v["flash_attn"] != "on" {
		t.Fatalf("cli override: %v", v)
	}
	args := customdecider.Spec.Args(decision.RuntimeConfig{Files: decision.ModelFiles{Model: "m.gguf"}, Device: "cuda", Settings: v})
	want := []string{"--model", "m.gguf", "--device", "cuda", "--ctx", "2048", "--temperature", "1.5", "--gpu-layers", "-1", "--flash-attn", "on"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v", args)
	}
}

// Local file sits between the registry and --set.
func TestResolveSettingsLocal(t *testing.T) {
	local, err := localconf.Parse([]byte(`
adapters:
  ggmlc-custom-decider: {threads: 8, temperature: 2.0}
models:
  d:1b:
    settings: {context_size: 2048}
    quants:
      q4: {flash_attn: "off"}
`))
	if err != nil {
		t.Fatal(err)
	}
	v, src, err := ResolveSettingsLayered(resolve(t, reg, "q4"), local, map[string]any{"threads": "2"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"context_size": int64(2048), "temperature": 2.0, "threads": int64(2), "flash_attn": "off"}
	if !reflect.DeepEqual(map[string]any(v), want) {
		t.Fatalf("values = %v", v)
	}
	wantSrc := map[string]string{"context_size": "local model", "temperature": "local adapter ggmlc-custom-decider",
		"threads": "--set", "flash_attn": "local quant q4"}
	if !reflect.DeepEqual(src, wantSrc) {
		t.Fatalf("sources = %v", src)
	}
	// Local values are validated against the schema too.
	bad, _ := localconf.Parse([]byte("models: {d:1b: {settings: {context_size: 5}}}"))
	if _, _, err := ResolveSettingsLayered(resolve(t, reg, "q4"), bad, nil); errs.KindOf(err) != errs.InvalidRequest {
		t.Fatalf("bad local value: %v", err)
	}
}

func TestResolveSettingsRejects(t *testing.T) {
	for name, doc := range map[string]string{
		"unknown key": strings.Replace(reg, "temperature: 1.5", "top_k: 5", 1),
		"bad range":   strings.Replace(reg, "context_size: 4096", "context_size: 10", 1),
		"bad type":    strings.Replace(reg, "temperature: 1.5", "temperature: hot", 1),
	} {
		_, err := ResolveSettings(resolve(t, doc, "q4"), nil)
		if errs.KindOf(err) != errs.InvalidRequest {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if _, err := ResolveSettings(resolve(t, reg, "q4"), map[string]any{"context_size": "huge"}); err == nil {
		t.Error("bad --set accepted")
	}
}

// Every bundled model's settings must be valid for its adapter.
func TestBundledRegistrySettings(t *testing.T) {
	b, err := os.ReadFile("../../models/registry.yml")
	if err != nil {
		t.Fatal(err)
	}
	r, err := registry.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range r.IDs() {
		for _, q := range r.Models[id].Quants() {
			res, _ := r.Resolve(id, registry.ResolveOptions{Quant: q})
			if _, err := ResolveSettings(res, nil); err != nil {
				t.Errorf("%s/%s: %v", id, q, err)
			}
			if m := r.Models[id]; m.Type == registry.TypeDecision && (m.Info.ContextLength == 0 || m.Info.MaxOptions == 0) {
				t.Errorf("%s: info.context_length and info.max_options should be set", id)
			}
		}
	}
}
