package registry

import (
	"strings"

	"ai-server/internal/errs"
)

// Resolved is a fully selected model variant.
type Resolved struct {
	Model   Model
	Variant Variant
}

// ID returns the registry id, e.g. "decider:2b-vision".
func (r Resolved) ID() string { return r.Model.ID }

// Name and Tag split the id at ':' ("decider", "2b-vision"). Tag may be
// empty.
func (r Resolved) Name() string { n, _, _ := strings.Cut(r.Model.ID, ":"); return n }
func (r Resolved) Tag() string  { _, t, _ := strings.Cut(r.Model.ID, ":"); return t }

// ResolveOptions controls resolution.
type ResolveOptions struct {
	// Quant overrides the default quant when non-empty.
	Quant string
	// Type, when non-empty, requires the model to be of this type.
	Type ModelType
	// AdapterKnown reports whether an adapter name is available for the
	// model's type. Nil skips the check.
	AdapterKnown func(name string, t ModelType) bool
}

// Resolve selects a model and variant.
func (r *Registry) Resolve(ref string, opt ResolveOptions) (Resolved, error) {
	ref = strings.TrimSpace(ref)
	m, ok := r.Models[ref]
	if !ok {
		return Resolved{}, errs.New(errs.ModelNotFound, "model %q is not in the registry%s", ref, r.suggest(ref))
	}
	if opt.Type != "" && m.Type != opt.Type {
		return Resolved{}, errs.New(errs.UnsupportedModel, "model %q has type %q, expected %q", ref, m.Type, opt.Type)
	}
	q := opt.Quant
	if q == "" {
		q = m.Default
	}
	v, ok := m.Variants[q]
	if !ok {
		return Resolved{}, errs.New(errs.QuantNotFound, "model %q has no quant %q (available: %s)", ref, q, strings.Join(m.Quants(), ", "))
	}
	if opt.AdapterKnown != nil && !opt.AdapterKnown(v.Adapter, m.Type) {
		return Resolved{}, errs.New(errs.UnsupportedModel, "model %q quant %q uses unknown %s adapter %q", ref, q, m.Type, v.Adapter)
	}
	return Resolved{Model: m, Variant: v}, nil
}

func (r *Registry) suggest(ref string) string {
	name, _, _ := strings.Cut(ref, ":")
	var hits []string
	for _, id := range r.IDs() {
		n, _, _ := strings.Cut(id, ":")
		if n == name {
			hits = append(hits, id)
		}
	}
	if len(hits) == 0 {
		return " (run `self list` to see available models)"
	}
	return " (did you mean: " + strings.Join(hits, ", ") + "?)"
}
