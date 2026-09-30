package localconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const doc = `version: 1
adapters:
  ggmlc-custom-decider:
    threads: 8
models:
  "decider-vision:2b":
    settings:
      context_size: 4096
    quants:
      8bit:
        flash_attn: "on"
`

func TestParseAndLayers(t *testing.T) {
	c, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	l := c.Layers("decider-vision:2b", "8bit", "ggmlc-custom-decider")
	if len(l) != 3 || l[0].Values["threads"] != 8 || l[1].Values["context_size"] != 4096 || l[2].Values["flash_attn"] != "on" {
		t.Fatalf("layers = %+v", l)
	}
	if got := c.Layers("decider-vision:2b", "4bit", "ggmlc-laya"); len(got) != 1 || got[0].Name != "local model" {
		t.Fatalf("other quant/adapter = %+v", got)
	}
	if got := c.Layers("kev:4b", "4bit", "ggmlc-laya"); len(got) != 0 {
		t.Fatalf("unrelated = %+v", got)
	}
	var nilc *Config
	if nilc.Layers("a", "b", "c") != nil {
		t.Fatal("nil config must have no layers")
	}
}

func TestParseRejects(t *testing.T) {
	for name, d := range map[string]string{
		"bad version": "version: 2\n",
		"typo field":  "modles: {}\n",
		"bad key":     "adapters: {x: {Context-Size: 1}}\n",
		"nested":      "models: {a:1b: {settings: {x: {y: 1}}}}\n",
		"model typo":  "models: {a:1b: {setting: {x: 1}}}\n",
	} {
		if _, err := Parse([]byte(d)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := Parse(nil); err != nil {
		t.Errorf("empty file: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.yml")
	if c, err := Load(missing, true); err != nil || c.Path != "" {
		t.Fatalf("optional missing: %+v %v", c, err)
	}
	if _, err := Load(missing, false); err == nil {
		t.Fatal("explicit missing file accepted")
	}
	p := filepath.Join(dir, "s.yml")
	os.WriteFile(p, []byte(doc), 0o644)
	c, err := Load(p, false)
	if err != nil || c.Path != p {
		t.Fatalf("%+v %v", c, err)
	}
	os.WriteFile(p, []byte("adapters: [1]"), 0o644)
	if _, err := Load(p, true); err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("error should name the file: %v", err)
	}
}
