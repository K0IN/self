// Package adapters maps registry adapter names to typed adapter factories.
//
// Each model type has its own table with its own typed interface, e.g.
// decision.Adapter today and later image.Adapter, tts.Adapter, ... There is
// deliberately no universal adapter interface.
package adapters

import (
	"ai-server/internal/adapters/audio"
	"ai-server/internal/adapters/clef"
	"ai-server/internal/adapters/customdecider"
	embeddingadapter "ai-server/internal/adapters/embedding"
	"ai-server/internal/adapters/ggmlclaya"
	imageadapter "ai-server/internal/adapters/image"
	"ai-server/internal/adapters/selfipc"
	"ai-server/internal/adapters/text"
	domaudio "ai-server/internal/audio"
	"ai-server/internal/decision"
	domembedding "ai-server/internal/embedding"
	"ai-server/internal/errs"
	domimage "ai-server/internal/image"
	"ai-server/internal/localconf"
	"ai-server/internal/registry"
	"ai-server/internal/settings"
	domtext "ai-server/internal/text"
)

// Base is what every adapter entry has, whatever its model type.
type Base struct {
	// Engine is the bundled executable name under libexec/ai-server.
	Engine string
	// Settings lists the engine parameters the adapter accepts.
	Settings settings.Schema
}

// DecisionEntry describes a decision adapter.
type DecisionEntry struct {
	Base
	New decision.Factory
}

type AudioEntry struct {
	Base
	New domaudio.Factory
}

// TextEntry describes a text (chat) adapter.
type TextEntry struct {
	Base
	New domtext.Factory
}

type EmbeddingEntry struct {
	Base
	New domembedding.Factory
}

type ImageEntry struct {
	Base
	New domimage.Factory
}

var decisionAdapters = map[string]DecisionEntry{
	// Upstream ggmlc Laya daemon: ggmlc-compiled decision GGUFs (Kev, Laya).
	"ggmlc-laya": {Base: Base{Engine: ggmlclaya.Engine, Settings: ggmlclaya.Settings}, New: ggmlclaya.New},
	"clef":       {Base: Base{Engine: clef.Engine, Settings: clef.Settings}, New: selfipc.Factory(clef.Spec)},
	// Our llama.cpp-based engine for Decider checkpoints (+ vision via mmproj).
	"ggmlc-custom-decider": {Base: Base{Engine: customdecider.Engine, Settings: customdecider.Settings}, New: selfipc.Factory(customdecider.Spec)},
}

var audioAdapters = map[string]AudioEntry{
	// Our persistent llama.cpp text-to-speech engine (Qwen3-TTS, Pocket TTS).
	"ggmlc-audio": {Base: Base{Engine: audio.Engine, Settings: audio.Settings}, New: audio.New},
}

var textAdapters = map[string]TextEntry{
	// Our persistent llama.cpp chat engine (Qwen3.5, Gemma 4; + vision via mmproj).
	"llama-server": {Base: Base{Engine: text.Engine, Settings: text.Settings}, New: text.New},
}

var embeddingAdapters = map[string]EmbeddingEntry{
	"llama-server": {Base: Base{Engine: text.Engine, Settings: text.Settings}, New: embeddingadapter.New},
}

var imageAdapters = map[string]ImageEntry{
	"sd-server": {Base: Base{Engine: imageadapter.Engine, Settings: imageadapter.Settings}, New: imageadapter.New},
}

// Lookup returns the parts of adapter name that do not depend on the model
// type. A new model type adds a table above and a case here.
func Lookup(t registry.ModelType, name string) (Base, error) {
	switch t {
	case registry.TypeDecision:
		e, err := Decision(name)
		return e.Base, err
	case registry.TypeAudio:
		e, err := Audio(name)
		return e.Base, err
	case registry.TypeText:
		e, err := Text(name)
		return e.Base, err
	case registry.TypeEmbedding:
		e, err := Embedding(name)
		return e.Base, err
	case registry.TypeImage:
		e, err := Image(name)
		return e.Base, err
	}
	return Base{}, errs.New(errs.UnsupportedModel, "no adapters for model type %q", t)
}

func Embedding(name string) (EmbeddingEntry, error) {
	e, ok := embeddingAdapters[name]
	if !ok {
		return EmbeddingEntry{}, errs.New(errs.UnsupportedModel, "unknown embedding adapter %q", name)
	}
	return e, nil
}

func Image(name string) (ImageEntry, error) {
	e, ok := imageAdapters[name]
	if !ok {
		return ImageEntry{}, errs.New(errs.UnsupportedModel, "unknown image adapter %q", name)
	}
	return e, nil
}

func Audio(name string) (AudioEntry, error) {
	e, ok := audioAdapters[name]
	if !ok {
		return AudioEntry{}, errs.New(errs.UnsupportedModel, "unknown audio adapter %q", name)
	}
	return e, nil
}

// Text returns the text adapter registered under name.
func Text(name string) (TextEntry, error) {
	e, ok := textAdapters[name]
	if !ok {
		return TextEntry{}, errs.New(errs.UnsupportedModel, "unknown text adapter %q", name)
	}
	return e, nil
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
	e, err := Lookup(res.Model.Type, res.Variant.Adapter)
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
	_, err := Lookup(t, name)
	return err == nil
}
