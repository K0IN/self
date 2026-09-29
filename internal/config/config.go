// Package config resolves server options with precedence
// CLI flags > environment variables > defaults.
package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Serve holds options for `self serve`.
type Serve struct {
	Model                 string
	Host                  string
	Port                  int
	Quant                 string
	ModelsDir             string
	Device                string
	QueueSize             int
	PreprocessConcurrency int
	RuntimeDir            string
	Registry              string
	AllowHTTPImages       bool
	AllowPrivateImages    bool
	Verbose               bool
	// Set holds `--set key=value` engine setting overrides (win over the
	// registry and the local settings file).
	Set []string
	// SettingsFile is the local settings file. Empty = default
	// (~/.ai-server/settings.yml, optional). SettingsFileExplicit is true
	// when set by flag or env (then it must exist).
	SettingsFile         string
	SettingsFileExplicit bool
}

const DefaultRegistry = "https://k0in.github.io/self/models.yml"

// multiFlag collects repeated string flags.
type multiFlag struct{ v *[]string }

func (m multiFlag) String() string {
	if m.v == nil {
		return ""
	}
	return strings.Join(*m.v, ",")
}
func (m multiFlag) Set(s string) error { *m.v = append(*m.v, s); return nil }

// Addr returns host:port.
func (s Serve) Addr() string { return netJoin(s.Host, s.Port) }

func netJoin(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}

// DefaultModelsDir is ~/.ai-server/models.
func DefaultModelsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ai-server", "models")
	}
	return filepath.Join(home, ".ai-server", "models")
}

var deviceRE = regexp.MustCompile(`^(auto|cpu|cuda|cuda:\d+|metal|vulkan|vulkan:\d+)$`)

// ParseServe parses `self serve` arguments. getenv is os.Getenv in
// production and injectable in tests.
func ParseServe(args []string, getenv func(string) string, stderr io.Writer) (Serve, error) {
	s := Serve{
		Host:                  "127.0.0.1",
		Port:                  8080,
		ModelsDir:             DefaultModelsDir(),
		Registry:              DefaultRegistry,
		Device:                "auto",
		QueueSize:             64,
		PreprocessConcurrency: 8,
	}
	// environment overrides defaults
	if v := getenv("AI_SERVER_HOST"); v != "" {
		s.Host = v
	}
	if v := getenv("AI_SERVER_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return s, fmt.Errorf("AI_SERVER_PORT: invalid port %q", v)
		}
		s.Port = p
	}
	if v := getenv("AI_SERVER_MODELS"); v != "" {
		s.ModelsDir = v
	}
	if v := getenv("AI_SERVER_RUNTIME_DIR"); v != "" {
		s.RuntimeDir = v
	}
	if v := getenv("AI_SERVER_SETTINGS"); v != "" {
		s.SettingsFile = v
	}
	if v := getenv("AI_SERVER_REGISTRY"); v != "" {
		s.Registry = v
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&s.Host, "host", s.Host, "listen host (env AI_SERVER_HOST)")
	fs.IntVar(&s.Port, "port", s.Port, "listen port (env AI_SERVER_PORT)")
	fs.StringVar(&s.Quant, "quant", "", "quantization (default: registry default)")
	fs.StringVar(&s.ModelsDir, "models-dir", s.ModelsDir, "model storage directory (env AI_SERVER_MODELS)")
	fs.StringVar(&s.Device, "device", s.Device, "auto | cpu | cuda | cuda:N | metal | vulkan | vulkan:N")
	fs.IntVar(&s.QueueSize, "queue-size", s.QueueSize, "max ready requests waiting for the model")
	fs.IntVar(&s.PreprocessConcurrency, "preprocess-concurrency", s.PreprocessConcurrency, "concurrent image fetch/decode/resize jobs")
	fs.StringVar(&s.RuntimeDir, "runtime-dir", s.RuntimeDir, "engine directory override (development; env AI_SERVER_RUNTIME_DIR)")
	fs.StringVar(&s.Registry, "registry", "", "registry file override (default: bundled registry)")
	fs.BoolVar(&s.AllowHTTPImages, "allow-http-images", false, "allow plain http:// image URLs")
	fs.BoolVar(&s.AllowPrivateImages, "allow-private-images", false, "allow image URLs resolving to private/loopback addresses")
	fs.BoolVar(&s.Verbose, "verbose", false, "show engine logs and request logs")
	fs.BoolVar(&s.Verbose, "v", false, "shorthand for --verbose")
	fs.StringVar(&s.SettingsFile, "settings-file", s.SettingsFile, "local engine settings file (default ~/.ai-server/settings.yml; env AI_SERVER_SETTINGS)")
	fs.Var(multiFlag{&s.Set}, "set", "engine setting override key=value (repeatable), e.g. --set context_size=4096")

	// Allow flags before and after the model argument.
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return s, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) != 1 {
		return s, fmt.Errorf("expected exactly one model, e.g. `self serve kev:4b`")
	}
	s.Model = positional[0]
	if s.Port < 1 || s.Port > 65535 {
		return s, fmt.Errorf("invalid port %d", s.Port)
	}
	if !deviceRE.MatchString(s.Device) {
		return s, fmt.Errorf("invalid device %q (auto, cpu, cuda, cuda:N, metal, vulkan, vulkan:N)", s.Device)
	}
	if s.QueueSize < 1 || s.QueueSize > 100000 {
		return s, fmt.Errorf("queue-size must be between 1 and 100000")
	}
	if s.PreprocessConcurrency < 1 || s.PreprocessConcurrency > 1024 {
		return s, fmt.Errorf("preprocess-concurrency must be between 1 and 1024")
	}
	for _, kv := range s.Set {
		if k, _, ok := strings.Cut(kv, "="); !ok || strings.TrimSpace(k) == "" {
			return s, fmt.Errorf("--set %q: want key=value", kv)
		}
	}
	s.ModelsDir = expandHome(s.ModelsDir)
	s.RuntimeDir = expandHome(s.RuntimeDir)
	s.SettingsFileExplicit = s.SettingsFile != ""
	s.SettingsFile = expandHome(s.SettingsFile)
	return s, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
