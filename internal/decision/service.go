package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-server/internal/errs"
)

// ImageSource is an unprocessed image reference from a client.
type ImageSource struct {
	URL         string
	Name        string
	Description string
}

// Preprocessor turns image sources into engine-ready images.
type Preprocessor interface {
	Prepare(ctx context.Context, src ImageSource, geom ImageGeometry) (Image, error)
}

// SystemOneInput is a validated public request before preprocessing.
type SystemOneInput struct {
	State     json.RawMessage
	Images    []ImageSource
	Questions Questions
}

// ServiceConfig configures a Service.
type ServiceConfig struct {
	ModelID               string
	Quant                 string
	Capabilities          Capabilities
	ImageInput            *ImageGeometry
	QueueSize             int
	PreprocessConcurrency int
	MaxQuestions          int
	// Info and Settings are reported by /v1/model (descriptive only).
	Info     any
	Settings map[string]any
}

// Service is the decision use-case used by HTTP handlers. It validates
// requests against capabilities, preprocesses images concurrently, then
// submits ready requests to the single-runner scheduler.
type Service struct {
	cfg   ServiceConfig
	pre   Preprocessor
	sched *Scheduler
	sem   chan struct{}
}

// NewService creates a service around a started adapter.
func NewService(cfg ServiceConfig, adapter Adapter, pre Preprocessor) *Service {
	if cfg.PreprocessConcurrency < 1 {
		cfg.PreprocessConcurrency = 1
	}
	if cfg.MaxQuestions <= 0 {
		cfg.MaxQuestions = 64
	}
	s := &Service{
		cfg: cfg,
		pre: pre,
		sem: make(chan struct{}, cfg.PreprocessConcurrency),
	}
	s.sched = NewScheduler(cfg.QueueSize, adapter.Decide)
	return s
}

// ModelID returns the registry id of the loaded model.
func (s *Service) ModelID() string { return s.cfg.ModelID }

// Quant returns the loaded quant.
func (s *Service) Quant() string { return s.cfg.Quant }

// Capabilities returns the effective capabilities.
func (s *Service) Capabilities() Capabilities { return s.cfg.Capabilities }

// Info returns registry model metadata.
func (s *Service) Info() any { return s.cfg.Info }

// Settings returns the effective engine settings.
func (s *Service) Settings() map[string]any { return s.cfg.Settings }

// QueueLen returns the number of ready requests waiting for the runner.
func (s *Service) QueueLen() int { return s.sched.QueueLen() }

// Fail marks the runner unusable.
func (s *Service) Fail(err error) { s.sched.Fail(err) }

// Close stops the scheduler.
func (s *Service) Close() { s.sched.Close() }

// Validate checks a request against the loaded model without doing work.
func (s *Service) Validate(in SystemOneInput) error {
	caps := s.cfg.Capabilities
	if len(in.Questions) == 0 {
		return errs.New(errs.InvalidRequest, "questions must contain at least one question")
	}
	if len(in.Questions) > s.cfg.MaxQuestions {
		return errs.New(errs.InvalidRequest, "too many questions (%d, max %d)", len(in.Questions), s.cfg.MaxQuestions)
	}
	if err := caps.CheckQuestions(in.Questions); err != nil {
		return errs.New(errs.UnsupportedCapability, "%s", err.Error())
	}
	if n := len(in.Images); n > 0 {
		if !caps.Vision || s.cfg.ImageInput == nil {
			return errs.New(errs.UnsupportedCapability, "The loaded decision model does not support image input.")
		}
		if n > caps.MaxImages {
			return errs.New(errs.UnsupportedCapability, "The loaded decision model accepts at most %d image(s) per request.", caps.MaxImages)
		}
	}
	for i, im := range in.Images {
		if strings.TrimSpace(im.URL) == "" {
			return errs.New(errs.InvalidRequest, "images[%d]: url is required", i)
		}
	}
	return nil
}

// Decide validates, preprocesses and runs a request.
func (s *Service) Decide(ctx context.Context, in SystemOneInput) (Response, error) {
	if err := s.Validate(in); err != nil {
		return Response{}, err
	}
	images, err := s.prepareImages(ctx, in.Images)
	if err != nil {
		return Response{}, err
	}
	resp, err := s.sched.Submit(ctx, Request{State: in.State, Images: images, Questions: in.Questions})
	if err != nil {
		return Response{}, err
	}
	resp.Answers = Reorder(in.Questions, resp.Answers)
	if len(images) > 0 {
		resp.Usage.Images = len(images)
	}
	return resp, nil
}

func (s *Service) prepareImages(ctx context.Context, srcs []ImageSource) ([]Image, error) {
	if len(srcs) == 0 {
		return nil, nil
	}
	// One slot per request bounds concurrent downloads/decodes globally;
	// images of a request are processed sequentially to bound memory.
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, errs.Wrap(errs.Timeout, ctx.Err(), "request cancelled")
	}
	defer func() { <-s.sem }()
	out := make([]Image, 0, len(srcs))
	for i, src := range srcs {
		im, err := s.pre.Prepare(ctx, src, *s.cfg.ImageInput)
		if err != nil {
			var e *errs.Error
			if ok := asErr(err, &e); ok {
				return nil, &errs.Error{Kind: e.Kind, Message: fmt.Sprintf("images[%d]: %s", i, e.Message), Err: e.Err}
			}
			return nil, errs.Wrap(errs.ImageFetchFailed, err, "images[%d]", i)
		}
		out = append(out, im)
	}
	return out, nil
}

func asErr(err error, target **errs.Error) bool {
	e, ok := err.(*errs.Error)
	if ok {
		*target = e
	}
	return ok
}
