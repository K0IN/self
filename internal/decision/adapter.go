package decision

import (
	"context"
	"io"

	"ai-server/internal/settings"
)

// ModelFiles are the local files of the selected model variant.
type ModelFiles struct {
	// Model is the main GGUF.
	Model string
	// MMProj is an optional multimodal projector GGUF.
	MMProj string
	// Head is an optional trained decision head artifact.
	Head string
}

// RuntimeConfig configures an adapter's engine.
type RuntimeConfig struct {
	ModelID string
	Files   ModelFiles
	// Device is a validated device selector: auto, cpu, cuda, cuda:N,
	// metal, vulkan, vulkan:N.
	Device string
	// EnginePath is the resolved engine executable.
	EnginePath string
	// LibDir, when non-empty, holds bundled shared libraries for the engine.
	LibDir string
	// Log receives engine stderr lines when non-nil (verbose mode).
	Log io.Writer
	// Settings are validated engine parameters (registry + --set), already
	// checked against the adapter's schema.
	Settings settings.Values
}

// ResizeMode controls how images are fit into target geometry.
type ResizeMode string

const (
	ResizeContain ResizeMode = "contain" // fit inside, preserve aspect, pad
	ResizeCover   ResizeMode = "cover"   // fill, preserve aspect, crop
	ResizeStretch ResizeMode = "stretch" // ignore aspect
)

// GeometryMode selects how target image size is derived.
type GeometryMode string

const (
	GeometryFixed   GeometryMode = "fixed"
	GeometryBounded GeometryMode = "bounded"
	GeometryDynamic GeometryMode = "dynamic"
)

// ImageGeometry is the image input contract reported by an engine.
type ImageGeometry struct {
	Mode   GeometryMode `json:"mode"`
	Resize ResizeMode   `json:"resize,omitempty"`

	// fixed
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// bounded
	MaxWidth  int `json:"max_width,omitempty"`
	MaxHeight int `json:"max_height,omitempty"`
	// dynamic
	MaxPixels      int `json:"max_pixels,omitempty"`
	MinPixels      int `json:"min_pixels,omitempty"`
	WidthMultiple  int `json:"width_multiple,omitempty"`
	HeightMultiple int `json:"height_multiple,omitempty"`
}

// RuntimeInfo describes a started engine.
type RuntimeInfo struct {
	// EngineModel is the engine-reported model name (informational).
	EngineModel string
	// Device is the engine-reported device, or the requested one when the
	// engine does not report it.
	Device       string
	Capabilities Capabilities
	// ImageInput is required when Capabilities.Vision is true.
	ImageInput *ImageGeometry
}

// Adapter runs decision inference through an engine. Implementations own
// all upstream-specific knowledge. Decide is called by a single worker, one
// request at a time.
type Adapter interface {
	Start(ctx context.Context, cfg RuntimeConfig) error
	Close(ctx context.Context) error
	Info(ctx context.Context) (RuntimeInfo, error)
	Decide(ctx context.Context, req Request) (Response, error)
	// Done is closed when the engine exits for any reason.
	Done() <-chan struct{}
}

// Factory creates an unstarted adapter.
type Factory func() Adapter
