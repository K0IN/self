package models_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ai-server/internal/api"
	apidecision "ai-server/internal/api/decision"
	dec "ai-server/internal/decision"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
)

// These tests check the checks: the suite must pass on an engine that answers
// correctly and fail on one that does not. They need no models and always run.

// fakeEngine answers like a healthy model, or like a broken one.
type fakeEngine struct{ broken bool }

func (*fakeEngine) Start(context.Context, dec.RuntimeConfig) error { return nil }
func (*fakeEngine) Close(context.Context) error                    { return nil }
func (*fakeEngine) Done() <-chan struct{}                          { return nil }
func (*fakeEngine) Info(context.Context) (dec.RuntimeInfo, error)  { return dec.RuntimeInfo{}, nil }

func (f *fakeEngine) Decide(_ context.Context, req dec.Request) (dec.Response, error) {
	state := string(req.State)
	var as dec.Answers
	for _, q := range req.Questions {
		switch b := q.Body.(type) {
		case *dec.ChoiceQuestion:
			pick := b.Criteria[0].Key
			if !f.broken {
				pick = f.pick(b, req, state)
			}
			ps := make(dec.Probabilities, len(b.Criteria))
			for i, o := range b.Criteria {
				ps[i] = dec.Probability{Key: o.Key, P: 0.1 / float64(max(len(b.Criteria)-1, 1))}
				if o.Key == pick {
					ps[i].P = 0.9
				}
			}
			as = append(as, dec.Answer{ID: q.ID, Body: &dec.ChoiceAnswer{Choice: pick, Probabilities: ps, Confidence: 0.9}})
		case *dec.NoulQuestion:
			p := 0.5
			if !f.broken {
				p = 0.1
				if strings.Contains(b.Instructions, "sky") || strings.Contains(b.Instructions, "refund") {
					p = 0.9
				}
			}
			as = append(as, dec.Answer{ID: q.ID, Body: &dec.NoulAnswer{Noul: p, Confidence: p}})
		case *dec.ScoreQuestion:
			s := 0.0
			if !f.broken {
				s = min(1, float64(len(b.Criteria)-1))
				if strings.Contains(state, "URGENT") {
					s = float64(len(b.Criteria)-1) - 0.5
				}
			}
			as = append(as, dec.Answer{ID: q.ID, Body: &dec.ScoreAnswer{Score: s, Probabilities: dec.Probabilities{{Key: "0", P: 1}}}})
		}
	}
	return dec.Response{Answers: as, Usage: dec.Usage{InputTokens: len(state)/4 + 1, LatencyMS: 1}}, nil
}

func (*fakeEngine) pick(q *dec.ChoiceQuestion, req dec.Request, state string) string {
	has := func(key string) bool {
		for _, o := range q.Criteria {
			if o.Key == key {
				return true
			}
		}
		return false
	}
	if len(req.Images) > 0 {
		px := req.Images[0].Pixels
		for name, dominant := range map[string]bool{
			"red": px[0] > px[1] && px[0] > px[2], "green": px[1] > px[0] && px[1] > px[2], "blue": px[2] > px[0] && px[2] > px[1],
		} {
			if dominant && has(name) {
				return name
			}
		}
	}
	if has(billingKey) && (strings.Contains(state, "charged") || strings.Contains(state, "Kreditkarte")) {
		return billingKey
	}
	return q.Criteria[0].Key
}

type healthy struct{}

func (healthy) RunnerState() string { return "ready" }

const fakeRegistry = `
version: 1
models:
  test:1b:
    description: text only
    readme: readmes/test/1b.md
    type: decision
    default: q4
    capabilities:
      input: [text]
      output: [choice, score, noul]
    info: {context_length: 2048, max_options: 16}
    q4:
      adapter: ggmlc-laya
      repo: owner/test
      files:
        - {file: test.gguf, size: 10, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
  testv:1b:
    description: vision
    readme: readmes/testv/1b.md
    type: decision
    default: q4
    capabilities:
      input: [text, vision]
      output: [choice, score, noul]
    info: {context_length: 2048, max_options: 16}
    q4:
      adapter: ggmlc-laya
      repo: owner/testv
      files:
        - {file: testv.gguf, size: 10, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
    q8:
      adapter: ggmlc-laya
      repo: owner/testv
      files:
        - {file: testv8.gguf, size: 20, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
`

