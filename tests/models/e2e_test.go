package models_test

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	appconfig "ai-server/internal/config"
	imodels "ai-server/internal/models"
	"ai-server/internal/registry"
)

// e2eConfig is read from SELF_TEST_* environment variables, so the tests can
// be driven by `just test-models` and `just test-model <model>`.
type e2eConfig struct {
	quants, device, bin, engineDir, modelsDir, registry string
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadE2EConfig(t *testing.T) e2eConfig {
	t.Helper()
	cfg := e2eConfig{
		quants:    getenv("SELF_TEST_QUANTS", "default"),
		device:    getenv("SELF_TEST_DEVICE", "auto"),
		bin:       getenv("SELF_TEST_BIN", "../../bin/self"),
		engineDir: getenv("SELF_TEST_ENGINE_DIR", "../../bin/libexec/ai-server"),
		modelsDir: getenv("SELF_TEST_MODELS_DIR", getenv("AI_SERVER_MODELS", appconfig.DefaultModelsDir())),
		registry:  getenv("SELF_TEST_REGISTRY", appconfig.DefaultRegistry(os.Getenv)),
	}
	// The engine runs from its own directory, so relative paths would break.
	paths := []*string{&cfg.bin, &cfg.engineDir, &cfg.modelsDir}
	if !strings.Contains(cfg.registry, "://") {
		paths = append(paths, &cfg.registry)
	}
	for _, p := range paths {
		abs, err := filepath.Abs(*p)
		if err != nil {
			t.Fatal(err)
		}
		*p = abs
	}
	if _, err := os.Stat(cfg.bin); err != nil {
		t.Fatalf("%v\nbuild it first: just build", err)
	}
	return cfg
}

// TestModels downloads (once) and serves each selected registry model with
// the real `self` and engine, then checks that the API answers correctly.
// It is skipped unless SELF_TEST_MODELS is set, see tests/models/README.md.
func TestModels(t *testing.T) {
	selection := os.Getenv("SELF_TEST_MODELS")
	if selection == "" {
		t.Skip("downloads models and starts the real engines; run `just test-models` or `just test-model <model>`")
	}
	if selection == "all" {
		selection = "*"
	}
	cfg := loadE2EConfig(t)
	reg, err := loadRegistry(cfg.registry)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := selectTargets(reg, selection, cfg.quants)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range targets {
		t.Run(res.ID()+"@"+res.Variant.Quant, func(t *testing.T) { testModel(t, cfg, res) })
	}
}

func testModel(t *testing.T, cfg e2eConfig, res registry.Resolved) {
	ensureDownloaded(t, cfg, res)

	started := time.Now()
	proc, err := startServe(cfg.bin, []string{
		"serve", res.ID(), "--quant", res.Variant.Quant, "--models-dir", cfg.modelsDir,
		"--runtime-dir", cfg.engineDir, "--device", cfg.device, "--registry", cfg.registry,
	}, nil)
	if err != nil {
		t.Fatalf("cannot start self serve: %v", err)
	}
	t.Cleanup(func() {
		select {
		case <-proc.exited:
		default:
			_ = proc.stop(10 * time.Second)
		}
		if t.Failed() {
			t.Logf("self serve log:\n%s", proc.log.tail(40))
		}
	})
	if err := proc.waitReady(t.Context(), 10*time.Minute); err != nil {
		t.Fatalf("model did not load: %v", err)
	}
	t.Logf("loaded in %.1fs", time.Since(started).Seconds())

	e := newEnv(t.Context(), newClient(proc.base), res)
	for _, c := range checksFor(e) {
		t.Run(c.name, func(t *testing.T) {
			status, detail := runCheck(t.Context(), e, c)
			switch status {
			case statusSkip:
				t.Skip(detail)
			case statusFail:
				t.Error(detail)
			default:
				if detail != "" {
					t.Log(detail)
				}
			}
		})
	}
	t.Run("lifecycle: graceful shutdown", func(t *testing.T) {
		if err := proc.stop(20 * time.Second); err != nil {
			t.Error(err)
		}
	})
}

// ensureDownloaded pulls the model unless its files are already in the model
// directory, so every model is downloaded once and reused by later runs.
func ensureDownloaded(t *testing.T, cfg e2eConfig, res registry.Resolved) {
	t.Helper()
	if installed(imodels.Store{Root: cfg.modelsDir}, res) {
		t.Logf("model files already in %s", cfg.modelsDir)
		return
	}
	t.Logf("downloading %.2f GiB", float64(res.Variant.Size())/(1<<30))
	cmd := exec.CommandContext(t.Context(), cfg.bin, "pull", res.ID(), "--quant", res.Variant.Quant, "--models-dir", cfg.modelsDir, "--registry", cfg.registry)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("download failed: %v", err)
	}
}

func installed(store imodels.Store, res registry.Resolved) bool {
	for _, f := range res.Variant.Files {
		if !store.Installed(res, f) {
			return false
		}
	}
	return true
}

// loadRegistry reads the registry `self` is started with: a URL or a file.
func loadRegistry(source string) (*registry.Registry, error) {
	if strings.Contains(source, "://") {
		reg, _, err := registry.Fetch(&http.Client{Timeout: 30 * time.Second}, source)
		return reg, err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	return registry.Parse(data)
}

// selectTargets expands the model patterns (ids or globs, optionally with
// @quant) and the quants setting (default, all or a list) into variants,
// sorted by id then quant.
func selectTargets(reg *registry.Registry, patterns, quants string) ([]registry.Resolved, error) {
	type ref struct{ id, quant string }
	seen := map[ref]bool{}
	var refs []ref
	add := func(id, quant string) {
		if r := (ref{id, quant}); !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}

	pats := splitList(patterns)
	if len(pats) == 0 {
		pats = []string{"*"}
	}
	for _, p := range pats {
		idPat, quant, _ := strings.Cut(p, "@")
		matched := false
		for _, id := range reg.IDs() {
			modelType := reg.Models[id].Type
			if modelType != registry.TypeDecision && modelType != registry.TypeEmbedding {
				continue
			}
			ok, err := path.Match(idPat, id)
			if err != nil {
				return nil, fmt.Errorf("bad model pattern %q: %w", p, err)
			}
			if !ok {
				continue
			}
			matched = true
			m := reg.Models[id]
			switch {
			case quant != "":
				add(id, quant)
			case quants == "all":
				for _, q := range m.Quants() {
					add(id, q)
				}
			case quants == "default" || quants == "":
				add(id, m.Default)
			default:
				for _, q := range splitList(quants) {
					if _, ok := m.Variants[q]; ok {
						add(id, q)
					}
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("no registry model matches %q (see `self ls-remote`)", idPat)
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].id != refs[j].id {
			return refs[i].id < refs[j].id
		}
		return refs[i].quant < refs[j].quant
	})
	out := make([]registry.Resolved, 0, len(refs))
	for _, r := range refs {
		res, err := reg.Resolve(r.id, registry.ResolveOptions{Quant: r.quant})
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	if len(out) == 0 {
		return nil, errors.New("nothing selected: check SELF_TEST_MODELS and SELF_TEST_QUANTS")
	}
	return out, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
