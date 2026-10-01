// Package audio runs the persistent ggmlc-audio engine (engines/ggmlc-audio)
// over SELFIPC1. The model is loaded once in Start and serves every request;
// reference audio and the generated WAV travel as frame attachments.
package audio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	dom "ai-server/internal/audio"
	"ai-server/internal/errs"
	"ai-server/internal/ipc"
	"ai-server/internal/runtime"
	"ai-server/internal/settings"
)

// Engine is the bundled executable name.
const Engine = "ggmlc-audio"

// Settings are the engine parameters. Keep in sync with engines/ggmlc-audio/main.cpp.
var Settings = settings.Schema{
	{Name: "context_size", Flag: "--ctx", Kind: settings.Int, Min: 512, Max: 262144, Help: "context size in tokens (reference audio + text + frames); default 4096"},
	{Name: "threads", Flag: "--threads", Kind: settings.Int, Min: 1, Max: 1024, Help: "CPU threads; default half the cores"},
	{Name: "gpu_layers", Flag: "--gpu-layers", Kind: settings.Int, Min: -1, Max: 10000, Help: "layers offloaded to the GPU; -1 = all (default), 0 = CPU"},
	{Name: "temperature", Flag: "--temperature", Kind: settings.Float, Min: 0.01, Max: 10, Help: "sampling temperature; default 0.8"},
	{Name: "top_p", Flag: "--top-p", Kind: settings.Float, Min: 0, Max: 1, Help: "nucleus sampling; default 0.95"},
	{Name: "top_k", Flag: "--top-k", Kind: settings.Int, Min: 1, Max: 1000, Help: "top-k sampling; default 40 (1 = greedy, deterministic)"},
	{Name: "frames", Flag: "--max-frames", Kind: settings.Int, Min: 1, Max: 100000, Help: "maximum generated audio frames per request; default 512"},
	{Name: "seed", Flag: "--seed", Kind: settings.Int, Min: 0, Max: 4294967295, Help: "RNG seed; default random (only the first request after start repeats exactly)"},
}

// RequestTimeout kills an engine that does not answer a request in time.
const RequestTimeout = 5 * time.Minute

// Args are the engine flags for model, projector, device and settings.
func Args(cfg dom.RuntimeConfig) []string {
	args := []string{"--model", cfg.Files.Model, "--mmproj", cfg.Files.MMProj, "--device", cfg.Device}
	return append(args, Settings.Args(cfg.Settings)...)
}

// New returns an adapter; the engine starts in Start.
func New() dom.Adapter { return &Adapter{done: make(chan struct{})} }

// Adapter implements audio.Adapter over SELFIPC1.
type Adapter struct {
	mu     sync.Mutex // one request at a time
	proc   *runtime.Supervisor
	r      *ipc.Reader
	w      *ipc.Writer
	info   dom.RuntimeInfo
	voice  []byte // default reference voice, sent when a request has none
	nextID uint64
	done   chan struct{}
}

type readyMsg struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	Device   string `json:"device"`
	Audio    struct {
		Pipeline   string `json:"pipeline"`
		SampleRate int    `json:"sample_rate"`
	} `json:"audio"`
}

// Start launches the engine and waits until it has loaded the model.
func (a *Adapter) Start(ctx context.Context, cfg dom.RuntimeConfig) error {
	if cfg.Files.MMProj == "" {
		return errs.New(errs.UnsupportedModel, "%s: audio models need an mmproj file (role: mmproj)", cfg.ModelID)
	}
	if cfg.Files.Voice != "" {
		voice, err := os.ReadFile(cfg.Files.Voice)
		if err != nil {
			return errs.Wrap(errs.RuntimeStartFailed, err, "cannot read the default voice")
		}
		a.voice = voice
	}
	proc, err := runtime.Start(runtime.Spec{
		Path:      cfg.EnginePath,
		Args:      Args(cfg),
		LibDir:    cfg.LibDir,
		Log:       cfg.Log,
		LogPrefix: "[engine] ",
	})
	if err != nil {
		return errs.Wrap(errs.RuntimeStartFailed, err, "cannot start engine %s", cfg.EnginePath)
	}
	a.proc = runtime.Supervise(proc, "audio engine")
	a.r = ipc.NewReader(proc.Stdout(), ipc.DefaultLimits())
	a.w = ipc.NewWriter(proc.Stdin(), ipc.DefaultLimits())
	go func() { <-proc.Done(); close(a.done) }()

	var first ipc.Frame
	if err := runtime.Handshake(ctx, a.proc, func() (err error) { first, err = a.r.ReadFrame(); return }); err != nil {
		return err
	}
	var m readyMsg
	if err := json.Unmarshal(first.Header, &m); err != nil || m.Type != "ready" || m.Protocol != 1 || m.Audio.SampleRate <= 0 {
		a.proc.Kill()
		return errs.New(errs.RuntimeStartFailed, "invalid audio engine handshake: %s", first.Header)
	}
	a.info = dom.RuntimeInfo{EngineModel: m.Audio.Pipeline, Device: m.Device, SampleRate: m.Audio.SampleRate}
	if a.info.Device == "" {
		a.info.Device = cfg.Device
	}
	return nil
}

