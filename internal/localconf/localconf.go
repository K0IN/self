// Package localconf reads the user's local settings file
// (~/.ai-server/settings.yml by default). It lets users override engine
// settings on their machine without editing the public registry:
//
//	version: 1
//	adapters:                      # all models using this adapter
//	  ggmlc-custom-decider:
//	    threads: 8
//	models:
//	  decider:2b-vision:
//	    settings:                  # all quants of this model
//	      context_size: 4096
//	    quants:
//	      8bit:                    # one quant
//	        flash_attn: "on"
//
// Precedence (low -> high): registry model, registry quant, local adapter,
// local model, local quant, `--set`.
package localconf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// FileName is the default file name inside ~/.ai-server.
const FileName = "settings.yml"

// Config is a parsed local settings file.
type Config struct {
	// Path is where the file was read from ("" when no file exists).
	Path     string
	Adapters map[string]map[string]any
	Models   map[string]Model
}

// Model holds per-model overrides.
type Model struct {
	Settings map[string]any            `yaml:"settings"`
	Quants   map[string]map[string]any `yaml:"quants"`
}

// DefaultPath is ~/.ai-server/settings.yml.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ai-server", FileName)
	}
	return filepath.Join(home, ".ai-server", FileName)
}

// Load reads path. A missing file is not an error when optional is true
// (the default location); it returns an empty Config.
func Load(path string, optional bool) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && optional {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("local settings: %w", err)
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

var keyRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Parse parses the YAML. Only the shape is checked here; values are
// validated against the adapter schema when a model is resolved.
func Parse(data []byte) (*Config, error) {
	var doc struct {
		Version  int                       `yaml:"version"`
		Adapters map[string]map[string]any `yaml:"adapters"`
		Models   map[string]Model          `yaml:"models"`
	}
	dec := yaml.NewDecoder(bytesReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil && err.Error() != "EOF" {
		return nil, fmt.Errorf("local settings: %w", err)
	}
	if doc.Version != 0 && doc.Version != 1 {
		return nil, fmt.Errorf("local settings: unsupported version %d (want 1)", doc.Version)
	}
	c := &Config{Adapters: doc.Adapters, Models: doc.Models}
	for a, s := range c.Adapters {
		if err := checkKeys(s); err != nil {
			return nil, fmt.Errorf("adapters.%s: %w", a, err)
		}
	}
	for id, m := range c.Models {
		if err := checkKeys(m.Settings); err != nil {
			return nil, fmt.Errorf("models.%s.settings: %w", id, err)
		}
		for q, s := range m.Quants {
			if err := checkKeys(s); err != nil {
				return nil, fmt.Errorf("models.%s.quants.%s: %w", id, q, err)
			}
		}
	}
	return c, nil
}

func checkKeys(s map[string]any) error {
	for k, v := range s {
		if !keyRE.MatchString(k) {
			return fmt.Errorf("invalid key %q (use snake_case)", k)
		}
		switch v.(type) {
		case int, int64, uint64, float64, bool, string:
		default:
			return fmt.Errorf("%s must be a number, bool or string", k)
		}
	}
	return nil
}

// Layer is one named set of settings, for precedence and display.
type Layer struct {
	Name   string // e.g. "local adapter"
	Values map[string]any
}

// Layers returns the local layers for a model/quant/adapter, low to high.
func (c *Config) Layers(id, quant, adapter string) []Layer {
	if c == nil {
		return nil
	}
	var out []Layer
	if s := c.Adapters[adapter]; len(s) > 0 {
		out = append(out, Layer{"local adapter " + adapter, s})
	}
	if m, ok := c.Models[id]; ok {
		if len(m.Settings) > 0 {
			out = append(out, Layer{"local model", m.Settings})
		}
		if s := m.Quants[quant]; len(s) > 0 {
			out = append(out, Layer{"local quant " + quant, s})
		}
	}
	return out
}
