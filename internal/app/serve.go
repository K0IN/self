// Package app wires registry, downloads, engine adapters and the HTTP server
// together for the self commands (serve, pull, check, benchmark, settings).
// Everything that does not depend on the model type lives in target.go and
// serve.go; each type adds its own file (decision.go).
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"ai-server/internal/api"
	"ai-server/internal/config"
	"ai-server/internal/errs"
	"ai-server/internal/registry"
	"ai-server/internal/runtime"
)

// Serve runs `self serve` until ctx is cancelled or the engine dies. Each
// model type starts its engine in its own serve function and hands the
// running model to serveHTTP.
func Serve(ctx context.Context, cfg config.Serve, out io.Writer, isTTY bool) error {
	t, err := resolveTarget(cfg)
	if err != nil {
		return err
	}
	switch t.res.Model.Type {
	case registry.TypeDecision:
		return serveDecision(ctx, cfg, t, out, isTTY)
	case registry.TypeAudio:
		return serveAudio(ctx, cfg, t, out, isTTY)
	case registry.TypeText:
		return serveText(ctx, cfg, t, out, isTTY)
	case registry.TypeEmbedding:
		return serveEmbedding(ctx, cfg, t, out, isTTY)
	}
	return errs.New(errs.UnsupportedModel, "model type %q cannot be served yet", t.res.Model.Type)
}

// runningModel is a started engine, whatever the model type, as far as the
// HTTP lifecycle is concerned.
type runningModel struct {
	// Kind is the model type, for messages.
	Kind   string
	Engine string
	Device string
	// Mount registers the type's routes.
	Mount api.Mount
	// Done is closed when the engine exits.
	Done <-chan struct{}
	// StderrTail returns recent engine stderr for crash reports (optional).
	StderrTail func(n int) []string
	// Fail makes queued and later requests fail with err after a crash.
	Fail func(err error)
	// Drain cancels queued work and waits for the running request.
	Drain func()
	// Close stops the engine.
	Close func()
}

func serveHTTP(ctx context.Context, cfg config.Serve, out io.Writer, m runningModel) error {
	if m.Fail == nil {
		m.Fail = func(error) {}
	}
	if m.Drain == nil {
		m.Drain = func() {}
	}
	level := slog.LevelWarn
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	state := &runnerState{}
	state.v.Store("ready")
	srv := &http.Server{
		Handler:           api.NewRouter(m.Mount, state, log),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		m.Drain()
		m.Close()
		return fmt.Errorf("cannot listen on %s: %w", cfg.Addr(), err)
	}
	fmt.Fprintf(out, "Ready\n\nEngine   %s\nDevice   %s\nVRAM     %s\nAPI      http://%s\n", m.Engine, m.Device, runtime.VRAMUsage(ctx), ln.Addr())

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()

	var result error
	select {
	case <-ctx.Done():
		fmt.Fprintln(out, "\nShutting down...")
	case <-m.Done:
		state.v.Store("crashed")
		crash := errs.New(errs.RuntimeCrashed, "the %s engine exited unexpectedly", m.Kind)
		if m.StderrTail != nil {
			crash.Message += runtime.FormatTail(m.StderrTail(20))
		}
		m.Fail(crash)
		result = crash
	case err := <-srvErr:
		result = fmt.Errorf("http server: %w", err)
	}

	// 1. stop accepting requests; in-flight handlers get a bounded grace.
	state.v.Store("stopping")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() { _ = srv.Shutdown(shCtx) }()
	// 2. cancel queued work and wait for the running pass.
	m.Drain()
	_ = srv.Shutdown(shCtx)
	cancel()
	// 3-6. close engine stdin, wait, SIGTERM, SIGKILL.
	m.Close()
	if result == nil {
		if err := <-srvErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	}
	return result
}

type runnerState struct{ v atomic.Value }

func (r *runnerState) RunnerState() string { return r.v.Load().(string) }
