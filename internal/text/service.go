package text

import (
	"context"
	"fmt"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
)

// ServiceConfig configures a Service.
type ServiceConfig struct {
	ModelID string
	Quant   string
	// PreprocessConcurrency bounds requests that fetch and decode images at the same time.
	PreprocessConcurrency int
	// Info and Settings are reported by /v1/model (descriptive only).
	Info     any
	Settings map[string]any
}

// Service is the chat use-case used by HTTP handlers: it checks a request
// against the engine's capabilities, prepares images, then hands the request
// to the adapter.
type Service struct {
	cfg  ServiceConfig
	ad   Adapter
	info RuntimeInfo
	pre  decision.Preprocessor
	sem  chan struct{}
}

// NewService creates a service around a started adapter and what it reported.
func NewService(cfg ServiceConfig, ad Adapter, info RuntimeInfo, pre decision.Preprocessor) *Service {
	return &Service{cfg: cfg, ad: ad, info: info, pre: pre, sem: make(chan struct{}, max(1, cfg.PreprocessConcurrency))}
}

func (s *Service) ModelID() string          { return s.cfg.ModelID }
func (s *Service) Quant() string            { return s.cfg.Quant }
func (s *Service) Info() any                { return s.cfg.Info }
func (s *Service) Settings() map[string]any { return s.cfg.Settings }
func (s *Service) Runtime() RuntimeInfo     { return s.info }

// Chat validates and runs a request.
func (s *Service) Chat(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	if len(req.Messages) == 0 {
		return Response{}, errs.New(errs.InvalidRequest, "messages must contain at least one message")
	}
	msgs, err := s.prepare(ctx, req.Messages)
	if err != nil {
		return Response{}, err
	}
	req.Messages = msgs
	return s.ad.Chat(ctx, req, onDelta)
}

// prepare checks the images of messages against the engine and replaces their
// URLs with decoded pixels. The input is not modified.
func (s *Service) prepare(ctx context.Context, in []Message) ([]Message, error) {
	n := 0
	for _, m := range in {
		for _, p := range m.Parts {
			if p.ImageURL != "" {
				n++
			}
		}
	}
	if n == 0 {
		return in, nil
	}
	if !s.info.Vision || s.info.ImageInput == nil {
		return nil, errs.New(errs.UnsupportedCapability, "The loaded text model does not support image input.")
	}
	if n > s.info.MaxImages {
		return nil, errs.New(errs.UnsupportedCapability, "The loaded text model accepts at most %d image(s) per request.", s.info.MaxImages)
	}
	// One slot per request bounds concurrent downloads and decodes; the
	// images of a request are processed one after the other to bound memory.
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, errs.Wrap(errs.Timeout, ctx.Err(), "request cancelled")
	}
	defer func() { <-s.sem }()

	out := make([]Message, len(in))
	for i, m := range in {
		out[i] = Message{Role: m.Role, Parts: make([]Part, len(m.Parts))}
		for j, p := range m.Parts {
			if p.ImageURL == "" {
				out[i].Parts[j] = p
				continue
			}
			im, err := s.pre.Prepare(ctx, decision.ImageSource{URL: p.ImageURL}, *s.info.ImageInput)
			if err != nil {
				where := fmt.Sprintf("messages[%d].content[%d]", i, j)
				if e, ok := err.(*errs.Error); ok {
					return nil, &errs.Error{Kind: e.Kind, Message: where + ": " + e.Message, Err: e.Err}
				}
				return nil, errs.Wrap(errs.ImageFetchFailed, err, "%s", where)
			}
			out[i].Parts[j] = Part{Image: &Image{Width: im.Width, Height: im.Height, Pixels: im.Pixels}}
		}
	}
	return out, nil
}
