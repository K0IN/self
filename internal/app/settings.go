package app

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"ai-server/internal/adapters"
	"ai-server/internal/config"
	"ai-server/internal/localconf"
	"ai-server/internal/registry"
)

// Settings prints the effective engine settings of a model, where each value
// comes from, and every setting the adapter accepts (`self settings <id>`).
func Settings(cfg config.Serve, out io.Writer) error {
	reg, err := LoadRegistry(cfg.ModelsDir)
	if err != nil {
		return err
	}
	res, err := reg.Resolve(cfg.Model, registry.ResolveOptions{Quant: cfg.Quant, AdapterKnown: adapters.Known})
	if err != nil {
		return err
	}
	entry, err := adapters.Decision(res.Variant.Adapter)
	if err != nil {
		return err
	}
	vals, source, err := ResolveSettings(cfg, res)
	if err != nil {
		return err
	}
	local, _ := LoadLocal(cfg)
	path := local.Path
	if path == "" {
		path = localconf.DefaultPath() + " (not found)"
		if cfg.SettingsFileExplicit {
			path = cfg.SettingsFile
		}
	}
	fmt.Fprintf(out, "Model          %s\nQuant          %s\nAdapter        %s\nLocal settings %s\n\n", res.ID(), res.Variant.Quant, res.Variant.Adapter, path)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SETTING\tVALUE\tSOURCE\tTYPE\tHELP")
	names := entry.Settings.Names()
	sort.SliceStable(names, func(i, j int) bool {
		_, a := vals[names[i]]
		_, b := vals[names[j]]
		return a && !b
	})
	for _, n := range names {
		var p = entry.Settings[indexOf(entry.Settings.Names(), n)]
		v, src := "-", "engine default"
		if x, ok := vals[n]; ok {
			v, src = fmt.Sprint(x), source[n]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n, v, src, p.Kind, p.Help)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nOverride locally in %s:\n\n  models:\n    %s:\n      settings:\n        %s: <value>\n\nor per run: self serve %s --set key=value\n",
		localconf.DefaultPath(), res.ID(), firstOr(names, "key"), res.ID())
	return nil
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func firstOr(xs []string, d string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return d
}
