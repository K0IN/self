package selfipc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/ipc"
)

// The test binary doubles as a fake engine when SELFIPC_FAKE is set.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SELFIPC_FAKE"); mode != "" {
		os.Exit(fakeEngine(mode))
	}
	os.Exit(m.Run())
}

func fakeEngine(mode string) int {
	w := ipc.NewWriter(os.Stdout, ipc.DefaultLimits())
	r := ipc.NewReader(os.Stdin, ipc.DefaultLimits())
	if mode == "no-ready" {
		fmt.Fprintln(os.Stderr, "fatal: cannot load model")
		return 4
	}
	ready := `{"type":"ready","protocol":1,"model":"fake","device":"cpu","capabilities":{"choice":true,"score":true,"noul":true,"max_options":10,"vision":{"enabled":true,"max_images":1,"input":{"mode":"fixed","width":4,"height":2}}}}`
	w.WriteFrame(ipc.Frame{Header: []byte(ready)})
	for {
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
				Questions []struct {
					ID       string          `json:"id"`
					Type     string          `json:"type"`
					Criteria json.RawMessage `json:"criteria"`
				} `json:"questions"`
				Images []struct {
					Attachment int `json:"attachment"`
				} `json:"images"`
			} `json:"params"`
		}
		json.Unmarshal(f.Header, &req)
		switch mode {
		case "crash":
			fmt.Fprintln(os.Stderr, "segfault in kernel")
			return 139
		case "bad-id":
			req.ID++
		}
		// noul = mean pixel value of the first image / 255 (proves attachments arrive)
		noul := 0.5
		if len(req.Params.Images) > 0 {
			px := f.Attachments[req.Params.Images[0].Attachment]
			sum := 0
			for _, b := range px {
				sum += int(b)
			}
			noul = float64(sum) / float64(len(px)) / 255
		}
		answers := map[string]any{}
		for _, q := range req.Params.Questions {
			switch q.Type {
			case "noul":
				answers[q.ID] = map[string]any{"type": "noul", "noul": noul, "confidence": 0.1}
			case "choice":
				var crit map[string]any
				json.Unmarshal(q.Criteria, &crit)
				first := ""
				var raw []string
				dec := json.NewDecoder(jsonReader(q.Criteria))
				dec.Token()
				for dec.More() {
					t, _ := dec.Token()
					raw = append(raw, t.(string))
					var skip any
					dec.Decode(&skip)
				}
				first = raw[0]
				probs := map[string]float64{}
				for _, k := range raw {
					probs[k] = 1 / float64(len(raw))
				}
				answers[q.ID] = map[string]any{"type": "choice", "choice": first, "probabilities": probs, "confidence": 0}
			case "score":
				answers[q.ID] = map[string]any{"type": "score", "score": 1.0, "probabilities": map[string]float64{"0": 0.5, "1": 0.5}, "confidence": 0}
			}
		}
		if mode == "reject" {
			w.WriteFrame(ipc.Frame{Header: []byte(fmt.Sprintf(`{"id":%d,"error":{"type":"invalid_request","message":"too long"}}`, req.ID))})
			continue
		}
		res, _ := json.Marshal(map[string]any{"id": req.ID, "result": map[string]any{"answers": answers, "usage": map[string]any{"input_tokens": 7, "latency_ms": 1.5}}})
		w.WriteFrame(ipc.Frame{Header: res})
	}
}

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func jsonReader(b []byte) io.Reader { return &byteReader{b: b} }

func start(t *testing.T, mode string) (decision.Adapter, error) {
	t.Helper()
	a := Factory(Spec{
		Args: func(decision.RuntimeConfig) []string { return nil },
		Env:  []string{"SELFIPC_FAKE=" + mode},
	})()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := a.Start(ctx, decision.RuntimeConfig{ModelID: "fake:1b", EnginePath: os.Args[0], Device: "cpu"})
	if err == nil {
		t.Cleanup(func() { a.Close(context.Background()) })
	}
	return a, err
}

