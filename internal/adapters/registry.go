// Package adapters maps registry adapter names to typed adapter factories.
//
// Each model type has its own table with its own typed interface, e.g.
// decision.Adapter today and later image.Adapter, tts.Adapter, ... There is
// deliberately no universal adapter interface.
package adapters

import (
	"sort"

	"ai-server/internal/adapters/customdecider"
	"ai-server/internal/adapters/ggmlclaya"
	"ai-server/internal/adapters/selfipc"
	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/localconf"
	"ai-server/internal/registry"
	"ai-server/internal/settings"
)

// DecisionEntry describes a decision adapter.
type DecisionEntry struct {
	// Engine is the bundled executable name under libexec/ai-server.
	Engine string
	New    decision.Factory
	// Settings lists the engine parameters the adapter accepts.
	Settings settings.Schema
}

var decisionAdapters = map[string]DecisionEntry{
	// Upstream ggmlc Laya daemon: ggmlc-compiled decision GGUFs (Kev, Laya).
	"ggmlc-laya": {Engine: ggmlclaya.Engine, New: ggmlclaya.New, Settings: ggmlclaya.Settings},
	// Our llama.cpp-based engine for Decider checkpoints (+ vision via mmproj).
	"ggmlc-custom-decider": {Engine: customdecider.Engine, New: selfipc.Factory(customdecider.Spec), Settings: customdecider.Settings},
}

// ResolveSettings merges registry settings (model, then quant) with CLI
// overrides and validates them against the adapter's schema.
func ResolveSettings(res registry.Resolved, overrides map[string]any) (settings.Values, error) {
	v, _, err := ResolveSettingsLayered(res, nil, overrides)
	return v, err
}

// ResolveSettingsLayered merges, low to high: registry model, registry
// quant, local adapter, local model, local quant, CLI overrides. It returns
// the validated values and, per key, the name of the layer that set it.
func ResolveSettingsLayered(res registry.Resolved, local *localconf.Config, overrides map[string]any) (settings.Values, map[string]string, error) {
	e, err := Decision(res.Variant.Adapter)
	if err != nil {
		return nil, nil, err
	}
	layers := []localconf.Layer{
		{Name: "registry", Values: res.Model.Settings},
		{Name: "registry quant " + res.Variant.Quant, Values: res.Variant.Settings},
	}
	layers = append(layers, local.Layers(res.ID(), res.Variant.Quant, res.Variant.Adapter)...)
	layers = append(layers, localconf.Layer{Name: "--set", Values: overrides})

	merged, source := map[string]any{}, map[string]string{}
	for _, l := range layers {
		for k, v := range l.Values {
			merged[k], source[k] = v, l.Name
		}
	}
	v, err := e.Settings.Validate(merged)
	if err != nil {
		where := ""
		if local != nil && local.Path != "" {
			where = " (registry, " + local.Path + " or --set)"
		}
		return nil, nil, errs.New(errs.InvalidRequest, "%s (adapter %s): %s%s", res.ID(), res.Variant.Adapter, err, where)
	}
	return v, source, nil
}

// Decision returns the decision adapter registered under name.
func Decision(name string) (DecisionEntry, error) {
	e, ok := decisionAdapters[name]
	if !ok {
		return DecisionEntry{}, errs.New(errs.UnsupportedModel, "unknown decision adapter %q", name)
	}
	return e, nil
}

// Known reports whether an adapter exists for the model type.
func Known(name string, t registry.ModelType) bool {
	switch t {
	case registry.TypeDecision:
		_, ok := decisionAdapters[name]
		return ok
	}
	return false
}

// DecisionNames lists registered decision adapters.
func DecisionNames() []string {
	out := make([]string, 0, len(decisionAdapters))
	for k := range decisionAdapters {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
