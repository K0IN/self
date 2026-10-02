package decision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-server/internal/api"
	dom "ai-server/internal/decision"
	"ai-server/internal/imageutil"
)

// fakeAdapter is a scripted decision.Adapter.
type fakeAdapter struct {
	mu      sync.Mutex
	last    dom.Request
	block   chan struct{} // if non-nil, Decide waits on it
	started chan struct{}
	done    chan struct{}
}

func newFake() *fakeAdapter { return &fakeAdapter{done: make(chan struct{})} }

func (f *fakeAdapter) Start(context.Context, dom.RuntimeConfig) error { return nil }
func (f *fakeAdapter) Close(context.Context) error                    { return nil }
func (f *fakeAdapter) Done() <-chan struct{}                          { return f.done }
func (f *fakeAdapter) Info(context.Context) (dom.RuntimeInfo, error)  { return dom.RuntimeInfo{}, nil }

func (f *fakeAdapter) Decide(_ context.Context, req dom.Request) (dom.Response, error) {
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	f.last = req
	f.mu.Unlock()
	var as dom.Answers
	for _, q := range req.Questions {
		switch b := q.Body.(type) {
		case *dom.ChoiceQuestion:
			var ps dom.Probabilities
			for _, o := range b.Criteria {
				ps = append(ps, dom.Probability{Key: o.Key, P: 1 / float64(len(b.Criteria))})
			}
			as = append(as, dom.Answer{ID: q.ID, Body: &dom.ChoiceAnswer{Choice: b.Criteria[0].Key, Probabilities: ps, Confidence: 0.5}})
		case *dom.ScoreQuestion:
			as = append(as, dom.Answer{ID: q.ID, Body: &dom.ScoreAnswer{Score: 1, Probabilities: dom.Probabilities{{Key: "0", P: 0.5}, {Key: "1", P: 0.5}}}})
		case *dom.NoulQuestion:
			as = append(as, dom.Answer{ID: q.ID, Body: &dom.NoulAnswer{Noul: 0.9, Confidence: 0.8}})
		}
	}
	return dom.Response{Answers: as, Usage: dom.Usage{InputTokens: 10, LatencyMS: 1.5}}, nil
}

func (f *fakeAdapter) lastReq() dom.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

type healthy struct{}

func (healthy) RunnerState() string { return "ready" }

func setup(t *testing.T, caps dom.Capabilities, geom *dom.ImageGeometry, queue int, f *fakeAdapter) *httptest.Server {
	srv, _ := setupSvc(t, caps, geom, queue, f)
	return srv
}

func setupSvc(t *testing.T, caps dom.Capabilities, geom *dom.ImageGeometry, queue int, f *fakeAdapter) (*httptest.Server, *dom.Service) {
	t.Helper()
	lim := imageutil.DefaultLimits()
	lim.AllowHTTP, lim.AllowPrivate = true, true
	svc := dom.NewService(dom.ServiceConfig{
		ModelID: "test:1b", Quant: "q4", Capabilities: caps, ImageInput: geom, QueueSize: queue, PreprocessConcurrency: 4,
	}, f, imageutil.NewPreprocessor(lim))
	srv := httptest.NewServer(api.NewRouter(New(svc).Mount, healthy{}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(func() {
		svc.Close()
		srv.Close()
	})
	return srv, svc
}

var textCaps = dom.Capabilities{Text: true, Choice: true, Score: true, Noul: true, MaxOptions: 16}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+"/v1/systemone", "application/json", strings.NewReader(body))
	if err != nil {
		t.Error(err)
		return 0, nil
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func errType(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["type"].(string)
	return s
}

func TestInfoRoutes(t *testing.T) {
	srv := setup(t, textCaps, nil, 4, newFake())
	for path, want := range map[string]string{
		"/health":    `"status":"ok"`,
		"/v1/model":  `"id":"test:1b","object":"model","type":"decision","quant":"q4","capabilities":{"input":{"text":true,"vision":false`,
		"/v1/models": `"data":[{"id":"test:1b"`,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), want) {
			t.Errorf("%s: %d %s", path, resp.StatusCode, b)
		}
	}
}

func TestSystemOneIgnoresModel(t *testing.T) {
	srv := setup(t, textCaps, nil, 4, newFake())
	for _, model := range []string{"other", "test:1b", ""} {
		status, m := post(t, srv.URL, `{"state":"x","model":"`+model+`","questions":{"q":{"type":"noul","instructions":"x"}}}`)
		if status != 200 || m["model"] != "test:1b" {
			t.Errorf("model %q: got %d %v", model, status, m)
		}
	}
}