func questions(t *testing.T, s string) decision.Questions {
	var q decision.Questions
	if err := json.Unmarshal([]byte(s), &q); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestHandshakeAndDecide(t *testing.T) {
	a, err := start(t, "ok")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := a.Info(context.Background())
	if !info.Capabilities.Vision || info.ImageInput == nil || info.ImageInput.Width != 4 || info.Capabilities.MaxOptions != 10 || info.EngineModel != "fake" {
		t.Fatalf("info = %+v", info)
	}
	qs := questions(t, `{"z":{"type":"choice","instructions":"pick","criteria":{"zeta":"last","alpha":null}},"n":{"type":"noul","instructions":"x"},"s":{"type":"score","instructions":"x","criteria":["lo","hi"]}}`)
	img := decision.Image{Width: 4, Height: 2, Format: decision.FormatRGB8, Pixels: make([]byte, 4*2*3)}
	for i := range img.Pixels {
		img.Pixels[i] = 255
	}
	resp, err := a.Decide(context.Background(), decision.Request{State: json.RawMessage(`{"a":1}`), Questions: qs, Images: []decision.Image{img}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answers[0].ID != "z" || resp.Answers[0].Body.(*decision.ChoiceAnswer).Choice != "zeta" {
		t.Fatalf("choice order not preserved: %+v", resp.Answers[0].Body)
	}
	if n := resp.Answers[1].Body.(*decision.NoulAnswer).Noul; n != 1 {
		t.Fatalf("attachment not delivered (noul=%v)", n)
	}
	if resp.Usage.InputTokens != 7 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	// second request on the same engine
	if _, err := a.Decide(context.Background(), decision.Request{State: json.RawMessage(`"x"`), Questions: qs}); err != nil {
		t.Fatal(err)
	}
}

func TestEngineRejects(t *testing.T) {
	a, err := start(t, "reject")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Decide(context.Background(), decision.Request{State: json.RawMessage(`"x"`), Questions: questions(t, `{"n":{"type":"noul","instructions":"x"}}`)})
	if errs.KindOf(err) != errs.InvalidRequest {
		t.Fatalf("got %v", err)
	}
	select {
	case <-a.Done():
		t.Fatal("engine should still be running")
	default:
	}
}

func TestStartFailureShowsStderr(t *testing.T) {
	_, err := start(t, "no-ready")
	if errs.KindOf(err) != errs.RuntimeStartFailed {
		t.Fatalf("got %v", err)
	}
	if want := "cannot load model"; !contains(err.Error(), want) {
		t.Fatalf("stderr not surfaced: %v", err)
	}
}

func TestCrashAndProtocolErrors(t *testing.T) {
	for _, mode := range []string{"crash", "bad-id"} {
		a, err := start(t, mode)
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.Decide(context.Background(), decision.Request{State: json.RawMessage(`"x"`), Questions: questions(t, `{"n":{"type":"noul","instructions":"x"}}`)})
		if errs.KindOf(err) != errs.RuntimeCrashed {
			t.Fatalf("%s: got %v", mode, err)
		}
		select {
		case <-a.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: engine not stopped", mode)
		}
	}
}

func TestCapabilityChecksBeforeSending(t *testing.T) {
	a, err := start(t, "ok")
	if err != nil {
		t.Fatal(err)
	}
	opts := map[string]string{}
	for i := 0; i < 11; i++ {
		opts[fmt.Sprintf("o%d", i)] = ""
	}
	b, _ := json.Marshal(map[string]any{"q": map[string]any{"type": "choice", "instructions": "x", "criteria": opts}})
	_, err = a.Decide(context.Background(), decision.Request{State: json.RawMessage(`"x"`), Questions: questions(t, string(b))})
	if errs.KindOf(err) != errs.UnsupportedCapability {
		t.Fatalf("got %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
