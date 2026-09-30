package onboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-server/internal/registry"
)

var timeZero time.Time

// lfs attaches Hub LFS metadata (size + a fixed fake sha256).
func lfs(f File) File {
	f.LFS = &struct {
		OID  string `json:"oid"`
		Size int64  `json:"size"`
	}{OID: strings.Repeat("ab", 32), Size: f.Size}
	return f
}

func TestDeriveID(t *testing.T) {
	for in, want := range map[string]string{
		"mradermacher/decider-2b-vision-GGUF": "decider-vision:2b",
		"mys/kev-4b-GGUF":                     "kev:4b",
		"mys/laya-GGUF":                       "laya:latest",
	} {
		if got := DeriveID(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func gguf(kv map[string]string) []byte {
	var b bytes.Buffer
	b.WriteString("GGUF")
	binary.Write(&b, binary.LittleEndian, uint32(3))
	binary.Write(&b, binary.LittleEndian, uint64(0))
	binary.Write(&b, binary.LittleEndian, uint64(len(kv)))
	for k, v := range kv {
		binary.Write(&b, binary.LittleEndian, uint64(len(k)))
		b.WriteString(k)
		binary.Write(&b, binary.LittleEndian, uint32(8))
		binary.Write(&b, binary.LittleEndian, uint64(len(v)))
		b.WriteString(v)
	}
	b.Write(make([]byte, 64))
	return b.Bytes()
}

func hub(files []File, header []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/models/") {
			json.NewEncoder(w).Encode(files)
			return
		}
		http.ServeContent(w, r, "x.gguf", timeZero, bytes.NewReader(header))
	}))
}

func TestAnalyzeVisionRepo(t *testing.T) {
	files := []File{
		{Path: "README.md", Type: "file"},
		lfs(File{Path: "m.Q2_K.gguf", Type: "file", Size: 1}),
		lfs(File{Path: "m.Q4_K_M.gguf", Type: "file", Size: 4}),
		lfs(File{Path: "m.Q4_K_S.gguf", Type: "file", Size: 3}),
		lfs(File{Path: "m.Q8_0.gguf", Type: "file", Size: 8}),
		lfs(File{Path: "m.f16.gguf", Type: "file", Size: 16}),
		lfs(File{Path: "m.mmproj-f16.gguf", Type: "file", Size: 2}),
		lfs(File{Path: "m.mmproj-Q8_0.gguf", Type: "file", Size: 1}),
	}
	srv := hub(files, gguf(map[string]string{"general.architecture": "qwen35", "general.name": "Decider 2b Vision"}))
	defer srv.Close()
	c := NewClient()
	c.BaseURL = srv.URL
	r, err := c.Analyze(context.Background(), "someone/decider-2b-vision-GGUF", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Adapter != "ggmlc-custom-decider" || r.Default != "q4" || r.ID != "decider-vision:2b" {
		t.Fatalf("%+v", r)
	}
	quants := []string{}
	for _, v := range r.Variants {
		quants = append(quants, v.Quant+"="+v.Model.Path)
		if v.MMProj == nil || v.MMProj.Path != "m.mmproj-Q8_0.gguf" {
			t.Fatalf("mmproj = %+v", v.MMProj)
		}
	}
	if strings.Join(quants, ",") != "q4=m.Q4_K_M.gguf,q8=m.Q8_0.gguf,fp16=m.f16.gguf" {
		t.Fatalf("quants = %v", quants)
	}
	reg, err := registry.Parse([]byte("version: 1\nmodels:\n" + r.YAML()))
	if err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, r.YAML())
	}
	m := reg.Models["decider-vision:2b"]
	if !m.Capabilities.Has(registry.CapVision) {
		t.Fatal("vision not enabled")
	}
	if m.Readme != "readmes/decider-vision/2b.md" {
		t.Fatalf("readme = %q", m.Readme)
	}
	if rd := r.Readme(); !strings.HasPrefix(rd, "# Decider 2b Vision") || !strings.Contains(rd, "self serve decider-vision:2b") {
		t.Fatalf("readme skeleton = %q", rd)
	}
	f := m.Variants["4bit"].Files
	if f[0].Size != 4 || f[0].SHA256 != strings.Repeat("ab", 32) || f[1].Role != registry.RoleMMProj || f[1].Size != 1 {
		t.Fatalf("pins = %+v", f)
	}
}

func TestAnalyzeGgmlcRepo(t *testing.T) {
	files := []File{lfs(File{Path: "kev_4b_ud_q4_k_m.gguf", Type: "file", Size: 5}), lfs(File{Path: "kev_4b_q8_0.gguf", Type: "file", Size: 9})}
	srv := hub(files, gguf(map[string]string{"general.architecture": "ggmlc", "ggmlc.graph_spec": "{}", "ggmlc.decision": "{}"}))
	defer srv.Close()
	c := NewClient()
	c.BaseURL = srv.URL
	r, err := c.Analyze(context.Background(), "mys/kev-4b-GGUF", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Adapter != "ggmlc-laya" || len(r.Variants) != 2 || r.Variants[0].Model.Path != "kev_4b_ud_q4_k_m.gguf" {
		t.Fatalf("%+v", r)
	}
	if _, err := registry.Parse([]byte("version: 1\nmodels:\n" + r.YAML())); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeWarnsWithoutLFSHash(t *testing.T) {
	files := []File{{Path: "x.Q4_K_M.gguf", Type: "file", Size: 5}}
	srv := hub(files, gguf(map[string]string{"general.architecture": "qwen35"}))
	defer srv.Close()
	c := NewClient()
	c.BaseURL = srv.URL
	r, err := c.Analyze(context.Background(), "a/x-GGUF", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) == 0 || !strings.Contains(r.YAML(), "sha256: TODO") {
		t.Fatalf("warnings=%v yaml=%s", r.Warnings, r.YAML())
	}
	if _, err := registry.Parse([]byte("version: 1\nmodels:\n" + r.YAML())); err == nil {
		t.Fatal("registry accepted sha256: TODO")
	}
}
