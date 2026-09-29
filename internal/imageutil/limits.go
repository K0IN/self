// Package imageutil fetches, decodes, orients and resizes images entirely in
// memory. Nothing in this package touches the filesystem.
package imageutil

import "time"

// Limits bound work and memory per image.
type Limits struct {
	FetchTimeout   time.Duration
	MaxRedirects   int
	MaxSourceBytes int64 // compressed bytes (download or data URI)
	MaxPixels      int64 // decoded width*height
	MaxWidth       int   // decoded width
	MaxHeight      int   // decoded height
	// AllowHTTP permits plain http:// URLs (local development).
	AllowHTTP bool
	// AllowPrivate permits URLs that resolve to loopback, private, link-local
	// or otherwise non-public addresses.
	AllowPrivate bool
}

// DefaultLimits returns conservative defaults.
func DefaultLimits() Limits {
	return Limits{
		FetchTimeout:   15 * time.Second,
		MaxRedirects:   3,
		MaxSourceBytes: 20 << 20,
		MaxPixels:      40_000_000,
		MaxWidth:       12_000,
		MaxHeight:      12_000,
	}
}
