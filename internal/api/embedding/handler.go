// Package embedding serves the OpenAI-compatible embeddings API.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	apiroot "ai-server/internal/api"
	"ai-server/internal/errs"
	"github.com/go-chi/chi/v5"
)

const MaxBodyBytes = 32 << 20

type Service interface {
	ModelID() string
	Quant() string
	Info() any
	Settings() map[string]any
	Embed(context.Context, string) ([]float32, int, error)
}

type Handler struct{ svc Service }

func New(svc Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Mount(r chi.Router) apiroot.ModelInfo {
	r.Post("/v1/embeddings", h.embeddings)
	return apiroot.ModelInfo{ID: h.svc.ModelID(), Object: "model", Type: "embedding", Quant: h.svc.Quant(), Capabilities: map[string]any{"input": []string{"text"}, "output": []string{"embedding"}}, Info: h.svc.Info(), Settings: h.svc.Settings()}
}

type request struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"`
	EncodingFormat string          `json:"encoding_format"`
	Dimensions     *int            `json:"dimensions"`
	User           string          `json:"user"`
}

type parsedRequest struct{ inputs []string }

func (h *Handler) embeddings(w http.ResponseWriter, r *http.Request) {
	req, err := decode(w, r)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(req.inputs))
	totalTokens := 0
	for i, input := range req.inputs {
		vector, tokens, err := h.svc.Embed(r.Context(), input)
		if err != nil {
			apiroot.WriteError(w, err)
			return
		}
		data = append(data, map[string]any{"object": "embedding", "embedding": vector, "index": i})
		totalTokens += tokens
	}
	apiroot.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list", "data": data, "model": h.svc.ModelID(),
		"usage": map[string]int{"prompt_tokens": totalTokens, "total_tokens": totalTokens},
	})
}

func decode(w http.ResponseWriter, r *http.Request) (parsedRequest, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if ct != "" && ct != "application/json" {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "Content-Type must be application/json")
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return parsedRequest{}, errs.New(errs.RequestTooLarge, "request body exceeds %d bytes", MaxBodyBytes)
		}
		return parsedRequest{}, errs.New(errs.InvalidRequest, "cannot read request body: %s", err)
	}
	var req request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "invalid request body: %s", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "request body must contain one JSON object")
	}
	if len(req.Input) == 0 || string(req.Input) == "null" {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "input is required")
	}
	if req.EncodingFormat != "" && req.EncodingFormat != "float" {
		return parsedRequest{}, errs.New(errs.UnsupportedCapability, "encoding_format %q is not supported by the loaded embedding model", req.EncodingFormat)
	}
	if req.Dimensions != nil {
		return parsedRequest{}, errs.New(errs.UnsupportedCapability, "dimensions are not supported by the loaded embedding model")
	}
	var one string
	if json.Unmarshal(req.Input, &one) == nil {
		return parsedRequest{inputs: []string{one}}, nil
	}
	var many []string
	if err := json.Unmarshal(req.Input, &many); err != nil || len(many) == 0 {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "input must be a string or a non-empty array of strings")
	}
	return parsedRequest{inputs: many}, nil
}
