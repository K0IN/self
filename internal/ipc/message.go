package ipc

import (
	"encoding/json"
	"fmt"
)

// Request is the logical header of a request frame.
type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the logical header of a response frame. Exactly one of Result
// or Error is set.
type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ErrorBody      `json:"error,omitempty"`
}

// ErrorBody is an engine-reported error.
type ErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ParseResponse decodes and validates a response header against the
// expected request id.
func ParseResponse(f Frame, wantID uint64) (Response, error) {
	var r Response
	if err := json.Unmarshal(f.Header, &r); err != nil {
		return r, fmt.Errorf("%w: %v", ErrBadHeader, err)
	}
	if r.ID == 0 {
		return r, fmt.Errorf("%w: missing or zero id", ErrBadHeader)
	}
	if r.ID != wantID {
		return r, fmt.Errorf("ipc: response id %d does not match request id %d", r.ID, wantID)
	}
	if (r.Error == nil) == (len(r.Result) == 0) {
		return r, fmt.Errorf("%w: exactly one of result or error is required", ErrBadHeader)
	}
	return r, nil
}

// ParseRequest decodes and validates a request header.
func ParseRequest(f Frame) (Request, error) {
	var r Request
	if err := json.Unmarshal(f.Header, &r); err != nil {
		return r, fmt.Errorf("%w: %v", ErrBadHeader, err)
	}
	if r.ID == 0 {
		return r, fmt.Errorf("%w: missing or zero id", ErrBadHeader)
	}
	if r.Method == "" {
		return r, fmt.Errorf("%w: missing method", ErrBadHeader)
	}
	return r, nil
}
