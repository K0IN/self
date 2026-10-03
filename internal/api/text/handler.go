// Package text serves the OpenAI-compatible chat completions API
// (POST /v1/chat/completions) on top of a text (chat) model.
package text

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	apiroot "ai-server/internal/api"
	"ai-server/internal/errs"
	dom "ai-server/internal/text"
	"github.com/go-chi/chi/v5"
)

// MaxBodyBytes bounds request bodies, inline (base64) images included.
const MaxBodyBytes = 32 << 20

// maxStop is the number of stop sequences OpenAI allows.
const maxStop = 4

// Service is what the handler needs from the loaded model.
type Service interface {
	ModelID() string
	Quant() string
	Info() any
	Settings() map[string]any
	Runtime() dom.RuntimeInfo
	Chat(ctx context.Context, req dom.Request, onDelta func(dom.Delta)) (dom.Response, error)
}

type Handler struct {
	svc Service
}

func New(svc Service) *Handler { return &Handler{svc: svc} }

// Mount registers the routes and describes the model for /v1/model.
func (h *Handler) Mount(r chi.Router) apiroot.ModelInfo {
	r.Post("/v1/chat/completions", h.completions)
	rt := h.svc.Runtime()
	input := []string{"text"}
	caps := map[string]any{"output": []string{"text"}, "context_size": rt.ContextSize, "reasoning": rt.Reasoning}
	if rt.Vision {
		input = append(input, "vision")
		caps["max_images"] = rt.MaxImages
	}
	caps["input"] = input
	return apiroot.ModelInfo{ID: h.svc.ModelID(), Object: "model", Type: "text", Quant: h.svc.Quant(), Capabilities: caps, Info: h.svc.Info(), Settings: h.svc.Settings()}
}

