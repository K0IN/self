package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const demoRegistry = "version: 1\n" +
	"models:\n" +
	"  demo:1b:\n" +
	"    description: demo\n" +
	"    readme: readmes/demo/1b.md\n" +
	"    type: decision\n" +
	"    default: q4\n" +
	"    info:\n" +
	"      context_length: 128\n" +
	"      max_options: 2\n" +
	"    q4:\n" +
	"      adapter: ggmlc-laya\n" +
	"      repo: someone/demo\n" +
	"      files:\n" +
	"        - file: demo.gguf\n" +
	"          size: 4\n" +
	"          sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"

func TestLoadRegistryFallsBackToCache(t *testing.T) {
	body := ""
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := server.Client()
	dir := t.TempDir()

	if _, err := loadRegistryCached(client, server.URL, dir); err == nil || !strings.Contains(err.Error(), "offline cache unavailable") {
		t.Fatalf("no cache and invalid registry: err = %v", err)
	}

	body = demoRegistry
	if reg, err := loadRegistryCached(client, server.URL, dir); err != nil || len(reg.Models) != 1 {
		t.Fatalf("online: reg=%v err=%v", reg, err)
	}

	status = http.StatusServiceUnavailable
	if reg, err := loadRegistryCached(client, server.URL, dir); err != nil || len(reg.Models) != 1 {
		t.Fatalf("registry down: reg=%v err=%v", reg, err)
	}

	status, body = http.StatusOK, "not: [valid"
	if reg, err := loadRegistryCached(client, server.URL, dir); err != nil || len(reg.Models) != 1 {
		t.Fatalf("registry invalid: reg=%v err=%v", reg, err)
	}

	server.Close()
	if reg, err := loadRegistryCached(client, server.URL, dir); err != nil || len(reg.Models) != 1 {
		t.Fatalf("unreachable: reg=%v err=%v", reg, err)
	}
}

func TestLoadRegistryLocalFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "registry.yml")
	if err := os.WriteFile(file, []byte(demoRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	modelsDir := t.TempDir()
	reg, err := LoadRegistry(file, modelsDir)
	if err != nil || len(reg.Models) != 1 {
		t.Fatalf("reg=%v err=%v", reg, err)
	}
	// A local registry must not overwrite the cache of the published one.
	if entries, _ := os.ReadDir(modelsDir); len(entries) != 0 {
		t.Fatalf("local registry wrote %d cache entries", len(entries))
	}
	if _, err := LoadRegistry(filepath.Join(t.TempDir(), "missing.yml"), modelsDir); err == nil {
		t.Fatal("missing registry file accepted")
	}
}

func TestLoadRegistryIgnoresUnwritableCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(demoRegistry))
	}))
	defer server.Close()
	// A models path that is a file makes every cache write fail.
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistryCached(server.Client(), server.URL, notADir)
	if err != nil || len(reg.Models) != 1 {
		t.Fatalf("reg=%v err=%v", reg, err)
	}
}
