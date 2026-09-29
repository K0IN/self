// Package registry loads the human-editable YAML model registry and resolves
// model references ("kev:4b", "decider:2b-vision") to concrete, typed
// variants. It knows nothing about inference.
package registry

import (
	"fmt"
	"sort"
	"strings"
)

// ModelType is the serving mode of a model.
type ModelType string

const (
	TypeDecision ModelType = "decision"
	// Future: TypeImage, TypeTTS, TypeSTT, TypeLLM, TypeEmbedding.
)

func parseModelType(s string) (ModelType, error) {
	switch ModelType(s) {
	case TypeDecision:
		return TypeDecision, nil
	}
	return "", fmt.Errorf("unknown model type %q (supported: decision)", s)
}

// Capability is a typed registry capability.
type Capability string

const (
	CapText       Capability = "text"
	CapVision     Capability = "vision"
	CapMultiImage Capability = "multi-image"
	CapChoice     Capability = "choice"
	CapScore      Capability = "score"
	CapNoul       Capability = "noul"
)

// capabilitiesByType lists the capability strings valid for each model type.
var capabilitiesByType = map[ModelType][]Capability{
	TypeDecision: {CapText, CapVision, CapMultiImage, CapChoice, CapScore, CapNoul},
}

// Capabilities separates accepted input features from produced output types.
type Capabilities struct {
	Input  CapabilitySet
	Output CapabilitySet
	// MaxImages is the registry-declared image limit (0 = unspecified).
	MaxImages int
}

type CapabilitySet struct{ set map[Capability]bool }

func (c Capabilities) Has(cap Capability) bool  { return c.Input.Has(cap) || c.Output.Has(cap) }
func (c CapabilitySet) Has(cap Capability) bool { return c.set[cap] }

// List returns the declared capabilities, sorted.
func (c CapabilitySet) List() []Capability {
	out := make([]Capability, 0, len(c.set))
	for k := range c.set {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (c Capabilities) List() []Capability {
	seen := map[Capability]bool{}
	for _, cap := range c.Input.List() {
		seen[cap] = true
	}
	for _, cap := range c.Output.List() {
		seen[cap] = true
	}
	out := make([]Capability, 0, len(seen))
	for cap := range seen {
		out = append(out, cap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func parseCapabilities(t ModelType, input, output []string, maxImages int) (Capabilities, error) {
	allowed := map[Capability]bool{}
	for _, c := range capabilitiesByType[t] {
		allowed[c] = true
	}
	caps := Capabilities{Input: CapabilitySet{set: map[Capability]bool{}}, Output: CapabilitySet{set: map[Capability]bool{}}, MaxImages: maxImages}
	parse := func(raw []string, dst *CapabilitySet) error {
		for _, s := range raw {
			c := Capability(strings.TrimSpace(strings.ToLower(s)))
			if !allowed[c] {
				return fmt.Errorf("unknown %s capability %q", t, s)
			}
			dst.set[c] = true
		}
		return nil
	}
	if err := parse(input, &caps.Input); err != nil {
		return caps, err
	}
	if err := parse(output, &caps.Output); err != nil {
		return caps, err
	}
	if caps.Input.Has(CapMultiImage) {
		caps.Input.set[CapVision] = true
	}
	if maxImages < 0 {
		return caps, fmt.Errorf("max_images must be >= 0")
	}
	return caps, nil
}

// FileRole describes what a model file is used for.
type FileRole string

const (
	RoleModel  FileRole = "model"
	RoleMMProj FileRole = "mmproj"
)

// File is one downloadable artifact of a variant. Size and SHA256 pin the
// exact upstream bytes; downloads that do not match are rejected.
type File struct {
	Name   string
	Role   FileRole
	Size   int64  // bytes, > 0
	SHA256 string // lowercase hex, 64 chars
}

// Variant is one quantization of a model.
type Variant struct {
	Quant   string
	Adapter string
	Repo    string
	Files   []File
	// Settings override the model-level settings for this quant.
	Settings map[string]any
}

// Info is descriptive model metadata. It is shown by `self list`, the API
// and the registry site; it does not change how the engine runs (use
// settings for that).
type Info struct {
	Family     string   `yaml:"family" json:"family,omitempty"`
	Parameters string   `yaml:"parameters" json:"parameters,omitempty"`     // e.g. "0.5B"
	Arch       string   `yaml:"architecture" json:"architecture,omitempty"` // e.g. "qwen2.5", "modernbert"
	BaseModel  string   `yaml:"base_model" json:"base_model,omitempty"`     // HF repo
	Source     string   `yaml:"source" json:"source,omitempty"`             // original (non-GGUF) HF repo
	License    string   `yaml:"license" json:"license,omitempty"`
	Languages  []string `yaml:"languages" json:"languages,omitempty"`
	// ContextLength is the maximum input length in tokens the model was
	// trained/compiled for.
	ContextLength int `yaml:"context_length" json:"context_length,omitempty"`
	// MaxOptions is the most options per question the model supports.
	MaxOptions int    `yaml:"max_options" json:"max_options,omitempty"`
	Homepage   string `yaml:"homepage" json:"homepage,omitempty"`
}

func (i Info) validate() error {
	if i.ContextLength < 0 || i.MaxOptions < 0 {
		return fmt.Errorf("context_length and max_options must be >= 0")
	}
	for _, u := range []string{i.Homepage} {
		if u != "" && !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("homepage must be an https:// URL")
		}
	}
	return nil
}

// Model is one registry entry.
type Model struct {
	ID           string
	Type         ModelType
	Default      string
	Capabilities Capabilities
	Variants     map[string]Variant
	// Description is a one-line summary (lists, overview pages).
	Description string
	// Readme is the path of the model card (Markdown), relative to the
	// registry file, e.g. "readmes/kev/4b.md".
	Readme string
	// Info is descriptive metadata (family, size, license, context, ...).
	Info Info
	// Settings are engine parameters for all quants (adapter-specific keys,
	// validated against the adapter's schema).
	Settings map[string]any
}

// EffectiveSettings merges model-level and quant-level settings (quant wins).
func (m Model) EffectiveSettings(quant string) map[string]any {
	out := map[string]any{}
	for k, v := range m.Settings {
		out[k] = v
	}
	for k, v := range m.Variants[quant].Settings {
		out[k] = v
	}
	return out
}

// Size returns the total download size of a variant in bytes.
func (v Variant) Size() int64 {
	var n int64
	for _, f := range v.Files {
		n += f.Size
	}
	return n
}

// Quants returns the variant names, sorted.
func (m Model) Quants() []string {
	out := make([]string, 0, len(m.Variants))
	for q := range m.Variants {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

// Registry is a parsed registry file.
type Registry struct {
	Version int
	Models  map[string]Model
}

// IDs returns all model ids, sorted.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.Models))
	for id := range r.Models {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
