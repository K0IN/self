// Package embedding serves the OpenAI-compatible embeddings API.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	apiroot "ai-server/internal/api"
	dom "ai-server/internal/embedding"
	"ai-server/internal/errs"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
	"github.com/go-chi/chi/v5"
)

const MaxBodyBytes = 32 << 20

type Service interface {
	ModelID() string
	Quant() string
	Info() any
	Settings() map[string]any
	Embed(context.Context, dom.Input) ([]float32, int, error)
	Capabilities() registry.Capabilities
}

type Handler struct {
	svc     Service
	fetcher *imageutil.Fetcher
}

func New(svc Service) *Handler {
	limits := imageutil.DefaultLimits()
	limits.MaxSourceBytes = maxMediaBytes
	return NewWithFetcher(svc, imageutil.NewFetcher(limits))
}

func NewWithFetcher(svc Service, fetcher *imageutil.Fetcher) *Handler {
	return &Handler{svc: svc, fetcher: fetcher}
}

func (h *Handler) Mount(r chi.Router) apiroot.ModelInfo {
	r.Post("/v1/embeddings", h.embeddings)
	r.Post("/similarity", h.similarity)
	caps := h.svc.Capabilities()
	return apiroot.ModelInfo{ID: h.svc.ModelID(), Object: "model", Type: "embedding", Quant: h.svc.Quant(), Capabilities: map[string]any{"input": caps.Input.List(), "output": caps.Output.List()}, Info: h.svc.Info(), Settings: h.svc.Settings()}
}

type request struct {
	Model          string              `json:"model"`
	Input          json.RawMessage     `json:"input"`
	EncodingFormat string              `json:"encoding_format"`
	Dimensions     *int                `json:"dimensions"`
	User           string              `json:"user"`
	MultiEmbedding bool                `json:"multi_embedding"`
	InputType      dom.EncodingPurpose `json:"input_type"`
}

type parsedRequest struct {
	inputs  []dom.Input
	multi   bool
	purpose dom.EncodingPurpose
}

type similarityRequest struct {
	Input json.RawMessage   `json:"input"`
	Ref   []json.RawMessage `json:"ref"`
}

func (h *Handler) embeddings(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	req, err := h.decode(w, r)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(req.inputs))
	if err := h.validate(req.inputs); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	totalTokens := 0
	for i, input := range req.inputs {
		if req.multi {
			vectors, tokens, err := h.svc.(dom.MultiVectorEmbedder).EmbedMulti(r.Context(), input, req.purpose)
			if err != nil {
				apiroot.WriteError(w, err)
				return
			}
			if err := validateMultiVectors(vectors); err != nil {
				apiroot.WriteError(w, errs.Wrap(errs.RuntimeCrashed, err, "invalid multi-embedding result"))
				return
			}
			data = append(data, map[string]any{"object": "multi-embedding", "embedding": vectors, "index": i})
			totalTokens += tokens
			continue
		}
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
		"usage": map[string]any{"prompt_tokens": totalTokens, "total_tokens": totalTokens, "latency_ms": elapsedMS(started)},
	})
}

func (h *Handler) similarity(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := h.validateSingleOutput(); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	var req similarityRequest
	if err := decodeJSON(w, r, &req); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	input, err := parseInputWithFetcher(r.Context(), req.Input, h.fetcher)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	if len(req.Ref) == 0 {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "ref is required and must not be empty"))
		return
	}
	inputs := []dom.Input{input}
	for _, raw := range req.Ref {
		ref, err := parseInputWithFetcher(r.Context(), raw, h.fetcher)
		if err != nil {
			apiroot.WriteError(w, err)
			return
		}
		inputs = append(inputs, ref)
	}
	if err := h.validate(inputs); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	inputVector, inputTokens, err := h.svc.Embed(r.Context(), input)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	scores := make([]float32, len(req.Ref))
	totalTokens := inputTokens
	for i, ref := range inputs[1:] {
		refVector, tokens, err := h.svc.Embed(r.Context(), ref)
		if err != nil {
			apiroot.WriteError(w, err)
			return
		}
		score, err := cosine(inputVector, refVector)
		if err != nil {
			apiroot.WriteError(w, errs.New(errs.RuntimeCrashed, "cannot compare embedding for ref[%d]: %s", i, err))
			return
		}
		score = (score + 1) / 2
		if score < 0 {
			score = 0
		} else if score > 1 {
			score = 1
		}
		scores[i] = score
		totalTokens += tokens
	}
	apiroot.WriteJSON(w, http.StatusOK, map[string]any{
		"similarities": scores,
		"usage":        map[string]any{"prompt_tokens": totalTokens, "total_tokens": totalTokens, "latency_ms": elapsedMS(started)},
	})
}

