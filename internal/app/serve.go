// Package app wires registry, downloads, engine adapter, service and HTTP
// server together for `self serve`.
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
	"strings"
	"sync/atomic"
	"time"

	"ai-server/internal/adapters"
	"ai-server/internal/api"
	apidecision "ai-server/internal/api/decision"
	"ai-server/internal/config"
	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/imageutil"
	"ai-server/internal/localconf"
	"ai-server/internal/models"
	"ai-server/internal/registry"
	"ai-server/internal/runtime"
	"ai-server/internal/settings"
	bundled "ai-server/models"
)

// LoadRegistry loads the override file or the embedded registry.
func LoadRegistry(path string) (*registry.Registry, error) {
	if path != "" {
		return registry.LoadFile(path)
	}
	return registry.Parse(bundled.Registry)
}

// Serve runs `self serve` until ctx is cancelled or the engine dies.
// requireType, when set, enforces the model type (used by `self decision`).
func Serve(ctx context.Context, cfg config.Serve, requireType registry.ModelType, out io.Writer, isTTY bool) error {
	reg, err := LoadRegistry(cfg.Registry)
	if err != nil {
		return err
	}
	res, err := reg.Resolve(cfg.Model, registry.ResolveOptions{Quant: cfg.Quant, Type: requireType, AdapterKnown: adapters.Known})
	if err != nil {
		return err
	}
	switch res.Model.Type {
	case registry.TypeDecision:
		return serveDecision(ctx, cfg, res, out, isTTY)
	}
	return errs.New(errs.UnsupportedModel, "model type %q cannot be served yet", res.Model.Type)
}

func serveDecision(ctx context.Context, cfg config.Serve, res registry.Resolved, out io.Writer, isTTY bool) error {
	entry, err := adapters.Decision(res.Variant.Adapter)
	if err != nil {
		return err
	}
	engine, err := runtime.Find(entry.Engine, runtime.SearchDirs(cfg.RuntimeDir))
	if err != nil {
		return err
	}

	set, _, err := ResolveSettings(cfg, res)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Model    %s\nQuant    %s\nAdapter  %s\n", res.ID(), res.Variant.Quant, res.Variant.Adapter)
	if len(set) > 0 {
		fmt.Fprintf(out, "Settings %s\n", settings.Format(set))
	}

	store := models.Store{Root: cfg.ModelsDir}
	files, err := models.Ensure(ctx, store, models.NewDownloader(), res, func() models.Progress {
		fmt.Fprintln(out)
		return &models.TerminalProgress{W: out, TTY: isTTY}
	})
	if err != nil {
		return err
	}

	var engineLog io.Writer
	if cfg.Verbose {
		engineLog = os.Stderr
	}
	adapter := entry.New()
	fmt.Fprintf(out, "\nUsing %s\n\nLoading model...\n", tildify(files.Model))
	startCtx, cancelStart := context.WithTimeout(ctx, 10*time.Minute)
	err = adapter.Start(startCtx, decision.RuntimeConfig{
		ModelID:    res.ID(),
		Files:      files,
		Device:     cfg.Device,
		EnginePath: engine.Path,
		LibDir:     engine.LibDir,
		Log:        engineLog,
		Settings:   set,
	})
	cancelStart()
	if err != nil {
		return err
	}
	closeAdapter := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = adapter.Close(c)
	}

	info, err := adapter.Info(ctx)
	if err != nil {
		closeAdapter()
		return err
	}
	caps := modelCaps(res).Intersect(info.Capabilities)
	if res.Model.Capabilities.Has(registry.CapVision) && !caps.Vision {
		closeAdapter()
		return errs.New(errs.UnsupportedModel, "%s declares vision, but the %s engine reports no image input", res.ID(), res.Variant.Adapter)
	}
	if caps.Vision && info.ImageInput == nil {
		closeAdapter()
		return errs.New(errs.RuntimeStartFailed, "engine reports vision support but no image input geometry")
	}

	lim := imageutil.DefaultLimits()
	lim.AllowHTTP, lim.AllowPrivate = cfg.AllowHTTPImages, cfg.AllowPrivateImages
	svc := decision.NewService(decision.ServiceConfig{
		ModelID:               res.ID(),
		Quant:                 res.Variant.Quant,
		Capabilities:          caps,
		ImageInput:            info.ImageInput,
		QueueSize:             cfg.QueueSize,
		PreprocessConcurrency: cfg.PreprocessConcurrency,
		Info:                  res.Model.Info,
		Settings:              set,
	}, adapter, imageutil.NewPreprocessor(lim))

	level := slog.LevelWarn
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	state := &runnerState{}
	state.v.Store("ready")
	srv := &http.Server{
		Handler:           api.NewRouter(apidecision.New(svc), state, log),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		svc.Close()
		closeAdapter()
		return fmt.Errorf("cannot listen on %s: %w", cfg.Addr(), err)
	}
	fmt.Fprintf(out, "Ready\n\nEngine   %s\nDevice   %s\nVRAM     %s\nAPI      http://%s\n", engine.Path, info.Device, runtime.VRAMUsage(ctx), ln.Addr())

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()

	var result error
	select {
	case <-ctx.Done():
		fmt.Fprintln(out, "\nShutting down...")
	case <-adapter.Done():
		state.v.Store("crashed")
		crash := errs.New(errs.RuntimeCrashed, "the decision engine exited unexpectedly")
		if t, ok := adapter.(interface{ StderrTail(int) []string }); ok {
			crash.Message += runtime.FormatTail(t.StderrTail(20))
		}
		svc.Fail(crash)
		result = crash
	case err := <-srvErr:
		result = fmt.Errorf("http server: %w", err)
	}

	// 1. stop accepting requests; in-flight handlers get a bounded grace.
	state.v.Store("stopping")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() { _ = srv.Shutdown(shCtx) }()
	// 2. cancel queued work and wait for the running pass.
	svc.Close()
	_ = srv.Shutdown(shCtx)
	cancel()
	// 3-6. close engine stdin, wait, SIGTERM, SIGKILL.
	closeAdapter()
	if result == nil {
		if err := <-srvErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	}
	return result
}

// overrides parses --set key=value flags.
func overrides(cfg config.Serve) map[string]any {
	m, _ := settings.ParseOverrides(cfg.Set) // syntax validated by config
	return m
}

// LoadLocal reads the local settings file (default location is optional).
func LoadLocal(cfg config.Serve) (*localconf.Config, error) {
	if cfg.SettingsFileExplicit {
		return localconf.Load(cfg.SettingsFile, false)
	}
	return localconf.Load(localconf.DefaultPath(), true)
}

// ResolveSettings returns the effective engine settings for res: registry,
// then the local settings file, then --set. The map gives each key's source.
func ResolveSettings(cfg config.Serve, res registry.Resolved) (settings.Values, map[string]string, error) {
	local, err := LoadLocal(cfg)
	if err != nil {
		return nil, nil, errs.New(errs.InvalidRequest, "%s", err)
	}
	return adapters.ResolveSettingsLayered(res, local, overrides(cfg))
}

// modelCaps are the registry capabilities, capped by info.max_options.
func modelCaps(res registry.Resolved) decision.Capabilities {
	c := decision.CapabilitiesFromRegistry(res.Model.Capabilities)
	c.MaxOptions = res.Model.Info.MaxOptions
	return c
}

type runnerState struct{ v atomic.Value }

func (r *runnerState) RunnerState() string { return r.v.Load().(string) }

func tildify(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}
