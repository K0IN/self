// Package image adapts the persistent stable-diffusion.cpp HTTP server.
package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-server/internal/errs"
	dom "ai-server/internal/image"
	"ai-server/internal/runtime"
	"ai-server/internal/settings"
)

const Engine = "sd-server"

var Settings = settings.Schema{
	{Name: "threads", Flag: "--threads", Kind: settings.Int, Min: 1, Max: 1024, Help: "CPU threads"},
	{Name: "offload_to_cpu", Flag: "--offload-to-cpu", Kind: settings.Bool, Help: "offload inactive model stages to CPU"},
	{Name: "steps", Flag: "--steps", Kind: settings.Int, Min: 1, Max: 200, Help: "sampling steps"},
	{Name: "cfg_scale", Flag: "--cfg-scale", Kind: settings.Float, Min: 0, Max: 50, Help: "guidance scale"},
	{Name: "sampling_method", Flag: "--sampling-method", Kind: settings.String, Enum: []string{"euler"}, Help: "sampling method"},
	{Name: "flash_attention", Flag: "--fa", Kind: settings.Bool, Help: "flash attention"},
	{Name: "seed", Flag: "--seed", Kind: settings.Int, Min: 0, Max: 9223372036854775807, Help: "default seed"},
}

func New() dom.Adapter { return &Adapter{done: make(chan struct{})} }

type Adapter struct {
	mu      sync.Mutex
	proc    *runtime.Supervisor
	client  *http.Client
	baseURL string
	info    dom.RuntimeInfo
	done    chan struct{}
}

func Args(cfg dom.RuntimeConfig, port int) []string {
	args := []string{"--listen-ip", "127.0.0.1", "--listen-port", strconv.Itoa(port), "--eager-load", "--diffusion-model", cfg.Files.Model}
	if cfg.Files.VAE != "" {
		args = append(args, "--vae", cfg.Files.VAE)
	}
	if cfg.Files.TextEncoder != "" {
		args = append(args, "--llm", cfg.Files.TextEncoder)
	}
	if cfg.Files.MMProj != "" {
		args = append(args, "--llm_vision", cfg.Files.MMProj)
	}
	if cfg.Device == "cpu" {
		args = append(args, "--backend", "cpu", "--params-backend", "cpu")
	} else if cfg.Device == "cuda" {
		args = append(args, "--backend", "cuda0")
	} else if strings.HasPrefix(cfg.Device, "cuda:") {
		args = append(args, "--backend", "cuda"+strings.TrimPrefix(cfg.Device, "cuda:"))
	}
	return append(args, Settings.Args(cfg.Settings)...)
}

func (a *Adapter) Start(ctx context.Context, cfg dom.RuntimeConfig) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	proc, err := runtime.Start(runtime.Spec{Path: cfg.EnginePath, Args: Args(cfg, port), LibDir: cfg.LibDir, Log: cfg.Log, LogPrefix: "[engine] "})
	if err != nil {
		return errs.Wrap(errs.RuntimeStartFailed, err, "cannot start engine %s", cfg.EnginePath)
	}
	a.proc = runtime.Supervise(proc, "image engine")
	a.client = &http.Client{Timeout: 15 * time.Minute, Transport: &http.Transport{Proxy: nil}}
	a.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)
	a.info = dom.RuntimeInfo{EngineModel: cfg.ModelID, Device: cfg.Device}
	readyCtx, cancelReady := context.WithCancel(ctx)
	defer cancelReady()
	go func() { _, _ = io.Copy(io.Discard, proc.Stdout()) }()
	go func() { <-proc.Done(); close(a.done); cancelReady() }()
	if err := waitReady(readyCtx, a.client, a.baseURL); err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = a.proc.Close(closeCtx)
		return err
	}
	return nil
}

func (a *Adapter) Info(context.Context) (dom.RuntimeInfo, error) { return a.info, nil }
func (a *Adapter) Done() <-chan struct{}                         { return a.done }
func (a *Adapter) Close(ctx context.Context) error               { return a.proc.Close(ctx) }
func (a *Adapter) StderrTail(n int) []string                     { return a.proc.StderrTail(n) }

