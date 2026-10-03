// Package text runs the upstream llama-server process and adapts its local
// OpenAI-compatible API to the typed text domain.
package text

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/ggufmeta"
	"ai-server/internal/runtime"
	"ai-server/internal/settings"
	dom "ai-server/internal/text"
)

const Engine = "llama-server"

var Settings = settings.Schema{
	{Name: "context_size", Flag: "--ctx-size", Kind: settings.Int, Min: 512, Max: 1048576, Help: "context size in tokens; default from model"},
	{Name: "threads", Flag: "--threads", Kind: settings.Int, Min: 1, Max: 1024, Help: "CPU threads"},
	{Name: "gpu_layers", Flag: "--gpu-layers", Kind: settings.Int, Min: -1, Max: 10000, Help: "layers offloaded to GPU; -1 = all"},
	{Name: "flash_attn", Flag: "--flash-attn", Kind: settings.String, Enum: []string{"auto", "on", "off"}, Help: "Flash Attention mode"},
	{Name: "parallel", Flag: "--parallel", Kind: settings.Int, Min: 1, Max: 256, Help: "parallel llama-server slots"},
	{Name: "temperature", Flag: "", Kind: settings.Float, Min: 0, Max: 2, Help: "default request temperature"},
	{Name: "top_p", Flag: "", Kind: settings.Float, Min: 0, Max: 1, Help: "default request top-p"},
	{Name: "top_k", Flag: "", Kind: settings.Int, Min: 0, Max: 100000, Help: "default request top-k"},
	{Name: "min_p", Flag: "", Kind: settings.Float, Min: 0, Max: 1, Help: "default request min-p"},
	{Name: "presence_penalty", Flag: "", Kind: settings.Float, Min: -2, Max: 2, Help: "default request presence penalty"},
	{Name: "thinking", Flag: "", Kind: settings.String, Enum: []string{"on", "off"}, Help: "default thinking mode"},
	{Name: "pooling", Flag: "", Kind: settings.String, Enum: []string{"none", "mean", "cls", "last", "rank"}, Help: "embedding pooling type"},
}

const requestTimeout = 10 * time.Minute

func Args(cfg dom.RuntimeConfig, port int) []string {
	args := []string{"--model", cfg.Files.Model, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--no-webui", "--jinja"}
	if cfg.Embedding {
		args = append(args, "--embedding")
		if cfg.Pooling != "" {
			args = append(args, "--pooling", cfg.Pooling)
		}
	}
	if cfg.Device == "cpu" {
		args = append(args, "--device", "none")
	}
	if cfg.Files.MMProj != "" {
		args = append(args, "--mmproj", cfg.Files.MMProj)
	}
	return append(args, Settings.Args(cfg.Settings)...)
}

type Adapter struct {
	mu              sync.Mutex
	proc            *runtime.Supervisor
	client          *http.Client
	baseURL         string
	info            dom.RuntimeInfo
	defaultThinking bool
	done            chan struct{}
	socketDir       string
}

func New() dom.Adapter { return &Adapter{done: make(chan struct{})} }

type openAIModels struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (a *Adapter) Start(ctx context.Context, cfg dom.RuntimeConfig) error {
	if cfg.Files.MMProj != "" {
		if _, err := os.Stat(cfg.Files.MMProj); err != nil {
			return errs.Wrap(errs.UnsupportedModel, err, "%s: cannot access the configured mmproj file", cfg.ModelID)
		}
	}
	md, err := ggufmeta.ReadFile(cfg.Files.Model)
	if err == nil {
		if err := CheckModel(md); err != nil {
			return errs.New(errs.UnsupportedModel, "%s: %s", cfg.ModelID, err)
		}
	}
	args := Args(cfg, 0)
	a.client = &http.Client{Timeout: requestTimeout}
	if goruntime.GOOS == "windows" {
		port, err := freePort()
		if err != nil {
			return errs.Wrap(errs.RuntimeStartFailed, err, "find a private llama-server port")
		}
		args = Args(cfg, port)
		a.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)
	} else {
		dir, err := os.MkdirTemp("", "self-llama-")
		if err != nil {
			return errs.Wrap(errs.RuntimeStartFailed, err, "create private llama-server socket directory")
		}
		a.socketDir = dir
		socketPath := filepath.Join(dir, "engine.sock")
		args[3] = socketPath
		a.baseURL = "http://llama-server"
		a.client.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}}
	}
	proc, err := runtime.Start(runtime.Spec{Path: cfg.EnginePath, Args: args, LibDir: cfg.LibDir, Log: cfg.Log, LogPrefix: "[llama-server] "})
	if err != nil {
		if a.socketDir != "" {
			_ = os.RemoveAll(a.socketDir)
		}
		return errs.Wrap(errs.RuntimeStartFailed, err, "cannot start llama-server")
	}
	a.proc = runtime.Supervise(proc, "llama-server")
	go func() {
		io.Copy(io.Discard, proc.Stdout())
		<-proc.Done()
		a.client.CloseIdleConnections()
		if a.socketDir != "" {
			_ = os.RemoveAll(a.socketDir)
		}
		close(a.done)
	}()
	if err := a.waitReady(ctx); err != nil {
		a.proc.Kill()
		return err
	}
	a.defaultThinking = stringSetting(cfg.Settings, "thinking") != "off"
	vision := cfg.Files.MMProj != ""
	info := dom.RuntimeInfo{EngineModel: cfg.ModelID, Device: cfg.Device, ContextSize: intSetting(cfg.Settings, "context_size"), Reasoning: true, Vision: vision, MaxImages: 1}
	if vision {
		info.ImageInput = &decision.ImageGeometry{Mode: decision.GeometryBounded, MaxWidth: 1536, MaxHeight: 1536, Resize: decision.ResizeContain}
	}
	a.info = info
	return nil
}

