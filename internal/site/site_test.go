package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const reg = `version: 1
models:
  demo:1b-vision:
    description: "Demo <b>model</b>"
    readme: readmes/demo/1b-vision.md
    type: decision
    capabilities:
      input: [text, vision]
      output: [choice]
    4bit:
      adapter: ggmlc-custom-decider
      repo: someone/demo-GGUF
      files:
        - {file: demo.Q4_K_M.gguf, size: 1048576, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
        - {file: demo.mmproj.gguf, size: 1024, sha256: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, role: mmproj}
`

func TestBuild(t *testing.T) {
	out := t.TempDir()
	fsys := fstest.MapFS{"readmes/demo/1b-vision.md": {Data: []byte("# Demo\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")}}
	if err := Build([]byte(reg), fsys, out, Options{RepoURL: "https://github.com/x/y"}); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(out, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	idx := read("index.html")
	for _, want := range []string{`href="models/demo/1b-vision.html"`, "Demo &lt;b&gt;model&lt;/b&gt;", "1.00 MiB", `tag vision`} {
		if !strings.Contains(idx, want) {
			t.Errorf("index missing %q", want)
		}
	}
	page := read("models/demo/1b-vision.html")
	for _, want := range []string{"<h1>Demo</h1>", "<table>", "sha256 aaaa", "mmproj", "https://huggingface.co/someone/demo-GGUF/blob/main/demo.Q4_K_M.gguf", `href="../../index.html"`, "/blob/main/models/readmes/demo/1b-vision.md"} {
		if !strings.Contains(page, want) {
			t.Errorf("model page missing %q", want)
		}
	}
	if read("registry.yml") != reg {
		t.Error("registry.yml not copied")
	}
	if _, err := os.Stat(filepath.Join(out, ".nojekyll")); err != nil {
		t.Error(".nojekyll missing")
	}
}

func TestBuildMissingReadme(t *testing.T) {
	if err := Build([]byte(reg), fstest.MapFS{}, t.TempDir(), Options{}); err == nil {
		t.Fatal("expected error for missing readme")
	}
}

// The real registry must render.
func TestBuildBundledRegistry(t *testing.T) {
	b, err := os.ReadFile("../../models/registry.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := Build(b, os.DirFS("../../models"), t.TempDir(), Options{}); err != nil {
		t.Fatal(err)
	}
}
