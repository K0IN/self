// Package selfipc is a generic decision.Adapter for native engines that speak
// the SELFIPC1 framed protocol (internal/ipc) on stdin/stdout.
//
// Any engine implementing the protocol below can be onboarded with a Spec
// (command line + optional model check) and one line in the adapter table;
// no protocol or process code has to be written again.
//
// Engine -> server, first frame:
//
//	{"type":"ready","protocol":1,"model":"...","device":"...",
//	 "capabilities":{"text":true,"choice":true,"score":true,"noul":true,"max_options":10,
//	   "vision":{"enabled":true,"max_images":1,"input":{"mode":"bounded","max_width":1536,...}}}}
//
// Server -> engine request (raw RGB8 images travel as attachments):
//
//	{"id":7,"method":"systemone","params":{"state":<json>,
//	  "questions":[{"id":"q","type":"choice","instructions":"...","criteria":{"a":"...","b":null}}],
//	  "images":[{"attachment":0,"width":448,"height":448,"format":"rgb8","name":"front"}]}}
//
// Engine -> server response:
//
//	{"id":7,"result":{"answers":{<System One answers>},"usage":{"input_tokens":123,"latency_ms":4.2}}}
//	{"id":7,"error":{"type":"invalid_request","message":"..."}}
package selfipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/ipc"
	"ai-server/internal/runtime"
)

// Spec describes one engine.
type Spec struct {
	// Args builds the engine arguments from the runtime config.
	Args func(cfg decision.RuntimeConfig) []string
	// Check validates the model files before the engine is started
	// (optional). Returned errors should explain why the model cannot run.
	Check func(cfg decision.RuntimeConfig) error
	// Env adds environment variables for the engine (optional).
	Env []string
	// RequestTimeout kills a hung engine (default 2 minutes).
	RequestTimeout time.Duration
}

// Factory returns a decision.Factory for spec.
func Factory(spec Spec) decision.Factory {
	if spec.RequestTimeout <= 0 {
		spec.RequestTimeout = 2 * time.Minute
	}
	return func() decision.Adapter { return &Adapter{spec: spec, done: make(chan struct{})} }
}

// Adapter implements decision.Adapter over SELFIPC1.
type Adapter struct {
	spec Spec

	mu     sync.Mutex // one request at a time
	proc   *runtime.Supervisor
	r      *ipc.Reader
	w      *ipc.Writer
	info   decision.RuntimeInfo
	nextID uint64
	done   chan struct{}
}

type readyMsg struct {
	Type         string `json:"type"`
	Protocol     int    `json:"protocol"`
	Model        string `json:"model"`
	Device       string `json:"device"`
	Capabilities struct {
		Text       *bool `json:"text"`
		Choice     bool  `json:"choice"`
		Score      bool  `json:"score"`
		Noul       bool  `json:"noul"`
		MaxOptions int   `json:"max_options"`
		Vision     struct {
			Enabled   bool                    `json:"enabled"`
			MaxImages int                     `json:"max_images"`
			Input     *decision.ImageGeometry `json:"input"`
		} `json:"vision"`
	} `json:"capabilities"`
}

// Start launches the engine and waits for its ready frame.
func (a *Adapter) Start(ctx context.Context, cfg decision.RuntimeConfig) error {
	if a.spec.Check != nil {
		if err := a.spec.Check(cfg); err != nil {
			var e *errs.Error
			if errors.As(err, &e) {
				return err
			}
			return errs.New(errs.UnsupportedModel, "%s: %s", cfg.ModelID, err)
		}
	}
	proc, err := runtime.Start(runtime.Spec{
		Path:      cfg.EnginePath,
		Args:      a.spec.Args(cfg),
		Env:       a.spec.Env,
		LibDir:    cfg.LibDir,
		Log:       cfg.Log,
		LogPrefix: "[engine] ",
	})
	if err != nil {
		return errs.Wrap(errs.RuntimeStartFailed, err, "cannot start engine %s", cfg.EnginePath)
	}
	a.proc = runtime.Supervise(proc, "decision engine")
	a.r = ipc.NewReader(proc.Stdout(), ipc.DefaultLimits())
	a.w = ipc.NewWriter(proc.Stdin(), ipc.DefaultLimits())
	go func() { <-proc.Done(); close(a.done) }()

	var first ipc.Frame
	err = runtime.Handshake(ctx, a.proc, func() (err error) { first, err = a.r.ReadFrame(); return })
	if err != nil {
		return err
	}
	info, err := parseReady(first.Header, cfg.Device)
	if err != nil {
		a.proc.Kill()
		return errs.Wrap(errs.RuntimeStartFailed, err, "invalid engine handshake")
	}
	a.info = info
	return nil
}