func TestSystemOneText(t *testing.T) {
	f := newFake()
	srv := setup(t, textCaps, nil, 4, f)
	resp, err := http.Post(srv.URL+"/v1/systemone", "application/json", strings.NewReader(`{
		"state": {"message": "I was charged twice."},
		"questions": {
			"department": {"type":"choice","instructions":"Which?","criteria":{"billing":"Payments","technical":"Tech"}},
			"refund": {"type":"noul","instructions":"Refund?"}
		}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	want := `{"model":"test:1b","answers":{"department":{"type":"choice","choice":"billing","probabilities":{"billing":0.5,"technical":0.5},"confidence":0.5},"refund":{"type":"noul","noul":0.9,"confidence":0.8}},"usage":{"input_tokens":10,"output_tokens":0,"latency_ms":1.5`
	if resp.StatusCode != 200 || !strings.HasPrefix(string(b), want) {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if string(f.lastReq().State) != `{"message": "I was charged twice."}` {
		t.Fatalf("state not passed through: %s", f.lastReq().State)
	}
}

func TestSystemOneErrors(t *testing.T) {
	srv := setup(t, dom.Capabilities{Text: true, Choice: true, Noul: true, MaxOptions: 2}, nil, 4, newFake())
	cases := []struct {
		body   string
		status int
		typ    string
	}{
		{`{`, 400, "invalid_request"},
		{`{"questions":{"q":{"type":"noul","instructions":"x"}}}`, 400, "invalid_request"},
		{`{"state":"x","questions":{}}`, 400, "invalid_request"},
		{`{"state":"x","questions":{"q":{"type":"essay","instructions":"x"}}}`, 400, "invalid_request"},
		{`{"state":"x","bogus":1,"questions":{"q":{"type":"noul","instructions":"x"}}}`, 400, "invalid_request"},
		{`{"state":"x","questions":{"q":{"type":"score","instructions":"x","criteria":["a","b"]}}}`, 422, "unsupported_capability"},
		{`{"state":"x","questions":{"q":{"type":"choice","instructions":"x","criteria":["a","b","c"]}}}`, 422, "unsupported_capability"},
		{`{"state":"x","images":["https://example.com/a.png"],"questions":{"q":{"type":"noul","instructions":"x"}}}`, 422, "unsupported_capability"},
	}
	for _, c := range cases {
		status, m := post(t, srv.URL, c.body)
		if status != c.status || errType(m) != c.typ {
			t.Errorf("%s: got %d %v", c.body, status, m)
		}
	}
	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("unknown route: %d", resp.StatusCode)
	}
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.Black)
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestSystemOneVision(t *testing.T) {
	f := newFake()
	caps := textCaps
	caps.Vision, caps.MaxImages = true, 1
	geom := &dom.ImageGeometry{Mode: dom.GeometryFixed, Width: 32, Height: 32}
	srv := setup(t, caps, geom, 4, f)

	frame := pngBytes(64, 40)
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(frame)
	}))
	defer imgSrv.Close()

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes(10, 10))
	for _, img := range []string{`"` + imgSrv.URL + `/frame.png"`, `{"url":"` + dataURI + `","name":"front","description":"cam"}`} {
		status, m := post(t, srv.URL, `{"state":"Choose.","images":[`+img+`],"questions":{"action":{"type":"choice","instructions":"Next?","criteria":{"go":"Continue","stop":"Stop"}}}}`)
		if status != 200 {
			t.Fatalf("status %d %v", status, m)
		}
		usage := m["usage"].(map[string]any)
		if usage["images"].(float64) != 1 {
			t.Fatalf("usage = %v", usage)
		}
		last := f.lastReq()
		if len(last.Images) != 1 || last.Images[0].Width != 32 || len(last.Images[0].Pixels) != 32*32*3 {
			t.Fatalf("image not resized to engine geometry: %d images", len(last.Images))
		}
	}
	if im := f.lastReq().Images[0]; im.Name != "front" || im.Description != "cam" {
		t.Fatalf("metadata lost: %q %q", im.Name, im.Description)
	}
	status, m := post(t, srv.URL, `{"state":"x","images":["a","b"],"questions":{"q":{"type":"noul","instructions":"x"}}}`)
	if status != 422 || errType(m) != "unsupported_capability" {
		t.Fatalf("image count: %d %v", status, m)
	}
	status, m = post(t, srv.URL, `{"state":"x","images":["data:image/png;base64,aGVsbG8="],"questions":{"q":{"type":"noul","instructions":"x"}}}`)
	if status != 400 || errType(m) != "unsupported_image" {
		t.Fatalf("bad image: %d %v", status, m)
	}
}

func TestQueueFull429(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	f.started = make(chan struct{}, 10)
	srv, svc := setupSvc(t, textCaps, nil, 1, f)
	body := `{"state":"x","questions":{"q":{"type":"noul","instructions":"x"}}}`
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); post(t, srv.URL, body) }() // running
	<-f.started
	go func() { defer wg.Done(); post(t, srv.URL, body) }() // queued
	deadline := time.Now().Add(5 * time.Second)
	for svc.QueueLen() < 1 {
		if time.Now().After(deadline) {
			close(f.block)
			t.Fatal("second request never queued")
		}
		time.Sleep(time.Millisecond)
	}
	status, m := post(t, srv.URL, body)
	close(f.block)
	wg.Wait()
	if status != 429 || errType(m) != "queue_full" {
		t.Fatalf("got %d %v", status, m)
	}
}
