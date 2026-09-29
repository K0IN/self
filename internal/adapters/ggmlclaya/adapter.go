// Package ggmlclaya adapts the upstream ggmlc Laya decision engine
// (`laya daemon`, monatis/ggmlc examples/laya) to decision.Adapter.
//
// All knowledge about the upstream executable — command line, NDJSON daemon
// protocol, GGUF compatibility rules and its failure modes — is contained
// in this package.
package ggmlclaya

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/ggufmeta"
	"ai-server/internal/runtime"
)

const (
	maxLine        = 16 << 20
	requestTimeout = 2 * time.Minute
	stopGrace      = 5 * time.Second
)

// Adapter implements decision.Adapter.
type Adapter struct {
	mu     sync.Mutex // serializes Decide; the daemon is strictly sequential
	proc   *runtime.Process
	out    *bufio.Reader
	info   decision.RuntimeInfo
	nextID uint64
	done   chan struct{}
}

// New returns an unstarted adapter.
func New() decision.Adapter { return &Adapter{done: make(chan struct{})} }

// Start launches the engine and waits for its ready line.
func (a *Adapter) Start(ctx context.Context, cfg decision.RuntimeConfig) error {
	md, err := ggufmeta.ReadFile(cfg.Files.Model)
	if err != nil {
		return errs.Wrap(errs.UnsupportedModel, err, "cannot read GGUF metadata")
	}
	if err := CheckModel(md); err != nil {
		return errs.New(errs.UnsupportedModel, "%s is not supported by the ggmlc-laya engine: %s", cfg.ModelID, err)
	}
	if cfg.Files.MMProj != "" {
		return errs.New(errs.UnsupportedModel, "%s: the ggmlc-laya engine has no image input; multimodal projector files are not supported", cfg.ModelID)
	}

	args := append([]string{"daemon", cfg.Files.Model, "--device", cfg.Device}, laArgs(cfg)...)
	proc, err := runtime.Start(runtime.Spec{
		Path:      cfg.EnginePath,
		Args:      args,
		LibDir:    cfg.LibDir,
		Log:       cfg.Log,
		LogPrefix: "[engine] ",
	})
	if err != nil {
		return errs.Wrap(errs.RuntimeStartFailed, err, "cannot start engine %s", cfg.EnginePath)
	}
	a.proc = proc
	a.out = bufio.NewReaderSize(proc.Stdout(), 64<<10)
	go func() { <-proc.Done(); close(a.done) }()

	ready := make(chan error, 1)
	go func() {
		line, err := a.readLine()
		if err != nil {
			ready <- err
			return
		}
		var r readyLine
		if json.Unmarshal(line, &r) != nil || r.Status != "ready" {
			ready <- fmt.Errorf("unexpected engine handshake: %.200s", line)
			return
		}
		ready <- nil
	}()
	select {
	case err := <-ready:
		if err != nil {
			a.proc.Kill()
			return a.startErr(err)
		}
	case <-ctx.Done():
		a.proc.Kill()
		return errs.Wrap(errs.RuntimeStartFailed, ctx.Err(), "engine start cancelled")
	}

	caps := decision.Capabilities{Text: true, Choice: true, Score: true, Noul: true, MaxOptions: maxOptions(md)}
	a.info = decision.RuntimeInfo{
		EngineModel:  firstNonEmpty(ggufmeta.String(md, "laya.model_name"), ggufmeta.String(md, "kev.model_name"), ggufmeta.String(md, "general.name")),
		Device:       cfg.Device,
		Capabilities: caps,
	}
	return nil
}

func (a *Adapter) startErr(err error) error {
	<-a.proc.Done()
	return errs.Wrap(errs.RuntimeStartFailed, err, "engine failed to start (%s)%s",
		a.proc.ExitDescription(), runtime.FormatTail(a.proc.StderrTail(15)))
}

func (a *Adapter) readLine() ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, isPrefix, err := a.out.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		buf.Write(chunk)
		if buf.Len() > maxLine {
			return nil, fmt.Errorf("engine output line exceeds %d bytes", maxLine)
		}
		if !isPrefix {
			b := bytes.TrimSpace(buf.Bytes())
			if len(b) == 0 {
				continue
			}
			return b, nil
		}
	}
}

// Info returns engine information.
func (a *Adapter) Info(context.Context) (decision.RuntimeInfo, error) { return a.info, nil }

// Done is closed when the engine exits.
func (a *Adapter) Done() <-chan struct{} { return a.done }

// Decide sends one request and waits for its response. The context is not
// used to abort an in-flight pass (the scheduler detaches it); a hung engine
// is killed after requestTimeout.
func (a *Adapter) Decide(_ context.Context, req decision.Request) (decision.Response, error) {
	if len(req.Images) > 0 {
		return decision.Response{}, errs.New(errs.UnsupportedCapability, "The loaded decision model does not support image input.")
	}
	if err := a.info.Capabilities.CheckQuestions(req.Questions); err != nil {
		return decision.Response{}, errs.New(errs.UnsupportedCapability, "%s", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.done:
		return decision.Response{}, a.crashErr(nil)
	default:
	}
	a.nextID++
	id := a.nextID
	state := req.State
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	line, err := json.Marshal(requestLine{ID: id, State: state, Questions: req.Questions})
	if err != nil {
		return decision.Response{}, errs.Wrap(errs.Internal, err, "encode engine request")
	}
	line = append(line, '\n')
	if _, err := a.proc.Stdin().Write(line); err != nil {
		return decision.Response{}, a.crashErr(err)
	}

	watchdog := time.AfterFunc(requestTimeout, a.proc.Kill)
	resp, err := a.readLine()
	stopped := watchdog.Stop()
	if err != nil {
		if !stopped {
			return decision.Response{}, errs.New(errs.RuntimeCrashed, "engine did not answer within %s and was stopped", requestTimeout)
		}
		return decision.Response{}, a.crashErr(err)
	}
	out, err := translateResponse(resp, id, req.Questions)
	if err != nil {
		var ee *engineError
		if errors.As(err, &ee) {
			return decision.Response{}, errs.New(errs.InvalidRequest, "engine rejected request: %s", ee.msg)
		}
		// A protocol violation leaves the stream in an unknown state.
		a.proc.Kill()
		return decision.Response{}, errs.Wrap(errs.RuntimeCrashed, err, "engine protocol error")
	}
	return out, nil
}

func (a *Adapter) crashErr(cause error) error {
	select {
	case <-a.done:
	case <-time.After(2 * time.Second):
		// Stream broke but the process lingers; make sure it is gone.
		a.proc.Kill()
	}
	return &errs.Error{
		Kind:    errs.RuntimeCrashed,
		Message: fmt.Sprintf("decision engine %s%s", a.proc.ExitDescription(), runtime.FormatTail(a.proc.StderrTail(15))),
		Err:     cause,
	}
}

// Close stops the engine gracefully.
func (a *Adapter) Close(ctx context.Context) error {
	if a.proc == nil {
		return nil
	}
	return a.proc.Stop(ctx, stopGrace)
}

// StderrTail exposes recent engine stderr for crash reports.
func (a *Adapter) StderrTail(n int) []string {
	if a.proc == nil {
		return nil
	}
	return a.proc.StderrTail(n)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
