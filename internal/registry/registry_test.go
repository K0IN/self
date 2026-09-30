package registry

import (
	"strings"
	"testing"

	"ai-server/internal/errs"
)

const sample = `
version: 1
models:
  "decider-vision:2b":
    description: "Decider 2B Vision: decisions about text and one image"
    readme: readmes/decider-vision/2b.md
    type: decision
	default: q4
    capabilities:
      input: [text, vision]
      output: [choice, score, noul]
	q4:
      adapter: ggmlc-laya
      repo: mradermacher/decider-2b-vision-GGUF
      files:
        - {file: decider-2b-vision.Q4_K_M.gguf, size: 100, sha256: ` + sumA + `}
        - {file: decider-2b-vision.mmproj-Q8_0.gguf, size: 20, sha256: ` + sumB + `, role: mmproj}
	q8:
      adapter: ggmlc-laya
      repo: mradermacher/decider-2b-vision-GGUF
      files:
        - {file: decider-2b-vision.Q8_0.gguf, size: 200, sha256: ` + sumA + `}
  kev:4b:
    description: Kev 4B
    readme: readmes/kev/4b.md
    type: decision
    capabilities:
      input: [text]
      output: [choice, score, noul]
	q4:
      adapter: ggmlc-laya
      repo: mys/kev-4b-GGUF
      files:
        - {file: kev_4b_ud_q4_k_m.gguf, size: 42, sha256: ` + sumB + `}
`

const (
	sumA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sumB = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

func mustParse(t *testing.T, s string) *Registry {
	t.Helper()
	r, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseValid(t *testing.T) {
	r := mustParse(t, sample)
	if got := strings.Join(r.IDs(), ","); got != "decider-vision:2b,kev:4b" {
		t.Fatalf("ids = %s", got)
	}
	d := r.Models["decider-vision:2b"]
	if d.Type != TypeDecision || d.Default != "q4" || len(d.Variants) != 2 {
		t.Fatalf("bad model: %+v", d)
	}
	files := d.Variants["q4"].Files
	if len(files) != 2 || files[0].Role != RoleModel || files[1].Role != RoleMMProj {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Size != 100 || files[0].SHA256 != sumA || files[1].SHA256 != strings.ToLower(sumB) {
		t.Fatalf("pins = %+v", files)
	}
	if d.Variants["q4"].Size() != 120 {
		t.Fatalf("variant size = %d", d.Variants["q4"].Size())
	}
	if d.Readme != "readmes/decider-vision/2b.md" || d.Description == "" {
		t.Fatalf("readme/description = %q %q", d.Readme, d.Description)
	}
	// single variant => implicit default
	if r.Models["kev:4b"].Default != "q4" {
		t.Fatal("implicit default not set")
	}
}

func TestResolveDefaultAndOverride(t *testing.T) {
	r := mustParse(t, sample)
	res, err := r.Resolve("decider-vision:2b", ResolveOptions{})
	if err != nil || res.Variant.Quant != "q4" {
		t.Fatalf("default: %v %+v", err, res.Variant)
	}
	res, err = r.Resolve("decider-vision:2b", ResolveOptions{Quant: "q8"})
	if err != nil || res.Variant.Files[0].Name != "decider-2b-vision.Q8_0.gguf" {
		t.Fatalf("override: %v %+v", err, res.Variant)
	}
	if res.Name() != "decider-vision" || res.Tag() != "2b" {
		t.Fatalf("name/tag = %s %s", res.Name(), res.Tag())
	}
}

func TestResolveErrors(t *testing.T) {
	r := mustParse(t, sample)
	cases := []struct {
		ref  string
		opt  ResolveOptions
		kind errs.Kind
	}{
		{"nope:1b", ResolveOptions{}, errs.ModelNotFound},
		{"kev:4b", ResolveOptions{Quant: "q2"}, errs.QuantNotFound},
		{"kev:4b", ResolveOptions{Type: "image"}, errs.UnsupportedModel},
		{"kev:4b", ResolveOptions{AdapterKnown: func(string, ModelType) bool { return false }}, errs.UnsupportedModel},
	}
	for _, c := range cases {
		_, err := r.Resolve(c.ref, c.opt)
		if errs.KindOf(err) != c.kind {
			t.Errorf("%s: got %v, want %s", c.ref, err, c.kind)
		}
	}
}

func TestCapabilities(t *testing.T) {
	r := mustParse(t, sample)
	c := r.Models["decider-vision:2b"].Capabilities
	if !c.Has(CapVision) || !c.Has(CapChoice) || c.Has(CapMultiImage) {
		t.Fatalf("caps = %v", c.List())
	}
	_, err := Parse([]byte(strings.Replace(sample, "output: [choice, score, noul]", "output: [choice, telepathy]", 1)))
	if err == nil || !strings.Contains(err.Error(), "telepathy") {
		t.Fatalf("unknown capability accepted: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	bad := map[string]string{
		"wrong type":      strings.Replace(sample, "type: decision\n    default: q4", "type: painting\n    default: q4", 1),
		"missing default": strings.Replace(sample, "    default: 4bit\n", "", 1),
		"bad default":     strings.Replace(sample, "default: 4bit", "default: 3bit", 1),
		"non gguf":        strings.Replace(sample, "kev_4b_ud_q4_k_m.gguf", "model.safetensors", 1),
		"no size":         strings.Replace(sample, "size: 42, ", "", 1),
		"bad sha256":      strings.Replace(sample, "size: 42, sha256: "+sumB, "size: 42, sha256: abc", 1),
		"no sha256":       strings.Replace(sample, ", sha256: "+sumB+"}\n", "}\n", 1),
		"plain file":      strings.Replace(sample, "- {file: kev_4b_ud_q4_k_m.gguf, size: 42, sha256: "+sumB+"}", "- kev_4b_ud_q4_k_m.gguf", 1),
		"no readme":       strings.Replace(sample, "    readme: readmes/kev/4b.md\n", "", 1),
		"inline readme":   strings.Replace(sample, "readme: readmes/kev/4b.md", "readme: \"# Kev\"", 1),
		"escaping readme": strings.Replace(sample, "readme: readmes/kev/4b.md", "readme: ../../etc/x.md", 1),
		"url readme":      strings.Replace(sample, "readme: readmes/kev/4b.md", "readme: https://x/y.md", 1),
		"no description":  strings.Replace(sample, "    description: Kev 4B\n", "", 1),
		"unsafe path":     strings.Replace(sample, "kev_4b_ud_q4_k_m.gguf", "../evil.gguf", 1),
		"no adapter":      strings.Replace(sample, "      adapter: ggmlc-laya\n      repo: mys", "      repo: mys", 1),
		"bad version":     strings.Replace(sample, "version: 1", "version: 2", 1),
	}
	for name, doc := range bad {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
