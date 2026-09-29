package ggmlclaya

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/ggufmeta"
)

func qs(t *testing.T, s string) decision.Questions {
	var q decision.Questions
	if err := json.Unmarshal([]byte(s), &q); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestSettingsArgs(t *testing.T) {
	cfg := decision.RuntimeConfig{Device: "cuda"}
	if got := strings.Join(laArgs(cfg), " "); got != "--cuda-graph" {
		t.Fatalf("cuda default = %q", got)
	}
	cfg.Device = "cpu"
	if got := laArgs(cfg); len(got) != 0 {
		t.Fatalf("cpu default = %v", got)
	}
	v, err := Settings.Validate(map[string]any{"threads": 8, "cuda_graph": false})
	if err != nil {
		t.Fatal(err)
	}
	cfg = decision.RuntimeConfig{Device: "cuda", Settings: v}
	if got := strings.Join(laArgs(cfg), " "); got != "--threads 8" {
		t.Fatalf("explicit = %q", got)
	}
	if _, err := Settings.Validate(map[string]any{"context_size": 4096}); err == nil {
		t.Fatal("laya must reject context_size (compiled into the GGUF)")
	}
}

func TestCheckModel(t *testing.T) {
	llama := ggufmeta.Metadata{"general.architecture": "qwen35", "tokenizer.ggml.model": "gpt2"}
	if err := CheckModel(llama); err == nil || !strings.Contains(err.Error(), "ggmlc.graph_spec") {
		t.Fatalf("llama.cpp gguf accepted: %v", err)
	}
	graphOnly := ggufmeta.Metadata{"general.architecture": "ggmlc", "ggmlc.graph_spec": "{}"}
	if err := CheckModel(graphOnly); err == nil {
		t.Fatal("non-decision ggmlc gguf accepted")
	}
	kev := ggufmeta.Metadata{"ggmlc.graph_spec": "{}", "ggmlc.decision": `{"kind":"kev","max_opts":12}`}
	if err := CheckModel(kev); err != nil || maxOptions(kev) != 12 {
		t.Fatalf("kev: %v %d", err, maxOptions(kev))
	}
	laya := ggufmeta.Metadata{"ggmlc.graph_spec": "{}", "laya.model_name": "laya", "laya.max_opts": uint32(16)}
	if err := CheckModel(laya); err != nil || maxOptions(laya) != 16 {
		t.Fatalf("laya: %v", err)
	}
}

func TestTranslateResponse(t *testing.T) {
	q := qs(t, `{"refund":{"type":"noul","instructions":"x"},"dept":{"type":"choice","instructions":"y","criteria":{"billing":"a","technical":"b"}}}`)
	line := `{"model":"kev-0.5b","answers":{"dept":{"type":"choice","action":{"act_probability":0},"confidence":0.8,"choice":"billing","probabilities":{"billing":0.9,"technical":0.1}},"refund":{"type":"noul","action":{"act_probability":0},"confidence":0.7,"noul":0.3}},"usage":{"input_tokens":62,"output_tokens":0,"latency_ms":305.0},"id":7}`
	r, err := translateResponse([]byte(line), 7, q)
	if err != nil {
		t.Fatal(err)
	}
	if r.Answers[0].ID != "refund" || r.Usage.InputTokens != 62 {
		t.Fatalf("got %+v", r)
	}
	if _, err := translateResponse([]byte(line), 8, q); err == nil {
		t.Fatal("id mismatch accepted")
	}
	_, err = translateResponse([]byte(`{"id":7,"error":"missing questions"}`), 7, q)
	if _, ok := err.(*engineError); !ok {
		t.Fatalf("engine error: %v", err)
	}
	if _, err := translateResponse([]byte(`{"error":"JSON parse error"}`), 7, q); err == nil {
		t.Fatal("id-less error accepted")
	}
}

// TestIntegrationLaya runs against a real engine and GGUF when available:
//
//	SELF_TEST_ENGINE=/path/to/laya SELF_TEST_GGUF=/path/to/kev.gguf go test ./internal/adapters/ggmlclaya -run Integration -v
func TestIntegrationLaya(t *testing.T) {
	engine, model := os.Getenv("SELF_TEST_ENGINE"), os.Getenv("SELF_TEST_GGUF")
	if engine == "" || model == "" {
		t.Skip("set SELF_TEST_ENGINE and SELF_TEST_GGUF to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	a := New()
	lib := filepath.Join(filepath.Dir(engine), "lib")
	if _, err := os.Stat(lib); err != nil {
		lib = ""
	}
	dev := os.Getenv("SELF_TEST_DEVICE")
	if dev == "" {
		dev = "auto"
	}
	if err := a.Start(ctx, decision.RuntimeConfig{ModelID: "test", Files: decision.ModelFiles{Model: model}, Device: dev, EnginePath: engine, LibDir: lib}); err != nil {
		t.Fatal(err)
	}
	defer a.Close(context.Background())
	q := qs(t, `{"refund":{"type":"noul","instructions":"Does this likely require a refund?"},"dept":{"type":"choice","instructions":"Which department?","criteria":{"billing":"Payments","technical":"Tech"}},"urgency":{"type":"score","instructions":"How urgent?","criteria":["low","medium","high"]}}`)
	for i := 0; i < 2; i++ {
		r, err := a.Decide(ctx, decision.Request{State: json.RawMessage(`"I was charged twice."`), Questions: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Answers) != 3 {
			t.Fatalf("answers: %+v", r.Answers)
		}
	}
	// Option overflow must be rejected before reaching (and aborting) the engine.
	crit := map[string]string{}
	for i := 0; i < 40; i++ {
		crit[string(rune('A'+i))] = "x"
	}
	b, _ := json.Marshal(map[string]any{"q": map[string]any{"type": "choice", "instructions": "pick", "criteria": crit}})
	if _, err := a.Decide(ctx, decision.Request{State: json.RawMessage(`"x"`), Questions: qs(t, string(b))}); errs.KindOf(err) != errs.UnsupportedCapability {
		t.Fatalf("overflow: %v", err)
	}
	if _, err := a.Decide(ctx, decision.Request{State: json.RawMessage(`"still alive"`), Questions: q}); err != nil {
		t.Fatalf("engine died: %v", err)
	}
}
