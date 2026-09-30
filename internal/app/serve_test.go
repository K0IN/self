package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRegistry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("version: 1\nmodels: {}\n"))
	}))
	defer server.Close()
	reg, _, err := loadRegistry(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Models) != 0 {
		t.Fatalf("expected empty test registry, got %d models", len(reg.Models))
	}
}
