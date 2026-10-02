package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-server/internal/adapters"
	"ai-server/internal/config"
	"ai-server/internal/errs"
	"ai-server/internal/localconf"
	"ai-server/internal/models"
	"ai-server/internal/registry"
	"ai-server/internal/runtime"
	"ai-server/internal/settings"
)

// target is the model named on the command line, resolved against the
// registry, with the adapter and settings it will run with. It holds
// everything that can be known before files are downloaded or an engine
// starts, and nothing specific to a model type.
type target struct {
	res      registry.Resolved
	adapter  adapters.Base
	settings settings.Values
	// source names, per setting key, the layer its value came from.
	source map[string]string
}

func resolveTarget(cfg config.Serve) (target, error) {
	reg, err := LoadRegistry(cfg.Registry, cfg.ModelsDir)
	if err != nil {
		return target{}, err
	}
	res, err := reg.Resolve(cfg.Model, registry.ResolveOptions{Quant: cfg.Quant, AdapterKnown: adapters.Known})
	if err != nil {
		return target{}, err
	}
	base, err := adapters.Lookup(res.Model.Type, res.Variant.Adapter)
	if err != nil {
		return target{}, err
	}
	set, source, err := ResolveSettings(cfg, res)
	if err != nil {
		return target{}, err
	}
	return target{res: res, adapter: base, settings: set, source: source}, nil
}

// locate finds the adapter's engine executable.
func (t target) locate(cfg config.Serve) (runtime.Engine, error) {
	return runtime.Find(t.adapter.Engine, runtime.SearchDirs(cfg.RuntimeDir))
}

// describe prints what is about to run; engine is left out when empty.
func (t target) describe(out io.Writer, engine string) {
	fmt.Fprintf(out, "Model    %s\nQuant    %s\nAdapter  %s\n", t.res.ID(), t.res.Variant.Quant, t.res.Variant.Adapter)
	if engine != "" {
		fmt.Fprintf(out, "Engine   %s\n", engine)
	}
	if len(t.settings) > 0 {
		fmt.Fprintf(out, "Settings %s\n", settings.Format(t.settings))
	}
}

// download makes sure the files of res are local and shows progress on out.
func download(ctx context.Context, cfg config.Serve, res registry.Resolved, out io.Writer, isTTY bool) (models.Files, error) {
	return models.Ensure(ctx, models.Store{Root: cfg.ModelsDir}, models.NewDownloader(), res, func() models.Progress {
		fmt.Fprintln(out)
		return &models.TerminalProgress{W: out, TTY: isTTY}
	})
}

// Pull downloads a model without starting it, whatever its type, and returns
// the local paths of its files in registry order.
func Pull(ctx context.Context, cfg config.Serve, out io.Writer, isTTY bool) ([]string, error) {
	reg, err := LoadRegistry(cfg.Registry, cfg.ModelsDir)
	if err != nil {
		return nil, err
	}
	res, err := reg.Resolve(cfg.Model, registry.ResolveOptions{Quant: cfg.Quant})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Model    %s\nQuant    %s\n", res.ID(), res.Variant.Quant)
	if _, err := download(ctx, cfg, res, out, isTTY); err != nil {
		return nil, err
	}
	store := models.Store{Root: cfg.ModelsDir}
	paths := make([]string, len(res.Variant.Files))
	for i, f := range res.Variant.Files {
		paths[i] = store.Path(res, f)
	}
	return paths, nil
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

func tildify(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}
