// Package errs defines the typed error kinds shared by every layer of the
// server. HTTP handlers map kinds to status codes; everything else just
// creates and wraps them.
package errs

import (
	"errors"
	"fmt"
)

// Kind is a stable, machine-readable error category. It is exposed verbatim
// as the "type" field of the public error body.
type Kind string

const (
	InvalidRequest        Kind = "invalid_request"
	ModelNotFound         Kind = "model_not_found"
	QuantNotFound         Kind = "quant_not_found"
	UnsupportedModel      Kind = "unsupported_model"
	DownloadFailed        Kind = "download_failed"
	RuntimeNotFound       Kind = "runtime_not_found"
	RuntimeStartFailed    Kind = "runtime_start_failed"
	RuntimeCrashed        Kind = "runtime_crashed"
	UnsupportedCapability Kind = "unsupported_capability"
	UnsupportedImage      Kind = "unsupported_image"
	ImageFetchFailed      Kind = "image_fetch_failed"
	ImageTooLarge         Kind = "image_too_large"
	RequestTooLarge       Kind = "request_too_large"
	QueueFull             Kind = "queue_full"
	Timeout               Kind = "timeout"
	ShuttingDown          Kind = "shutting_down"
	Internal              Kind = "internal_error"
)

// Error is a categorized error with a human readable message.
type Error struct {
	Kind    Kind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil && e.Message == "" {
		return e.Err.Error()
	}
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// New creates a categorized error.
func New(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Wrap attaches a kind and message to an underlying error.
func Wrap(kind Kind, err error, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...), Err: err}
}

// KindOf returns the kind of err, or Internal when err is not categorized.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return Internal
}

// Is reports whether err carries the given kind.
func Is(err error, kind Kind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}

// PublicMessage returns a message suitable for API clients. Internal errors
// are not echoed verbatim.
func PublicMessage(err error) string {
	var e *Error
	if errors.As(err, &e) {
		if e.Kind == Internal && e.Message == "" {
			return "Internal server error."
		}
		return e.Error()
	}
	return "Internal server error."
}