func (a *Adapter) Info(context.Context) (dom.RuntimeInfo, error) { return a.info, nil }
func (a *Adapter) Done() <-chan struct{}                         { return a.done }

// Close stops the engine gracefully (stdin EOF, then SIGTERM, then SIGKILL).
func (a *Adapter) Close(ctx context.Context) error { return a.proc.Close(ctx) }

// StderrTail exposes recent engine stderr for crash reports.
func (a *Adapter) StderrTail(n int) []string { return a.proc.StderrTail(n) }

type synthParams struct {
	Input             string `json:"input"`
	Language          string `json:"language,omitempty"`
	SpeakerAttachment *int   `json:"speaker_attachment,omitempty"`
}

type synthResult struct {
	SampleRate int `json:"sample_rate"`
}

// Synthesize sends one request to the running engine. A request in flight is
// not interrupted by the caller; a hung engine is killed after RequestTimeout.
func (a *Adapter) Synthesize(_ context.Context, req dom.Request) (dom.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.done:
		return dom.Response{}, a.proc.CrashErr(nil)
	default:
	}
	a.nextID++
	id := a.nextID
	p := synthParams{Input: req.Input, Language: req.Language}
	var attachments [][]byte
	voice := req.Voice.Audio
	if len(voice) == 0 {
		voice = a.voice
	}
	if len(voice) > 0 {
		zero := 0
		p.SpeakerAttachment = &zero
		attachments = append(attachments, voice)
	}
	params, _ := json.Marshal(p)
	header, _ := json.Marshal(ipc.Request{ID: id, Method: "synthesize", Params: params})
	if err := a.w.WriteFrame(ipc.Frame{Header: header, Attachments: attachments}); err != nil {
		if errors.Is(err, ipc.ErrTooLarge) {
			return dom.Response{}, errs.New(errs.RequestTooLarge, "reference audio exceeds the engine frame limit")
		}
		return dom.Response{}, a.proc.CrashErr(err)
	}

	var f ipc.Frame
	if err := a.proc.Await(RequestTimeout, func() (err error) { f, err = a.r.ReadFrame(); return }); err != nil {
		return dom.Response{}, err
	}
	resp, err := ipc.ParseResponse(f, id)
	if err != nil {
		a.proc.Kill() // stream state unknown after a protocol violation
		return dom.Response{}, errs.Wrap(errs.RuntimeCrashed, err, "audio engine protocol error")
	}
	if resp.Error != nil {
		if resp.Error.Type == "invalid_request" {
			return dom.Response{}, errs.New(errs.InvalidRequest, "%s", resp.Error.Message)
		}
		return dom.Response{}, errs.New(errs.Internal, "audio engine error: %s", resp.Error.Message)
	}
	var result synthResult
	if err := json.Unmarshal(resp.Result, &result); err != nil || len(f.Attachments) != 1 || len(f.Attachments[0]) < 44 || string(f.Attachments[0][:4]) != "RIFF" {
		a.proc.Kill()
		return dom.Response{}, errs.New(errs.RuntimeCrashed, "audio engine returned no WAV audio")
	}
	return dom.Response{WAV: f.Attachments[0], SampleRate: result.SampleRate}, nil
}
