// Package decision is the strongly typed System One decision domain: request
// and answer types, the Adapter interface implemented by engine adapters,
// the single-runner scheduler and the DecisionService used by HTTP handlers.
//
// Nothing in this package knows about a specific upstream engine.
package decision

import (
	"encoding/json"
)

// PixelFormat describes attachment pixel layout.
type PixelFormat string

// FormatRGB8 is tightly packed 8-bit RGB, row-major, no padding.
const FormatRGB8 PixelFormat = "rgb8"

// Image is a fully preprocessed image ready for an engine: decoded,
// orientation-corrected, resized to the engine's geometry and converted to
// RGB. It only ever lives in memory.
type Image struct {
	Name        string
	Description string
	Width       int
	Height      int
	Format      PixelFormat
	Pixels      []byte
}

// Request is an engine-ready decision request.
type Request struct {
	// State is arbitrary JSON, passed through untouched.
	State     json.RawMessage
	Images    []Image
	Questions Questions
}

// Usage reports resource usage. Zero-valued optional fields are omitted.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Images       int     `json:"images,omitempty"`
	LatencyMS    float64 `json:"latency_ms,omitempty"`
	QueueMS      float64 `json:"queue_ms,omitempty"`
}

// Response is a decision result.
type Response struct {
	Answers Answers
	Usage   Usage
}