func (a *Adapter) Generate(ctx context.Context, req dom.GenerateRequest) (dom.Response, error) {
	extra := make(map[string]any, len(req.Extra)+2)
	for key, value := range req.Extra {
		extra[key] = value
	}
	if req.Seed != nil {
		extra["seed"] = *req.Seed
	}
	if req.NegativePrompt != "" {
		extra["negative_prompt"] = req.NegativePrompt
	}
	body := map[string]any{"prompt": extendedPrompt(req.Prompt, extra), "n": req.N, "size": fmt.Sprintf("%dx%d", req.Width, req.Height), "output_format": req.OutputFormat, "output_compression": req.Compression}
	return a.doJSON(ctx, "/v1/images/generations", body)
}

func (a *Adapter) Edit(ctx context.Context, req dom.EditRequest) (dom.Response, error) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("prompt", extendedPrompt(req.Prompt, req.Extra))
	_ = mw.WriteField("n", strconv.Itoa(req.N))
	if req.Width > 0 && req.Height > 0 {
		_ = mw.WriteField("size", fmt.Sprintf("%dx%d", req.Width, req.Height))
	}
	_ = mw.WriteField("output_format", req.OutputFormat)
	_ = mw.WriteField("output_compression", strconv.Itoa(req.Compression))
	for _, img := range req.Images {
		part, err := mw.CreateFormFile("image[]", filename(img))
		if err != nil {
			return dom.Response{}, err
		}
		_, _ = part.Write(img.Bytes)
	}
	if req.Mask != nil {
		part, err := mw.CreateFormFile("mask", filename(*req.Mask))
		if err != nil {
			return dom.Response{}, err
		}
		_, _ = part.Write(req.Mask.Bytes)
	}
	_ = mw.Close()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/images/edits", &b)
	request.Header.Set("Content-Type", mw.FormDataContentType())
	return a.do(request)
}

func (a *Adapter) doJSON(ctx context.Context, path string, body any) (dom.Response, error) {
	b, _ := json.Marshal(body)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(b))
	request.Header.Set("Content-Type", "application/json")
	return a.do(request)
}
func (a *Adapter) do(request *http.Request) (dom.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := request.Context().Err(); err != nil {
		return dom.Response{}, err
	}
	resp, err := a.client.Do(request)
	if err != nil {
		return dom.Response{}, errs.Wrap(errs.RuntimeCrashed, err, "image engine request failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (256<<20)+1))
	if err != nil || len(data) > 256<<20 {
		return dom.Response{}, errs.New(errs.RuntimeCrashed, "image engine response is truncated or too large")
	}
	if resp.StatusCode/100 != 2 {
		return dom.Response{}, errs.New(errs.Internal, "image engine returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var raw struct {
		Created      int64  `json:"created"`
		OutputFormat string `json:"output_format"`
		Data         []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return dom.Response{}, errs.Wrap(errs.RuntimeCrashed, err, "invalid image engine response")
	}
	out := dom.Response{Created: raw.Created, OutputFormat: raw.OutputFormat}
	for _, item := range raw.Data {
		image, err := base64.StdEncoding.DecodeString(item.B64)
		if err != nil {
			return dom.Response{}, errs.New(errs.RuntimeCrashed, "image engine returned invalid base64")
		}
		out.Images = append(out.Images, dom.OutputImage{Bytes: image, MIME: "image/" + out.OutputFormat})
	}
	return out, nil
}
func extendedPrompt(prompt string, extra map[string]any) string {
	if len(extra) == 0 {
		return prompt
	}
	payload, err := json.Marshal(extra)
	if err != nil {
		return prompt
	}
	return prompt + " <sd_cpp_extra_args>" + string(payload) + "</sd_cpp_extra_args>"
}
func filename(img dom.InputImage) string {
	if img.Name != "" {
		return img.Name
	}
	return "image.png"
}
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
func waitReady(ctx context.Context, client *http.Client, base string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
		resp, err := client.Do(request)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errs.Wrap(errs.RuntimeStartFailed, ctx.Err(), "image engine did not become ready")
		case <-ticker.C:
		}
	}
}
