// Command self is a local AI model server.
//
//	self serve <model> [flags]
//	self decision <model> [flags]     (requires a decision model)
//	self list
//	self pull <model> [--quant Q] [--models-dir DIR]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"

	"ai-server/internal/app"
	"ai-server/internal/config"
	"ai-server/internal/errs"
	"ai-server/internal/models"
	"ai-server/internal/onboard"
	"ai-server/internal/registry"
)

const usage = `self — local AI model server

Usage:
  self serve <model> [flags]      Download (if needed) and serve a model
  self decision <model> [flags]   Same as serve, requires a decision model
  self pull <model> [flags]       Only download a model
  self list                       List registry models
  self settings <model>           Show effective engine settings and where they come from

Onboarding new models:
  self onboard <hf-repo> [--id name:tag]   Inspect a Hugging Face GGUF repo and print a registry entry
  self check <model> [flags]               Download, start the engine and run probe questions

Serve flags:
  --host HOST                     listen host (default 127.0.0.1, env AI_SERVER_HOST)
  --port PORT                     listen port (default 8080, env AI_SERVER_PORT)
  --quant Q                       quantization (default: registry default)
  --models-dir DIR                model directory (default ~/.ai-server/models, env AI_SERVER_MODELS)
  --device D                      auto | cpu | cuda | cuda:N | metal | vulkan | vulkan:N
  --queue-size N                  ready-request queue size (default 64)
  --preprocess-concurrency N      concurrent image preprocessing (default 8)
  --runtime-dir DIR               engine directory override (development)
  --registry FILE                 registry override
  --allow-http-images             allow plain http:// image URLs
  --allow-private-images          allow image URLs on private/loopback networks
  -v, --verbose                   show engine and request logs
  --set key=value                 engine setting override (repeatable)
  --settings-file FILE            local settings (default ~/.ai-server/settings.yml, env AI_SERVER_SETTINGS)

Example:
  self serve kev:4b
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "serve":
		err = serve(ctx, args[1:], "")
	case "decision":
		err = serve(ctx, args[1:], registry.TypeDecision)
	case "pull":
		err = pull(ctx, args[1:])
	case "list", "ls":
		err = list(args[1:])
	case "onboard":
		err = onboardCmd(ctx, args[1:])
	case "check":
		err = checkCmd(ctx, args[1:])
	case "settings":
		err = settingsCmd(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		if ctx.Err() != nil && errs.Is(err, errs.DownloadFailed) {
			fmt.Fprintln(os.Stderr, "\nInterrupted. Partial downloads are kept and will resume next time.")
			return 130
		}
		fmt.Fprintf(os.Stderr, "\nError [%s]: %s\n", errs.KindOf(err), err)
		return 1
	}
	return 0
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func serve(ctx context.Context, args []string, t registry.ModelType) error {
	cfg, err := config.ParseServe(args, os.Getenv, os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	// Terminal UX goes to stderr; stdout stays clean for scripting.
	return app.Serve(ctx, cfg, t, os.Stderr, isTTY(os.Stderr))
}

func onboardCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	id := fs.String("id", "", "registry id (default: derived from the repo name)")
	modelsDir := fs.String("readme-dir", "models", "directory of registry.yml; the model card skeleton is written below it (\"\" = don't write)")
	var repo string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return errs.New(errs.InvalidRequest, "%s", err)
		}
		if fs.NArg() == 0 {
			break
		}
		repo, rest = fs.Arg(0), fs.Args()[1:]
	}
	if strings.Count(repo, "/") != 1 {
		return errs.New(errs.InvalidRequest, "usage: self onboard <owner/repo> [--id name:tag]")
	}
	fmt.Fprintf(os.Stderr, "Inspecting %s ...\n", repo)
	r, err := onboard.NewClient().Analyze(ctx, repo, *id)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Architecture  %s\nAdapter       %s\n              %s\n", r.Arch, r.Adapter, r.Reason)
	for _, w := range r.Warnings {
		fmt.Fprintf(os.Stderr, "Warning       %s\n", w)
	}
	if *modelsDir != "" {
		p := filepath.Join(*modelsDir, filepath.FromSlash(r.ReadmePath()))
		if _, err := os.Stat(p); err == nil {
			fmt.Fprintf(os.Stderr, "Readme        %s (exists, kept)\n", p)
		} else if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil && os.WriteFile(p, []byte(r.Readme()), 0o644) == nil {
			fmt.Fprintf(os.Stderr, "Readme        %s (skeleton written, please edit)\n", p)
		}
	}
	fmt.Fprintf(os.Stderr, "\nAdd this to models/registry.yml, then run: self check %s\n\n", r.ID)
	fmt.Print(r.YAML())
	return nil
}

func settingsCmd(args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv, os.Stderr)
	if err != nil {
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	return app.Settings(cfg, os.Stdout)
}

func checkCmd(ctx context.Context, args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv, os.Stderr)
	if err != nil {
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	return app.Check(ctx, cfg, os.Stderr, isTTY(os.Stderr))
}

func pull(ctx context.Context, args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv, os.Stderr)
	if err != nil {
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	reg, err := app.LoadRegistry(cfg.Registry)
	if err != nil {
		return err
	}
	res, err := reg.Resolve(cfg.Model, registry.ResolveOptions{Quant: cfg.Quant})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Model    %s\nQuant    %s\n", res.ID(), res.Variant.Quant)
	files, err := models.Ensure(ctx, models.Store{Root: cfg.ModelsDir}, models.NewDownloader(), res, func() models.Progress {
		fmt.Fprintln(os.Stderr)
		return &models.TerminalProgress{W: os.Stderr, TTY: isTTY(os.Stderr)}
	})
	if err != nil {
		return err
	}
	fmt.Println(files.Model)
	if files.MMProj != "" {
		fmt.Println(files.MMProj)
	}
	return nil
}

func list(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	regPath := fs.String("registry", "", "registry override")
	modelsDir := fs.String("models-dir", config.DefaultModelsDir(), "model directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := app.LoadRegistry(*regPath)
	if err != nil {
		return err
	}
	store := models.Store{Root: *modelsDir}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tTYPE\tQUANTS\tSIZE\tCAPABILITIES\tDOWNLOADED\tDESCRIPTION")
	for _, id := range reg.IDs() {
		m := reg.Models[id]
		var quants, have []string
		for _, q := range m.Quants() {
			label := q
			if q == m.Default {
				label += "*"
			}
			quants = append(quants, label)
			res := registry.Resolved{Model: m, Variant: m.Variants[q]}
			ok := true
			for _, f := range res.Variant.Files {
				ok = ok && store.Installed(res, f)
			}
			if ok {
				have = append(have, q)
			}
		}
		var caps []string
		for _, c := range m.Capabilities.List() {
			caps = append(caps, string(c))
		}
		size := models.FormatBytes(m.Variants[m.Default].Size())
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", id, m.Type, strings.Join(quants, ","), size, strings.Join(caps, ","), strings.Join(have, ","), m.Description)
	}
	return w.Flush()
}
