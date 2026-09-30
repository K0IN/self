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
)

// Adapter implements decision.Adapter.
type Adapter struct {
	mu     sync.Mutex // serializes Decide; the daemon is strictly sequential
	proc   *runtime.Supervisor
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
	a.proc = runtime.Supervise(proc, "decision engine")
	a.out = bufio.NewReaderSize(proc.Stdout(), 64<<10)
	go func() { <-proc.Done(); close(a.done) }()

	err = runtime.Handshake(ctx, a.proc, func() error {
		line, err := a.readLine()
		if err != nil {
			return err
		}
		var r readyLine
		if json.Unmarshal(line, &r) != nil || r.Status != "ready" {
			return fmt.Errorf("unexpected engine handshake: %.200s", line)
		}
		return nil
	})
	if err != nil {
		return err
	}

	caps := decision.Capabilities{Text: true, Choice: true, Score: true, Noul: true, MaxOptions: maxOptions(md)}
	a.info = decision.RuntimeInfo{
		EngineModel:  firstNonEmpty(ggufmeta.String(md, "laya.model_name"), ggufmeta.String(md, "kev.model_name"), ggufmeta.String(md, "general.name")),
		Device:       cfg.Device,
		Capabilities: caps,
	}
	return nil
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
		return decision.Response{}, a.proc.CrashErr(nil)
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
		return decision.Response{}, a.proc.CrashErr(err)
	}

	var resp []byte
	if err := a.proc.Await(requestTimeout, func() (err error) { resp, err = a.readLine(); return }); err != nil {
		return decision.Response{}, err
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

// Close stops the engine gracefully.
func (a *Adapter) Close(ctx context.Context) error { return a.proc.Close(ctx) }

// StderrTail exposes recent engine stderr for crash reports.
func (a *Adapter) StderrTail(n int) []string { return a.proc.StderrTail(n) }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