func elapsedMS(started time.Time) float64 {
	return float64(time.Since(started).Microseconds()) / 1000
}

func (h *Handler) validateSingleOutput() error {
	caps := h.svc.Capabilities()
	if caps.Output.Has(registry.CapMultiEmbedding) && !caps.Output.Has(registry.CapEmbedding) {
		return errs.New(errs.UnsupportedCapability, "loaded model supports multi-embedding output only; set multi_embedding to true")
	}
	return nil
}

func validateMultiVectors(vectors [][]float32) error {
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return errors.New("token vectors must not be empty")
	}
	for _, vector := range vectors {
		if len(vector) != len(vectors[0]) {
			return errors.New("token vector dimensions do not match")
		}
		for _, value := range vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return errors.New("token vectors must contain finite numbers")
			}
		}
	}
	return nil
}

func cosine(left, right []float32) (float32, error) {
	if len(left) == 0 || len(left) != len(right) {
		return 0, errors.New("embedding dimensions do not match")
	}
	var dot, leftNorm, rightNorm float64
	for i := range left {
		l, r := float64(left[i]), float64(right[i])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	denominator := math.Sqrt(leftNorm * rightNorm)
	if denominator == 0 {
		return 0, errors.New("embedding vector has zero magnitude")
	}
	return float32(dot / denominator), nil
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request) (parsedRequest, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if ct != "" && ct != "application/json" {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "Content-Type must be application/json")
	}
	var req request
	if err := decodeJSON(w, r, &req); err != nil {
		return parsedRequest{}, err
	}
	if req.MultiEmbedding {
		_, implemented := h.svc.(dom.MultiVectorEmbedder)
		if !h.svc.Capabilities().Output.Has(registry.CapMultiEmbedding) || !implemented {
			return parsedRequest{}, errs.New(errs.UnsupportedCapability, "loaded model does not support multi-embedding output")
		}
		if req.InputType == "" {
			req.InputType = dom.PurposeDocument
		}
		if req.InputType != dom.PurposeQuery && req.InputType != dom.PurposeDocument {
			return parsedRequest{}, errs.New(errs.InvalidRequest, "input_type must be query or document")
		}
	} else if req.InputType != "" {
		return parsedRequest{}, errs.New(errs.UnsupportedCapability, "input_type requires multi_embedding output")
	}
	if !req.MultiEmbedding {
		if err := h.validateSingleOutput(); err != nil {
			return parsedRequest{}, err
		}
	}
	parsed, err := parseRequest(r.Context(), req, h.fetcher)
	parsed.multi = req.MultiEmbedding
	parsed.purpose = req.InputType
	return parsed, err
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if ct != "" && ct != "application/json" {
		return errs.New(errs.InvalidRequest, "Content-Type must be application/json")
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errs.New(errs.RequestTooLarge, "request body exceeds %d bytes", MaxBodyBytes)
		}
		return errs.New(errs.InvalidRequest, "cannot read request body: %s", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return errs.New(errs.InvalidRequest, "invalid request body: %s", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errs.New(errs.InvalidRequest, "request body must contain one JSON object")
	}
	return nil
}

func parseRequest(ctx context.Context, req request, fetcher *imageutil.Fetcher) (parsedRequest, error) {
	if len(req.Input) == 0 || string(req.Input) == "null" {
		return parsedRequest{}, errs.New(errs.InvalidRequest, "input is required")
	}
	if req.EncodingFormat != "" && req.EncodingFormat != "float" {
		return parsedRequest{}, errs.New(errs.UnsupportedCapability, "encoding_format %q is not supported by the loaded embedding model", req.EncodingFormat)
	}
	if req.Dimensions != nil {
		return parsedRequest{}, errs.New(errs.UnsupportedCapability, "dimensions are not supported by the loaded embedding model")
	}
	var many []json.RawMessage
	if len(req.Input) > 0 && req.Input[0] == '[' {
		if err := json.Unmarshal(req.Input, &many); err != nil || len(many) == 0 {
			return parsedRequest{}, errs.New(errs.InvalidRequest, "input must be a non-empty batch")
		}
	} else {
		many = []json.RawMessage{req.Input}
	}
	result := parsedRequest{}
	for _, raw := range many {
		input, err := parseInputWithFetcher(ctx, raw, fetcher)
		if err != nil {
			return parsedRequest{}, err
		}
		result.inputs = append(result.inputs, input)
	}
	return result, nil
}
