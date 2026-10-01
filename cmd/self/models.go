package main

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"ai-server/internal/app"
	"ai-server/internal/config"
	"ai-server/internal/errs"
	"ai-server/internal/models"
	"ai-server/internal/registry"
)

// The commands in this file manage model files and work for every model type.

func pull(ctx context.Context, args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv, os.Stderr)
	if err != nil {
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	paths, err := app.Pull(ctx, cfg, os.Stderr, isTTY(os.Stderr))
	if err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Println(p)
	}
	return nil
}

func defaultModelsDir() string {
	if v := os.Getenv("AI_SERVER_MODELS"); v != "" {
		return v
	}
	return config.DefaultModelsDir()
}

// remove deletes downloaded quants of one model; all of them unless a quant is given.
func remove(args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	modelsDir := fs.String("models-dir", defaultModelsDir(), "model directory")
	quant := fs.String("quant", "", "quantization to remove (default: all downloaded quants)")
	var refs []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if err == flag.ErrHelp {
				return nil
			}
			return errs.New(errs.InvalidRequest, "%s", err)
		}
		if fs.NArg() == 0 {
			break
		}
		refs, rest = append(refs, fs.Arg(0)), fs.Args()[1:]
	}
	if len(refs) != 1 {
		return errs.New(errs.InvalidRequest, "usage: self rm <model>[@quant] [--models-dir DIR]")
	}
	id, q, hasQuant := strings.Cut(refs[0], "@")
	if hasQuant {
		if q == "" || *quant != "" {
			return errs.New(errs.InvalidRequest, "invalid model reference %q; expected `<model>:<size>@<quant>` without --quant", refs[0])
		}
		*quant = q
	}
	removed, err := models.Store{Root: *modelsDir}.Remove(id, *quant)
	if err != nil {
		return err
	}
	if len(removed) == 0 {
		return errs.New(errs.ModelNotFound, "model %q is not downloaded (see `self ls`)", refs[0])
	}
	for _, q := range removed {
		fmt.Fprintf(os.Stderr, "Removed %s@%s\n", id, q)
	}
	return nil
}

func loadListRegistry(name string, args []string) (*registry.Registry, models.Store, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	modelsDir := fs.String("models-dir", defaultModelsDir(), "model directory")
	source := fs.String("registry", config.DefaultRegistry(os.Getenv), "registry URL or file (env AI_SERVER_REGISTRY)")
	if err := fs.Parse(args); err != nil {
		return nil, models.Store{}, err
	}
	reg, err := app.LoadRegistry(*source, *modelsDir)
	return reg, models.Store{Root: *modelsDir}, err
}

func capabilityNames(m registry.Model) string {
	var caps []string
	for _, c := range m.Capabilities.List() {
		caps = append(caps, string(c))
	}
	return strings.Join(caps, ",")
}

func shortHashes(files []registry.File) string {
	var out []string
	for _, f := range files {
		out = append(out, f.SHA256[:12])
	}
	return strings.Join(out, ",")
}

// list shows the model variants that are fully downloaded, with the registry-pinned
// sha256 (verified at download time) of each file, shortened.
func list(args []string) error {
	reg, store, err := loadListRegistry("list", args)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tQUANT\tTYPE\tADAPTER\tSIZE\tSHA256\tCAPABILITIES\tDESCRIPTION")
	found := false
	for _, id := range reg.IDs() {
		m := reg.Models[id]
		for _, q := range m.Quants() {
			res := registry.Resolved{Model: m, Variant: m.Variants[q]}
			installed := true
			for _, f := range res.Variant.Files {
				installed = installed && store.Installed(res, f)
			}
			if !installed {
				continue
			}
			found = true
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", id, q, m.Type, res.Variant.Adapter, models.FormatBytes(res.Variant.Size()), shortHashes(res.Variant.Files), capabilityNames(m), m.Description)
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "No models downloaded. Run `self ls-remote` to see what is available.")
		return nil
	}
	return w.Flush()
}

// listRemote shows every registry model, grouped by type.
func listRemote(args []string) error {
	reg, _, err := loadListRegistry("list-remote", args)
	if err != nil {
		return err
	}
	byType := map[registry.ModelType][]string{}
	for _, id := range reg.IDs() {
		t := reg.Models[id].Type
		byType[t] = append(byType[t], id)
	}
	for i, t := range slices.Sorted(maps.Keys(byType)) {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(t)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  MODEL\tQUANTS\tSIZE\tCAPABILITIES\tDESCRIPTION")
		for _, id := range byType[t] {
			m := reg.Models[id]
			var quants []string
			for _, q := range m.Quants() {
				if q == m.Default {
					q += "*"
				}
				quants = append(quants, q)
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", id, strings.Join(quants, ","), models.FormatBytes(m.Variants[m.Default].Size()), capabilityNames(m), m.Description)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if len(reg.Skipped) > 0 {
		fmt.Fprintf(os.Stderr, "\nNot shown, they need a newer self: %s\n", strings.Join(reg.Skipped, ", "))
	}
	return nil
}
