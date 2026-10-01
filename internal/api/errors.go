package api

import (
	"encoding/json"
	"net/http"

	"ai-server/internal/errs"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Type    errs.Kind `json:"type"`
	Message string    `json:"message"`
}

// StatusFor maps error kinds to HTTP status codes.
func StatusFor(k errs.Kind) int {
	switch k {
	case errs.InvalidRequest, errs.UnsupportedImage:
		return http.StatusBadRequest
	case errs.ModelNotFound, errs.QuantNotFound:
		return http.StatusNotFound
	case errs.UnsupportedCapability:
		return http.StatusUnprocessableEntity
	case errs.ImageTooLarge, errs.RequestTooLarge:
		return http.StatusRequestEntityTooLarge
	case errs.ImageFetchFailed:
		return http.StatusBadGateway
	case errs.QueueFull:
		return http.StatusTooManyRequests
	case errs.Timeout:
		return http.StatusGatewayTimeout
	case errs.RuntimeCrashed, errs.RuntimeNotFound, errs.RuntimeStartFailed, errs.ShuttingDown:
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// WriteError writes the standard error body.
func WriteError(w http.ResponseWriter, err error) {
	k := errs.KindOf(err)
	msg := errs.PublicMessage(err)
	if k == errs.Internal {
		msg = "Internal server error."
	}
	WriteJSON(w, StatusFor(k), errorBody{errorDetail{Type: k, Message: msg}})
}

// WriteJSON writes v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
