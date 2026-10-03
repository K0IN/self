// Package text defines the typed runtime contract for chat (text generation)
// models: request and response types, the Adapter interface implemented by
// engine adapters, and the Service used by HTTP handlers.
//
// Nothing in this package knows about a specific upstream engine.
package text

import (
	"context"
	"encoding/json"
	"io"

	"ai-server/internal/decision"
	"ai-server/internal/settings"
)

// ModelFiles are the local files of the selected model variant.
type ModelFiles struct {
	Model string
	// MMProj is the optional multimodal projector (image input).
	MMProj string
}

// RuntimeConfig configures an adapter's engine.
type RuntimeConfig struct {
	ModelID    string
	Files      ModelFiles
	Device     string
	EnginePath string
	LibDir     string
	Log        io.Writer
	Settings   settings.Values
}

// Roles of a message, as the OpenAI chat API names them. The HTTP layer maps
// "developer" to RoleSystem.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Image is a fully preprocessed image: tightly packed 8-bit RGB, row-major.
type Image struct {
	Width, Height int
	Pixels        []byte
}

// Part is one piece of a message: text, or an image. Clients name an image by
// ImageURL; the Service replaces it with decoded pixels before an adapter
// sees the request.
type Part struct {
	Text     string
	ImageURL string
	Image    *Image
}

// Message is one chat message.
type Message struct {
	Role  string
	Parts []Part
}

// Request is one chat completion. Nil pointers mean "the engine's default".
type Request struct {
	Messages []Message
	// MaxTokens limits the generated tokens; 0 = until the model stops or the context is full.
	MaxTokens        int
	Temperature      *float64
	TopP             *float64
	PresencePenalty  *float64
	FrequencyPenalty *float64
	Seed             *int64
	Stop             []string
	// JSONSchema constrains the output to a JSON schema (response_format); nil = free text.
	JSONSchema json.RawMessage
	// Thinking turns the model's reasoning on or off; nil = the engine default.
	Thinking *bool
}

// Delta is output produced since the previous delta.
type Delta struct {
	Content   string
	Reasoning string
}

// Finish reasons.
const (
	FinishStop   = "stop"
	FinishLength = "length"
)

// Usage counts tokens.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Response is a finished completion.
type Response struct {
	Content string
	// Reasoning is the model's thinking, split from Content; empty when it did not think.
	Reasoning    string
	FinishReason string
	Usage        Usage
}

// RuntimeInfo describes a started engine.
type RuntimeInfo struct {
	// EngineModel is the engine-reported model name (informational).
	EngineModel string
	Device      string
	// ContextSize is the context window in tokens.
	ContextSize int
	// Reasoning reports whether the chat template can switch thinking on and off.
	Reasoning bool
	Vision    bool
	MaxImages int
	// ImageInput is set when Vision is true.
	ImageInput *decision.ImageGeometry
}

// Adapter runs chat completions through an engine. Implementations own all
// upstream-specific knowledge. Chat serves one request at a time; callers
// that arrive meanwhile wait.
type Adapter interface {
	Start(ctx context.Context, cfg RuntimeConfig) error
	Close(ctx context.Context) error
	Info(ctx context.Context) (RuntimeInfo, error)
	// Chat runs req. onDelta, when non-nil, receives the output as it is
	// generated; the returned Response always holds the complete output.
	// Cancelling ctx stops generation.
	Chat(ctx context.Context, req Request, onDelta func(Delta)) (Response, error)
	// Done is closed when the engine exits for any reason.
	Done() <-chan struct{}
}

// Factory creates an unstarted adapter.
type Factory func() Adapter
