package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dom "ai-server/internal/audio"
	"ai-server/internal/errs"
	"ai-server/internal/ipc"
)

// The test binary doubles as a fake ggmlc-audio engine when AUDIO_FAKE is set.
func TestMain(m *testing.M) {
	if mode := os.Getenv("AUDIO_FAKE"); mode != "" {
		os.Exit(fakeEngine(mode))
	}
	os.Exit(m.Run())
}

func fakeEngine(mode string) int {
	w := ipc.NewWriter(os.Stdout, ipc.DefaultLimits())
	r := ipc.NewReader(os.Stdin, ipc.DefaultLimits())
	if mode == "no-ready" {
		fmt.Fprintln(os.Stderr, "load failed: cannot load model")
		return 1
	}
	w.WriteFrame(ipc.Frame{Header: []byte(`{"type":"ready","protocol":1,"device":"cpu","audio":{"pipeline":"qwen3tts","sample_rate":24000}}`)})
	for n := 1; ; n++ {
		f, err := r.ReadFrame()
		if err == io.EOF {
			return 0
		}
		if err != nil {
			return 3
		}
		var req struct {
			ID     uint64 `json:"id"`
			Params struct {
				Input             string `json:"input"`
				Language          string `json:"language"`
				SpeakerAttachment *int   `json:"speaker_attachment"`
			} `json:"params"`
		}
		json.Unmarshal(f.Header, &req)
		if mode == "crash" {
			fmt.Fprintln(os.Stderr, "segfault in vocoder")
			return 139
		}
		if req.Params.Input == "bad" {
			w.WriteFrame(ipc.Frame{Header: []byte(fmt.Sprintf(`{"id":%d,"error":{"type":"invalid_request","message":"cannot decode the reference audio"}}`, req.ID))})
			continue
		}
		speaker := -1
		if req.Params.SpeakerAttachment != nil {
			speaker = len(f.Attachments[*req.Params.SpeakerAttachment])
		}
		// The "WAV" reports which process answered and how many requests it has served.
		wav := fmt.Sprintf("RIFF%-60s", fmt.Sprintf("pid=%d n=%d speaker=%d lang=%s", os.Getpid(), n, speaker, req.Params.Language))
		w.WriteFrame(ipc.Frame{Header: []byte(fmt.Sprintf(`{"id":%d,"result":{"sample_rate":24000}}`, req.ID)), Attachments: [][]byte{[]byte(wav)}})
	}
}

func startFake(t *testing.T, mode string, voice ...string) (*Adapter, error) {
	t.Helper()
	t.Setenv("AUDIO_FAKE", mode)
	a := New().(*Adapter)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	files := dom.ModelFiles{Model: "m.gguf", MMProj: "p.gguf"}
	if len(voice) > 0 {
		files.Voice = voice[0]
	}
	err := a.Start(ctx, dom.RuntimeConfig{ModelID: "fake:1b", EnginePath: os.Args[0], Device: "cpu", Files: files})
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	return a, err
}

// The model loads once in Start; every request is served by that one process.
func TestEngineStaysLoaded(t *testing.T) {
	a, err := startFake(t, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := a.Info(context.Background()); info.SampleRate != 24000 || info.EngineModel != "qwen3tts" {
		t.Fatalf("info = %+v", info)
	}
	var replies []string
	for i, voice := range [][]byte{nil, []byte("RIFF-reference-audio")} {
		res, err := a.Synthesize(context.Background(), dom.Request{Input: "hello", Language: "en", Voice: dom.Voice{Audio: voice}})
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		replies = append(replies, strings.TrimSpace(string(res.WAV[4:])))
	}
	pid := strings.Fields(replies[0])[0]
	if want := []string{pid + " n=1 speaker=-1 lang=en", pid + " n=2 speaker=20 lang=en"}; replies[0] != want[0] || replies[1] != want[1] {
		t.Fatalf("replies = %q, want %q (one process, requests counted, voice as attachment)", replies, want)
	}
}

func TestEngineErrors(t *testing.T) {
	if _, err := startFake(t, "no-ready"); errs.KindOf(err) != errs.RuntimeStartFailed || !strings.Contains(err.Error(), "cannot load model") {
		t.Fatalf("no-ready: %v", err)
	}

	a, err := startFake(t, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Synthesize(context.Background(), dom.Request{Input: "bad"}); errs.KindOf(err) != errs.InvalidRequest {
		t.Fatalf("engine invalid_request: %v", err)
	}
	if _, err := a.Synthesize(context.Background(), dom.Request{Input: "still alive"}); err != nil {
		t.Fatalf("engine did not survive a bad request: %v", err)
	}

	a, err = startFake(t, "crash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Synthesize(context.Background(), dom.Request{Input: "hello"}); errs.KindOf(err) != errs.RuntimeCrashed || !strings.Contains(err.Error(), "segfault in vocoder") {
		t.Fatalf("crash: %v", err)
	}
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after the engine exited")
	}
}

func TestDefaultVoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.wav")
	if err := os.WriteFile(path, []byte("RIFF-default"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := startFake(t, "ok", path)
	if err != nil {
		t.Fatal(err)
	}
	for voice, want := range map[string]string{"": "speaker=12 ", "RIFF-request-voice": "speaker=18 "} {
		res, err := a.Synthesize(context.Background(), dom.Request{Input: "hi", Voice: dom.Voice{Audio: []byte(voice)}})
		if err != nil || !strings.Contains(string(res.WAV), want) {
			t.Fatalf("voice %q: %q %v, want %s", voice, res.WAV, err, want)
		}
	}
	if _, err := startFake(t, "ok", filepath.Join(t.TempDir(), "missing.wav")); errs.KindOf(err) != errs.RuntimeStartFailed {
		t.Fatalf("missing default voice: %v", err)
	}
}

func TestStartNeedsMMProj(t *testing.T) {
	err := New().Start(context.Background(), dom.RuntimeConfig{ModelID: "x:1b", Files: dom.ModelFiles{Model: "m.gguf"}})
	if errs.KindOf(err) != errs.UnsupportedModel {
		t.Fatalf("err = %v", err)
	}
}