func parseFake(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Parse([]byte(fakeRegistry))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// startFake serves the real HTTP stack on top of fakeEngine.
func startFake(t *testing.T, id string, broken bool) *env {
	t.Helper()
	res, err := parseFake(t).Resolve(id, registry.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caps := dec.CapabilitiesFromRegistry(res.Model.Capabilities)
	caps.MaxOptions = 16
	var geom *dec.ImageGeometry
	if caps.Vision {
		geom = &dec.ImageGeometry{Mode: dec.GeometryFixed, Resize: dec.ResizeContain, Width: 64, Height: 64}
	}
	svc := dec.NewService(dec.ServiceConfig{
		ModelID: id, Quant: res.Variant.Quant, Capabilities: caps, ImageInput: geom, QueueSize: 64, PreprocessConcurrency: 4,
		Info: res.Model.Info, Settings: map[string]any{"context_size": 4096},
	}, &fakeEngine{broken: broken}, imageutil.NewPreprocessor(imageutil.DefaultLimits()))
	srv := httptest.NewServer(api.NewRouter(apidecision.New(svc).Mount, healthy{}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(func() {
		svc.Close()
		srv.Close()
	})
	return newEnv(context.Background(), newClient(srv.URL), res)
}

func runAll(e *env) map[string]string {
	out := map[string]string{}
	for _, c := range checksFor(e) {
		out[c.name], _ = runCheck(context.Background(), e, c)
	}
	return out
}

func TestSuitePassesOnHealthyEngine(t *testing.T) {
	for _, id := range []string{"test:1b", "testv:1b"} {
		t.Run(id, func(t *testing.T) {
			e := startFake(t, id, false)
			for _, c := range modelChecks() {
				if status, detail := runCheck(context.Background(), e, c); status == statusFail {
					t.Errorf("%s failed: %s", c.name, detail)
				}
			}
			st := runAll(e)
			wantVision, wantTextOnly := statusSkip, statusPass
			if id == "testv:1b" {
				wantVision, wantTextOnly = statusPass, statusSkip
			}
			if st["vision: solid colors"] != wantVision || st["vision: text-only model rejects images"] != wantTextOnly {
				t.Errorf("vision checks: %v", st)
			}
		})
	}
}

func TestSuiteCatchesBrokenEngine(t *testing.T) {
	st := runAll(startFake(t, "test:1b", true))
	for _, name := range []string{"choice: option order swap", "noul: clearly true", "score: ordering", "answers: multi-question request", "limits: max options accepted"} {
		if st[name] != statusFail {
			t.Errorf("%s = %s, want fail", name, st[name])
		}
	}
	if st["api: health"] != statusPass || st["api: invalid requests rejected"] != statusPass {
		t.Errorf("server-level checks should still pass: %v", st)
	}
}

func TestMetadataMismatchIsReported(t *testing.T) {
	e := startFake(t, "test:1b", false)
	e.doc.ID = "other:1b"
	e.doc.Capabilities.Input.Vision = true
	if _, err := checkMetadata(context.Background(), e); err == nil || !strings.Contains(err.Error(), "other:1b") {
		t.Fatalf("checkMetadata = %v", err)
	}
}

func TestSelectTargets(t *testing.T) {
	reg := parseFake(t)
	ids := func(quants, models string) string {
		res, err := selectTargets(reg, models, quants)
		if err != nil {
			t.Fatalf("%s %s: %v", models, quants, err)
		}
		var out []string
		for _, r := range res {
			out = append(out, r.ID()+"@"+r.Variant.Quant)
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct{ quants, models, want string }{
		{"default", "", "test:1b@q4 testv:1b@q4"},
		{"default", "*", "test:1b@q4 testv:1b@q4"},
		{"all", "", "test:1b@q4 testv:1b@q4 testv:1b@q8"},
		{"q8", "", "testv:1b@q8"},
		{"default", "testv:*", "testv:1b@q4"},
		{"default", "test:1b, testv:1b@q8", "test:1b@q4 testv:1b@q8"},
		{"all", "testv:1b@q4,testv:1b", "testv:1b@q4 testv:1b@q8"},
	} {
		if got := ids(c.quants, c.models); got != c.want {
			t.Errorf("quants=%q models=%q: %q, want %q", c.quants, c.models, got, c.want)
		}
	}
	for _, models := range []string{"nope:*", "testv:1b@fp16"} {
		if _, err := selectTargets(reg, models, "default"); err == nil {
			t.Errorf("%q was accepted", models)
		}
	}
	if _, err := selectTargets(reg, "", "q99"); err == nil {
		t.Error("a quant no model has selected nothing without an error")
	}
}

func TestRealRegistrySelectsEveryModel(t *testing.T) {
	data, err := os.ReadFile("../../models/registry.yml")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	res, err := selectTargets(reg, "", "default")
	supportedCount := 0
	for _, model := range reg.Models {
		if model.Type == registry.TypeDecision || model.Type == registry.TypeEmbedding {
			supportedCount++
		}
	}
	if err != nil || len(res) != supportedCount {
		t.Fatalf("%d targets for %d supported models: %v", len(res), supportedCount, err)
	}
	audio, err := reg.Resolve("qwen3-tts:1.7b", registry.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if audio.Model.Type != registry.TypeAudio || audio.Variant.Adapter != "ggmlc-audio" || len(audio.Variant.Files) != 2 {
		t.Fatalf("unexpected audio model: %+v", audio)
	}
	for _, lang := range []string{"en", "de", "es", "fr", "it", "pt"} {
		p, err := reg.Resolve("pocket-tts-"+lang+":100m", registry.ResolveOptions{})
		if err != nil || p.Variant.Adapter != "ggmlc-audio" || len(p.Variant.Files) != 3 || p.Variant.Files[2].Role != registry.RoleVoice {
			t.Fatalf("pocket-tts-%s: %v %+v", lang, err, p.Variant)
		}
	}
}