func parseReady(header []byte, device string) (decision.RuntimeInfo, error) {
	var m readyMsg
	if err := json.Unmarshal(header, &m); err != nil {
		return decision.RuntimeInfo{}, err
	}
	if m.Type != "ready" {
		return decision.RuntimeInfo{}, fmt.Errorf("first frame has type %q, want \"ready\"", m.Type)
	}
	if m.Protocol != 1 {
		return decision.RuntimeInfo{}, fmt.Errorf("engine speaks protocol %d, this server speaks 1", m.Protocol)
	}
	c := m.Capabilities
	caps := decision.Capabilities{
		Text:       c.Text == nil || *c.Text,
		Choice:     c.Choice,
		Score:      c.Score,
		Noul:       c.Noul,
		MaxOptions: c.MaxOptions,
	}
	info := decision.RuntimeInfo{EngineModel: m.Model, Device: m.Device, Capabilities: caps}
	if info.Device == "" {
		info.Device = device
	}
	if c.Vision.Enabled {
		if c.Vision.Input == nil {
			return info, fmt.Errorf("engine reports vision without an input geometry")
		}
		switch c.Vision.Input.Mode {
		case decision.GeometryFixed, decision.GeometryBounded, decision.GeometryDynamic:
		default:
			return info, fmt.Errorf("unknown image geometry mode %q", c.Vision.Input.Mode)
		}
		info.Capabilities.Vision = true
		info.Capabilities.MaxImages = max(1, c.Vision.MaxImages)
		info.Capabilities.MultiImage = info.Capabilities.MaxImages > 1
		info.ImageInput = c.Vision.Input
	}
	return info, nil
}

// Info returns what the engine reported in its handshake.
func (a *Adapter) Info(context.Context) (decision.RuntimeInfo, error) { return a.info, nil }

// Done is closed when the engine exits.
func (a *Adapter) Done() <-chan struct{} { return a.done }

// Decide sends one request. In-flight passes are never interrupted by the
// caller; a hung engine is killed after RequestTimeout.
func (a *Adapter) Decide(_ context.Context, req decision.Request) (decision.Response, error) {
	if err := a.info.Capabilities.CheckQuestions(req.Questions); err != nil {
		return decision.Response{}, errs.New(errs.UnsupportedCapability, "%s", err)
	}
	if len(req.Images) > 0 && !a.info.Capabilities.Vision {
		return decision.Response{}, errs.New(errs.UnsupportedCapability, "The loaded decision model does not support image input.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.done:
		return decision.Response{}, a.proc.CrashErr(nil)
	default:
	}
	a.nextID++
	id := a.nextID
	frame, err := encodeRequest(id, req)
	if err != nil {
		return decision.Response{}, errs.Wrap(errs.Internal, err, "encode engine request")
	}
	if err := a.w.WriteFrame(frame); err != nil {
		if errors.Is(err, ipc.ErrTooLarge) {
			return decision.Response{}, errs.New(errs.ImageTooLarge, "request exceeds the engine frame limit")
		}
		return decision.Response{}, a.proc.CrashErr(err)
	}

	var f ipc.Frame
	if err := a.proc.Await(a.spec.RequestTimeout, func() (err error) { f, err = a.r.ReadFrame(); return }); err != nil {
		return decision.Response{}, err
	}
	out, err := decodeResponse(f, id, req.Questions)
	if err != nil {
		var ee *engineError
		if errors.As(err, &ee) {
			if ee.Type == "invalid_request" {
				return decision.Response{}, errs.New(errs.InvalidRequest, "%s", ee.Message)
			}
			return decision.Response{}, errs.New(errs.Internal, "engine error: %s", ee.Message)
		}
		a.proc.Kill() // stream state unknown after a protocol violation
		return decision.Response{}, errs.Wrap(errs.RuntimeCrashed, err, "engine protocol error")
	}
	return out, nil
}

// Close stops the engine gracefully (stdin EOF, then SIGTERM, then SIGKILL).
func (a *Adapter) Close(ctx context.Context) error { return a.proc.Close(ctx) }

// StderrTail exposes recent engine stderr for crash reports.
func (a *Adapter) StderrTail(n int) []string { return a.proc.StderrTail(n) }
