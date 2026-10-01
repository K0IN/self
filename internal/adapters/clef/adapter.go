// Package clef reserves the adapter contract for Clef models.
//
// Clef uses a custom joint schema head that is not supported by the current
// llama.cpp decision engine. The registry can describe the model now, while
// startup remains explicitly unsupported until native Clef inference exists.
package clef

import (
	"context"
	"fmt"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/settings"
)

const Engine = "ggmlc-clef"

var Settings = settings.Schema{
	{Name: "context_size", Kind: settings.Int, Min: 512, Max: 262144},
	{Name: "threads", Kind: settings.Int, Min: 1, Max: 1024},
	{Name: "temperature", Kind: settings.Float, Min: 0.01, Max: 100},
	{Name: "gpu_layers", Kind: settings.Int, Min: -1, Max: 10000},
}

type adapter struct {
	done chan struct{}
}

func New() decision.Adapter { return &adapter{done: make(chan struct{})} }

func (a *adapter) Start(context.Context, decision.RuntimeConfig) error {
	return errs.New(errs.UnsupportedModel, "%s inference is not implemented", "clef-flash")
}

func (*adapter) Close(context.Context) error { return nil }

func (*adapter) Info(context.Context) (decision.RuntimeInfo, error) {
	return decision.RuntimeInfo{}, fmt.Errorf("Clef runtime is not implemented")
}

func (*adapter) Decide(context.Context, decision.Request) (decision.Response, error) {
	return decision.Response{}, errs.New(errs.UnsupportedModel, "Clef inference is not implemented")
}

func (a *adapter) Done() <-chan struct{} { return a.done }
