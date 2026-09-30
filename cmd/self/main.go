// Command self is a local AI model server.
//
//	self serve <model> [flags]
//	self ls
//	self ls-remote
//	self rm <model>[@quant] [--models-dir DIR]
//	self pull <model> [--quant Q] [--models-dir DIR]
//	self benchmark <model> [flags]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"ai-server/internal/app"
	"ai-server/internal/config"
	"ai-server/internal/errs"
)

const usage = `self — local AI model server

Usage:
  self serve <model> [flags]      Download (if needed) and serve a model
  self pull <model> [flags]       Only download a model
  self ls                         List downloaded models (alias: list)
  self ls-remote                  List registry models, grouped by type (alias: list-remote)
  self rm <model>[@quant]         Delete a downloaded model, all quants unless one is given (alias: remove)
  self settings <model>           Show effective engine settings and where they come from
  self check <model> [flags]      Download, start the engine and run probe questions
  self benchmark <model> [flags]  Time the model on this machine and write a report you can share

Benchmark flags (plus --quant, --models-dir, --device, --runtime-dir, --set, --settings-file):
  --iterations N                  timed requests per scenario (default 20)
  --warmup N                      untimed requests before each scenario (default 2)
  --out PATH                      report file or directory (default: current directory)
  --gpu NAME                      GPU name to record when it cannot be detected (non-NVIDIA)

Serve flags:
  --host HOST                     listen host (default 127.0.0.1, env AI_SERVER_HOST)
  --port PORT                     listen port (default 8080, env AI_SERVER_PORT)
  --quant Q                       quantization (default: registry default)
  --models-dir DIR                model directory (default ~/.ai-server/models, env AI_SERVER_MODELS)
  --device D                      auto | cpu | cuda | cuda:N | metal | vulkan | vulkan:N
  --queue-size N                  ready-request queue size (default 64)
  --preprocess-concurrency N      concurrent image preprocessing (default 8)
  --runtime-dir DIR               engine directory override (development)
  --allow-http-images             allow plain http:// image URLs
  --allow-private-images          allow image URLs on private/loopback networks
  -v, --verbose                   show engine and request logs
  --set key=value                 engine setting override (repeatable)
  --settings-file FILE            local settings (default ~/.ai-server/settings.yml, env AI_SERVER_SETTINGS)

Example:
  self serve kev:4b
`

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := &cobra.Command{
		Use:           "self",
		Short:         "Local AI model server",
		Long:          usage,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	lsCommand := command("ls", "List downloaded models", list)
	lsCommand.Aliases = []string{"list"}
	lsRemoteCommand := command("ls-remote", "List registry models, grouped by type", listRemote)
	lsRemoteCommand.Aliases = []string{"list-remote"}
	rmCommand := command("rm", "Delete a downloaded model", remove)
	rmCommand.Aliases = []string{"remove"}
	root.AddCommand(
		command("serve", "Download (if needed) and serve a model", func(args []string) error { return serve(ctx, args) }),
		command("pull", "Only download a model", func(args []string) error { return pull(ctx, args) }),
		lsCommand,
		lsRemoteCommand,
		rmCommand,
		command("check", "Download, start, and probe a model", func(args []string) error { return checkCmd(ctx, args) }),
		command("benchmark", "Time a model and write a shareable report", func(args []string) error { return benchmarkCmd(ctx, args) }),
		command("settings", "Show effective engine settings", settingsCmd),
	)
	completion := &cobra.Command{Use: "completion", Short: "Generate shell completion scripts"}
	completion.AddCommand(
		&cobra.Command{Use: "bash", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return root.GenBashCompletion(cmd.OutOrStdout()) }},
		&cobra.Command{Use: "zsh", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return root.GenZshCompletion(cmd.OutOrStdout()) }},
		&cobra.Command{Use: "fish", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return root.GenFishCompletion(cmd.OutOrStdout(), true) }},
	)
	root.AddCommand(completion)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	err := root.Execute()
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

func command(name, short string, run func([]string) error) *cobra.Command {
	return &cobra.Command{
		Use:                name + " [flags]",
		Short:              short,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args)
		},
	}
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func serve(ctx context.Context, args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv, os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	// Terminal UX goes to stderr; stdout stays clean for scripting.
	return app.Serve(ctx, cfg, os.Stderr, isTTY(os.Stderr))
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

// benchmarkCmd takes the serve flags plus its own. The report path goes to
// stdout, everything else to stderr, so `path=$(self benchmark kev:0.5b)` works.
func benchmarkCmd(ctx context.Context, args []string) error {
	opt := app.BenchmarkOptions{Iterations: 20, Warmup: 2}
	cfg, err := config.ParseServeWith(args, os.Getenv, os.Stderr, func(fs *flag.FlagSet) {
		fs.IntVar(&opt.Iterations, "iterations", opt.Iterations, "timed requests per scenario")
		fs.IntVar(&opt.Warmup, "warmup", opt.Warmup, "untimed requests before each scenario")
		fs.StringVar(&opt.Out, "out", "", "report file or directory (default: current directory)")
		fs.StringVar(&opt.GPU, "gpu", "", "GPU name to record when it cannot be detected (non-NVIDIA)")
	})
	if err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return errs.New(errs.InvalidRequest, "%s", err)
	}
	if opt.Iterations < 1 || opt.Iterations > 1000 {
		return errs.New(errs.InvalidRequest, "--iterations must be between 1 and 1000")
	}
	if opt.Warmup < 0 || opt.Warmup > 100 {
		return errs.New(errs.InvalidRequest, "--warmup must be between 0 and 100")
	}
	path, err := app.Benchmark(ctx, cfg, opt, os.Stderr, isTTY(os.Stderr))
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}
