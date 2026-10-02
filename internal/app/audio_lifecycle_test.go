package app

import (
	"context"
	"io"
	"net"
	"testing"

	"ai-server/internal/api"
	"ai-server/internal/config"
	"github.com/go-chi/chi/v5"
)

func TestServeHTTPOptionalServiceCallbacks(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	closed := false
	mount := func(chi.Router) api.ModelInfo { return api.ModelInfo{ID: "test-audio"} }
	err = serveHTTP(context.Background(), config.Serve{
		Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
	}, io.Discard, runningModel{Kind: "audio", Mount: mount, Close: func() { closed = true }})
	if err == nil || !closed {
		t.Fatalf("err=%v closed=%v", err, closed)
	}
}