func (a *Adapter) waitReady(ctx context.Context) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/models", nil)
		resp, err := a.client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errs.Wrap(errs.RuntimeStartFailed, ctx.Err(), "llama-server did not become ready")
		case <-a.done:
			return errs.New(errs.RuntimeStartFailed, "llama-server exited before becoming ready")
		case <-t.C:
		}
	}
}

func (a *Adapter) Info(context.Context) (dom.RuntimeInfo, error) { return a.info, nil }
func (a *Adapter) Done() <-chan struct{}                         { return a.done }
func (a *Adapter) StderrTail(n int) []string                     { return a.proc.StderrTail(n) }
func (a *Adapter) Close(ctx context.Context) error               { return a.proc.Close(ctx) }

func (a *Adapter) Chat(ctx context.Context, req dom.Request, onDelta func(dom.Delta)) (dom.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return dom.Response{}, errs.Wrap(errs.Timeout, err, "request cancelled")
	}
	if req.Thinking == nil {
		req.Thinking = &a.defaultThinking
	}
	body, err := encode(req, onDelta != nil)
	if err != nil {
		return dom.Response{}, err
	}
	b, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return dom.Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(hreq)
	if err != nil {
		return dom.Response{}, a.proc.CrashErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dom.Response{}, decodeHTTPError(resp)
	}
	if onDelta == nil {
		var out openAICompletion
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return dom.Response{}, errs.Wrap(errs.RuntimeCrashed, err, "invalid llama-server response")
		}
		return convertCompletion(out), nil
	}
	return readSSE(resp.Body, onDelta)
}