// request is the body of POST /v1/chat/completions. Unknown top-level fields
// are rejected. Fields the loaded model cannot honour are accepted by the
// decoder (so the error can name them) and answered with 422 unless they are
// empty.
type request struct {
	Model               string          `json:"model"` // accepted and ignored: a server runs one model
	Messages            []message       `json:"messages"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	N                   *int            `json:"n"`
	Stream              bool            `json:"stream"`
	StreamOptions       *streamOptions  `json:"stream_options"`
	Stop                json.RawMessage `json:"stop"`
	PresencePenalty     *float64        `json:"presence_penalty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty"`
	Seed                *int64          `json:"seed"`
	ResponseFormat      *responseFormat `json:"response_format"`
	ReasoningEffort     *string         `json:"reasoning_effort"`
	// ChatTemplateKwargs is the llama.cpp / vLLM extension; only enable_thinking is understood.
	ChatTemplateKwargs map[string]json.RawMessage `json:"chat_template_kwargs"`

	// Accepted, no effect on the output.
	User              json.RawMessage `json:"user"`
	Store             json.RawMessage `json:"store"`
	Metadata          json.RawMessage `json:"metadata"`
	ServiceTier       json.RawMessage `json:"service_tier"`
	PromptCacheKey    json.RawMessage `json:"prompt_cache_key"`
	PromptCacheRetain json.RawMessage `json:"prompt_cache_retention"`
	SafetyIdentifier  json.RawMessage `json:"safety_identifier"`
	ParallelToolCalls json.RawMessage `json:"parallel_tool_calls"`

	// Standard fields the engine cannot do: 422 unless empty.
	Tools            json.RawMessage `json:"tools"`
	ToolChoice       json.RawMessage `json:"tool_choice"`
	Functions        json.RawMessage `json:"functions"`
	FunctionCall     json.RawMessage `json:"function_call"`
	Logprobs         json.RawMessage `json:"logprobs"`
	TopLogprobs      json.RawMessage `json:"top_logprobs"`
	LogitBias        json.RawMessage `json:"logit_bias"`
	Modalities       json.RawMessage `json:"modalities"`
	Audio            json.RawMessage `json:"audio"`
	Prediction       json.RawMessage `json:"prediction"`
	WebSearchOptions json.RawMessage `json:"web_search_options"`
	Verbosity        json.RawMessage `json:"verbosity"`
}

type streamOptions struct {
	IncludeUsage       bool  `json:"include_usage"`
	IncludeObfuscation *bool `json:"include_obfuscation"`
}

type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Strict      *bool           `json:"strict"` // the schema is always enforced while decoding
		Schema      json.RawMessage `json:"schema"`
	} `json:"json_schema"`
}

type message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Name         *string         `json:"name"`
	ToolCalls    json.RawMessage `json:"tool_calls"`
	ToolCallID   json.RawMessage `json:"tool_call_id"`
	FunctionCall json.RawMessage `json:"function_call"`
	Audio        json.RawMessage `json:"audio"`
}

// UnmarshalJSON ignores fields of a message it does not know, so clients that
// add their own bookkeeping to messages keep working.
func (m *message) UnmarshalJSON(b []byte) error {
	type plain message
	return json.Unmarshal(b, (*plain)(m))
}

// parsed is a validated request.
type parsed struct {
	req          dom.Request
	stream       bool
	includeUsage bool
}

func unsupported(what string) error {
	return errs.New(errs.UnsupportedCapability, "%s is not supported by the loaded text model", what)
}

// empty reports whether an optional JSON field is absent or has its "off" value.
func empty(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "false", "0", "[]", "{}", `""`, `"none"`:
		return true
	}
	return false
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, errs.New(errs.RequestTooLarge, "request body exceeds %d bytes", MaxBodyBytes)
		}
		return nil, errs.New(errs.InvalidRequest, "cannot read request body: %s", err)
	}
	return b, nil
}

func decode(w http.ResponseWriter, r *http.Request) (parsed, error) {
	if ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0])); ct != "" && ct != "application/json" {
		return parsed{}, errs.New(errs.InvalidRequest, "Content-Type must be application/json")
	}
	b, err := readBody(w, r)
	if err != nil {
		return parsed{}, err
	}
	var req request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return parsed{}, errs.New(errs.InvalidRequest, "invalid request body: %s", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return parsed{}, errs.New(errs.InvalidRequest, "request body must contain one JSON object")
	}
	return req.validate()
}

func (req request) validate() (parsed, error) {
	var p parsed
	for _, f := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"tools", req.Tools}, {"tool_choice", req.ToolChoice}, {"functions", req.Functions}, {"function_call", req.FunctionCall},
		{"logprobs", req.Logprobs}, {"top_logprobs", req.TopLogprobs}, {"logit_bias", req.LogitBias},
		{"audio", req.Audio}, {"prediction", req.Prediction}, {"web_search_options", req.WebSearchOptions}, {"verbosity", req.Verbosity},
	} {
		if len(f.raw) > 0 && !(f.name == "tool_choice" && strings.TrimSpace(string(f.raw)) == `"auto"`) && !empty(f.raw) {
			return p, unsupported(f.name)
		}
	}
	if m := strings.TrimSpace(string(req.Modalities)); !empty(req.Modalities) && m != `["text"]` {
		return p, unsupported("modalities other than text")
	}
	if req.N != nil && *req.N != 1 {
		return p, unsupported("n other than 1")
	}
	if len(req.Messages) == 0 {
		return p, errs.New(errs.InvalidRequest, "messages is required and must not be empty")
	}

	out := dom.Request{}
	for i, m := range req.Messages {
		msg, err := m.convert(i)
		if err != nil {
			return p, err
		}
		out.Messages = append(out.Messages, msg)
	}

	switch {
	case req.MaxCompletionTokens != nil:
		out.MaxTokens = *req.MaxCompletionTokens
	case req.MaxTokens != nil:
		out.MaxTokens = *req.MaxTokens
	}
	if (req.MaxCompletionTokens != nil || req.MaxTokens != nil) && out.MaxTokens < 1 {
		return p, errs.New(errs.InvalidRequest, "max_completion_tokens must be at least 1")
	}
	if err := inRange("temperature", req.Temperature, 0, 2); err != nil {
		return p, err
	}
	if err := inRange("top_p", req.TopP, 0, 1); err != nil {
		return p, err
	}
	if err := inRange("presence_penalty", req.PresencePenalty, -2, 2); err != nil {
		return p, err
	}
	if err := inRange("frequency_penalty", req.FrequencyPenalty, -2, 2); err != nil {
		return p, err
	}
	out.Temperature, out.TopP, out.PresencePenalty, out.FrequencyPenalty, out.Seed = req.Temperature, req.TopP, req.PresencePenalty, req.FrequencyPenalty, req.Seed

	stop, err := parseStop(req.Stop)
	if err != nil {
		return p, err
	}
	out.Stop = stop

	if rf := req.ResponseFormat; rf != nil {
		switch rf.Type {
		case "", "text":
		case "json_object":
			out.JSONSchema = json.RawMessage(`{"type":"object"}`)
		case "json_schema":
			if rf.JSONSchema == nil || len(bytes.TrimSpace(rf.JSONSchema.Schema)) == 0 || bytes.TrimSpace(rf.JSONSchema.Schema)[0] != '{' {
				return p, errs.New(errs.InvalidRequest, "response_format.json_schema.schema must be a JSON schema object")
			}
			out.JSONSchema = rf.JSONSchema.Schema
		default:
			return p, errs.New(errs.InvalidRequest, "response_format.type must be text, json_object or json_schema")
		}
	}

	if req.ReasoningEffort != nil {
		switch *req.ReasoningEffort {
		case "none":
			off := false
			out.Thinking = &off
		case "minimal", "low", "medium", "high", "xhigh":
			on := true
			out.Thinking = &on
		default:
			return p, errs.New(errs.InvalidRequest, "reasoning_effort must be none, minimal, low, medium, high or xhigh")
		}
	}
	for k, v := range req.ChatTemplateKwargs {
		if k != "enable_thinking" {
			return p, unsupported(fmt.Sprintf("chat_template_kwargs.%s", k))
		}
		var on bool
		if err := json.Unmarshal(v, &on); err != nil {
			return p, errs.New(errs.InvalidRequest, "chat_template_kwargs.enable_thinking must be a boolean")
		}
		out.Thinking = &on
	}

	p.req, p.stream = out, req.Stream
	if req.StreamOptions != nil {
		if !req.Stream {
			return p, errs.New(errs.InvalidRequest, "stream_options can only be set when stream is true")
		}
		p.includeUsage = req.StreamOptions.IncludeUsage
	}
	return p, nil
}

func inRange(name string, v *float64, lo, hi float64) error {
	if v != nil && (*v < lo || *v > hi || *v != *v) {
		return errs.New(errs.InvalidRequest, "%s must be between %g and %g", name, lo, hi)
	}
	return nil
}

// parseStop reads OpenAI's stop: a string or an array of at most four strings.
func parseStop(raw json.RawMessage) ([]string, error) {
	if empty(raw) {
		return nil, nil
	}
	var stops []string
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		stops = []string{one}
	} else if err := json.Unmarshal(raw, &stops); err != nil {
		return nil, errs.New(errs.InvalidRequest, "stop must be a string or an array of strings")
	}
	if len(stops) > maxStop {
		return nil, errs.New(errs.InvalidRequest, "stop can hold at most %d sequences", maxStop)
	}
	out := stops[:0]
	for _, s := range stops {
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// convert maps one OpenAI message to the domain type.
func (m message) convert(i int) (dom.Message, error) {
	at := fmt.Sprintf("messages[%d]", i)
	var role string
	switch m.Role {
	case "system", "developer":
		role = dom.RoleSystem
	case "user":
		role = dom.RoleUser
	case "assistant":
		role = dom.RoleAssistant
	case "tool", "function":
		return dom.Message{}, unsupported(at + ": messages with role " + m.Role)
	case "":
		return dom.Message{}, errs.New(errs.InvalidRequest, "%s.role is required", at)
	default:
		return dom.Message{}, errs.New(errs.InvalidRequest, "%s.role %q is not a chat role", at, m.Role)
	}
	if !empty(m.ToolCalls) || !empty(m.FunctionCall) || !empty(m.Audio) || !empty(m.ToolCallID) {
		return dom.Message{}, unsupported(at + ": tool calls and audio in messages")
	}
	if m.Name != nil && *m.Name != "" {
		return dom.Message{}, unsupported(at + ".name")
	}
	msg := dom.Message{Role: role}
	raw := bytes.TrimSpace(m.Content)
	switch {
	case len(raw) == 0 || string(raw) == "null":
		if role != dom.RoleAssistant {
			return msg, errs.New(errs.InvalidRequest, "%s.content is required", at)
		}
	case raw[0] == '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return msg, errs.New(errs.InvalidRequest, "%s.content: %s", at, err)
		}
		msg.Parts = []dom.Part{{Text: s}}
	case raw[0] == '[':
		var parts []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(raw, &parts); err != nil {
			return msg, errs.New(errs.InvalidRequest, "%s.content: %s", at, err)
		}
		for j, p := range parts {
			where := fmt.Sprintf("%s.content[%d]", at, j)
			switch p.Type {
			case "text":
				msg.Parts = append(msg.Parts, dom.Part{Text: p.Text})
			case "image_url":
				if role != dom.RoleUser {
					return msg, errs.New(errs.InvalidRequest, "%s: images are only allowed in user messages", where)
				}
				if p.ImageURL == nil || strings.TrimSpace(p.ImageURL.URL) == "" {
					return msg, errs.New(errs.InvalidRequest, "%s.image_url.url is required", where)
				}
				msg.Parts = append(msg.Parts, dom.Part{ImageURL: p.ImageURL.URL})
			case "input_audio", "file", "refusal":
				return msg, unsupported(fmt.Sprintf("%s: content parts of type %s", where, p.Type))
			default:
				return msg, errs.New(errs.InvalidRequest, "%s.type %q is not a content part type", where, p.Type)
			}
		}
	default:
		return msg, errs.New(errs.InvalidRequest, "%s.content must be a string or an array of content parts", at)
	}
	return msg, nil
}

func (h *Handler) completions(w http.ResponseWriter, r *http.Request) {
	p, err := decode(w, r)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	id, created, model := "chatcmpl-"+randomID(), time.Now().Unix(), h.svc.ModelID()
	if p.stream {
		h.stream(w, r, p, id, created, model)
		return
	}
	resp, err := h.svc.Chat(r.Context(), p.req, nil)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	msg := map[string]any{"role": "assistant", "content": resp.Content}
	if resp.Reasoning != "" {
		msg["reasoning_content"] = resp.Reasoning
	}
	apiroot.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "object": "chat.completion", "created": created, "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": resp.FinishReason, "logprobs": nil}},
		"usage":   usage(resp.Usage),
	})
}

func usage(u dom.Usage) map[string]int {
	return map[string]int{"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.PromptTokens + u.CompletionTokens}
}

// stream answers with server-sent events. Nothing is written until the first
// delta, so a request the engine rejects still gets a plain JSON error.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request, p parsed, id string, created int64, model string) {
	rc := http.NewResponseController(w)
	started := false
	chunk := func(delta map[string]any, finish any, u any) map[string]any {
		c := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		if u != nil {
			c["usage"] = u
		}
		return c
	}
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		_ = rc.Flush()
	}
	start := func() {
		if started {
			return
		}
		started = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		send(chunk(map[string]any{"role": "assistant", "content": ""}, nil, nil))
	}

	resp, err := h.svc.Chat(r.Context(), p.req, func(d dom.Delta) {
		start()
		delta := map[string]any{}
		if d.Reasoning != "" {
			delta["reasoning_content"] = d.Reasoning
		}
		if d.Content != "" {
			delta["content"] = d.Content
		}
		send(chunk(delta, nil, nil))
	})
	if err != nil {
		if !started {
			apiroot.WriteError(w, err)
			return
		}
		// Headers are gone: report the failure in-band, as OpenAI does.
		send(map[string]any{"error": map[string]any{"type": errs.KindOf(err), "message": errs.PublicMessage(err)}})
		return
	}
	start()
	send(chunk(map[string]any{}, resp.FinishReason, nil))
	if p.includeUsage {
		send(map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{}, "usage": usage(resp.Usage)})
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	_ = rc.Flush()
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