func (a *Adapter) Embed(ctx context.Context, input string) ([]float32, int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"input": input, "encoding_format": "float"})
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(hreq)
	if err != nil {
		return nil, 0, a.proc.CrashErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, decodeHTTPError(resp)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, errs.Wrap(errs.RuntimeCrashed, err, "invalid llama-server embedding response")
	}
	if len(out.Data) == 0 {
		return nil, 0, errs.New(errs.RuntimeCrashed, "llama-server returned no embedding")
	}
	return out.Data[0].Embedding, out.Usage.PromptTokens, nil
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}
type openAIRequest struct {
	Model              string          `json:"model"`
	Messages           []openAIMessage `json:"messages"`
	MaxTokens          int             `json:"max_tokens,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	TopP               *float64        `json:"top_p,omitempty"`
	PresencePenalty    *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty   *float64        `json:"frequency_penalty,omitempty"`
	Seed               *int64          `json:"seed,omitempty"`
	Stop               []string        `json:"stop,omitempty"`
	Stream             bool            `json:"stream,omitempty"`
	ResponseFormat     any             `json:"response_format,omitempty"`
	ChatTemplateKwargs map[string]any  `json:"chat_template_kwargs,omitempty"`
}
type openAICompletion struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
		} `json:"message"`
		Finish string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
	} `json:"usage"`
}
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
		} `json:"delta"`
		Finish string `json:"finish_reason"`
	} `json:"choices"`
}

func encode(req dom.Request, stream bool) (openAIRequest, error) {
	p := openAIRequest{Model: "self", MaxTokens: req.MaxTokens, Temperature: req.Temperature, TopP: req.TopP, PresencePenalty: req.PresencePenalty, FrequencyPenalty: req.FrequencyPenalty, Seed: req.Seed, Stop: req.Stop, Stream: stream}
	for _, m := range req.Messages {
		var parts []any
		for _, part := range m.Parts {
			if part.Image == nil {
				parts = append(parts, map[string]any{"type": "text", "text": part.Text})
				continue
			}
			dataURL, err := imageDataURL(part.Image)
			if err != nil {
				return p, err
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL}})
		}
		content := any("")
		if len(parts) == 1 {
			content = parts[0].(map[string]any)["text"]
		} else {
			content = parts
		}
		p.Messages = append(p.Messages, openAIMessage{Role: m.Role, Content: content})
	}
	if req.Thinking != nil {
		p.ChatTemplateKwargs = map[string]any{"enable_thinking": *req.Thinking}
	}
	if len(req.JSONSchema) > 0 {
		var schema any
		if err := json.Unmarshal(req.JSONSchema, &schema); err != nil {
			return p, errs.New(errs.InvalidRequest, "invalid JSON schema: %s", err)
		}
		p.ResponseFormat = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "response", "schema": schema}}
	}
	return p, nil
}

func convertCompletion(out openAICompletion) dom.Response {
	if len(out.Choices) == 0 {
		return dom.Response{}
	}
	c := out.Choices[0]
	return dom.Response{Content: c.Message.Content, Reasoning: c.Message.Reasoning, FinishReason: c.Finish, Usage: dom.Usage{PromptTokens: out.Usage.Prompt, CompletionTokens: out.Usage.Completion}}
}
func readSSE(r io.Reader, onDelta func(dom.Delta)) (dom.Response, error) {
	var out dom.Response
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64<<10), 4<<20)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var c streamChunk
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &c); err != nil {
			continue
		}
		if len(c.Choices) == 0 {
			continue
		}
		d := c.Choices[0].Delta
		onDelta(dom.Delta{Content: d.Content, Reasoning: d.Reasoning})
		out.FinishReason = c.Choices[0].Finish
	}
	if err := scan.Err(); err != nil {
		return out, errs.Wrap(errs.RuntimeCrashed, err, "read llama-server stream")
	}
	return out, nil
}
func decodeHTTPError(resp *http.Response) error {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e.Error.Message == "" {
		e.Error.Message = resp.Status
	}
	if resp.StatusCode == http.StatusBadRequest {
		return errs.New(errs.InvalidRequest, "%s", e.Error.Message)
	}
	return errs.New(errs.Internal, "llama-server: %s", e.Error.Message)
}
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
func intSetting(v settings.Values, key string) int {
	if x, ok := v[key].(int64); ok {
		return int(x)
	}
	return 0
}

func stringSetting(v settings.Values, key string) string {
	if x, ok := v[key].(string); ok {
		return x
	}
	return ""
}

func imageDataURL(im *dom.Image) (string, error) {
	if im == nil || im.Width <= 0 || im.Height <= 0 || len(im.Pixels) != im.Width*im.Height*3 {
		return "", errs.New(errs.InvalidRequest, "invalid preprocessed image")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, im.Width, im.Height))
	for y := 0; y < im.Height; y++ {
		for x := 0; x < im.Width; x++ {
			i := (y*im.Width + x) * 3
			rgba.SetRGBA(x, y, color.RGBA{R: im.Pixels[i], G: im.Pixels[i+1], B: im.Pixels[i+2], A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return "", errs.Wrap(errs.Internal, err, "encode image for llama-server")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
func CheckModel(md ggufmeta.Metadata) error {
	if ggufmeta.String(md, "general.architecture") == "" {
		return fmt.Errorf("the GGUF has no general.architecture")
	}
	if !ggufmeta.Has(md, "tokenizer.ggml.tokens") {
		return fmt.Errorf("the GGUF has no embedded tokenizer")
	}
	return nil
}
