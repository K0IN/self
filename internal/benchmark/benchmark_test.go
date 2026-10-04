package benchmark

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-server/internal/decision"
)

func sampleReport() Report {
	return Report{
		Schema:    SchemaVersion,
		CreatedAt: time.Date(2026, 9, 30, 15, 30, 12, 0, time.UTC),
		Benchmark: Info{Version: Version, Modality: "decision", Iterations: 20, Warmup: 2, SelfCommit: "14ba251ca378"},
		Model: Model{
			ID: "kev:0.5b", Quant: "q4", Adapter: "ggmlc-laya",
			Files:    []File{{Name: "kev_0.5b_ud_q4_k_m.gguf", Role: "model", Size: 570955168, SHA256: strings.Repeat("ab", 32)}},
			Settings: map[string]any{"threads": float64(4)},
		},
		Hardware: Hardware{
			OS: "linux/amd64", CPU: "AMD Ryzen 7 5800X3D 8-Core Processor", CPUThreads: 16, RAMGiB: 31.3,
			GPUs: []GPU{{Name: "NVIDIA GeForce RTX 5090", VRAMMiB: 32607, Driver: "615.71.09", ComputeCapability: "12.0"}},
		},
		Device:      Device{Requested: "auto", Engine: "auto", GPUMemoryMiB: 700, GPUUsed: true},
		LoadSeconds: 0.7,
		Checks:      Checks{ProbesPassed: 5, ProbesTotal: 5},
		Results: []Result{{
			Scenario: ShortChoice, Requests: 20, InputTokens: 45,
			Throughput: 11538.5, ThroughputUnit: "input_tokens_per_sec",
			Latency:        Stats{Min: 3.1, Mean: 3.9, P50: 3.8, P95: 4.6, Max: 5},
			Engine:         &Stats{Min: 3, Mean: 3.8, P50: 3.7, P95: 4.5, Max: 4.9},
			RequestsPerSec: 256.4, InputTokensPerSec: 11538.5,
		}},
	}
}

// --- statistics ---

func TestSummarize(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(100 - i) // unsorted input
	}
	s := summarize(values)
	if s.Min != 1 || s.Max != 100 || s.P50 != 50 || s.P95 != 95 || s.Mean != 50.5 {
		t.Fatalf("%+v", s)
	}
	if one := summarize([]float64{7.256}); one.P50 != 7.26 || one.P95 != 7.26 || one.Min != 7.26 {
		t.Fatalf("single value: %+v", one)
	}
}

func TestRates(t *testing.T) {
	// 5 requests of 1684 tokens in 65 s. Rounding requests per second to one
	// decimal first would report 0.1 req/s and 842 tokens/s instead of 129.5.
	req, in, out := rates(5, 5*1684, 0, 65*time.Second)
	if req != 0.08 || in != 129.5 || out != 0 {
		t.Fatalf("req/s=%v in tok/s=%v out tok/s=%v", req, in, out)
	}
	if r, i, o := rates(5, 10, 10, 0); r != 0 || i != 0 || o != 0 {
		t.Fatalf("zero time must not divide by zero: %v %v %v", r, i, o)
	}
}

// --- running ---

type fakeDecider struct {
	calls  int
	failAt int // fail this call (1-based), 0 = never
	delay  time.Duration
}

func (f *fakeDecider) Decide(context.Context, decision.Request) (decision.Response, error) {
	f.calls++
	if f.failAt > 0 && f.calls == f.failAt {
		return decision.Response{}, errors.New("engine crashed")
	}
	time.Sleep(f.delay)
	return decision.Response{Usage: decision.Usage{InputTokens: 100, LatencyMS: 2.5}}, nil
}

func TestRunCountsOnlyTimedRequests(t *testing.T) {
	d := &fakeDecider{delay: 2 * time.Millisecond}
	var seen []string
	res, err := Run(context.Background(), d, []Scenario{{Name: "a", Requests: 4}, {Name: "b", Requests: 3}}, 2, func(r Result) { seen = append(seen, r.Scenario) })
	if err != nil {
		t.Fatal(err)
	}
	if d.calls != (2+4)+(2+3) {
		t.Fatalf("%d calls, want warmup + timed for both scenarios", d.calls)
	}
	if !reflect.DeepEqual(seen, []string{"a", "b"}) || len(res) != 2 {
		t.Fatalf("progress %v results %d", seen, len(res))
	}
	a := res[0]
	if a.Requests != 4 || a.InputTokens != 100 || a.Engine == nil || a.Engine.P50 != 2.5 {
		t.Fatalf("%+v", a)
	}
	if a.Latency.Min < 2 || a.Latency.Min > a.Latency.P50 || a.RequestsPerSec <= 0 || a.RequestsPerSec > 500 || a.InputTokensPerSec <= 0 {
		t.Fatalf("timings do not follow the 2 ms delay: %+v", a)
	}
	if a.OutputTokens != 0 || a.OutputTokensPerSec != 0 {
		t.Fatalf("engines that report no output tokens must not invent them: %+v", a)
	}
}

func TestRunStopsOnFirstError(t *testing.T) {
	d := &fakeDecider{failAt: 3}
	res, err := Run(context.Background(), d, []Scenario{{Name: "a", Requests: 5}, {Name: "b", Requests: 5}}, 0, nil)
	if err == nil || !strings.Contains(err.Error(), "scenario a") || !strings.Contains(err.Error(), "engine crashed") {
		t.Fatalf("err = %v", err)
	}
	if len(res) != 0 || d.calls != 3 {
		t.Fatalf("results %d, calls %d: a failing model must not produce timings", len(res), d.calls)
	}
}

// --- scenarios ---

func TestScenariosByCapability(t *testing.T) {
	text := decision.Capabilities{Text: true, Choice: true, Score: true, Noul: true, MaxOptions: 16}
	vision := text
	vision.Vision, vision.MaxImages = true, 1
	choiceOnly := decision.Capabilities{Text: true, Choice: true}
	img := &decision.Image{Width: 8, Height: 8, Format: decision.FormatRGB8, Pixels: make([]byte, 8*8*3)}

	names := func(in Inputs) string {
		ss, err := Scenarios(in)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range ss {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		name string
		in   Inputs
		want string
	}{
		{"text model", Inputs{Caps: text, ContextTokens: 256, Iterations: 20}, "short-choice,multi-question,max-options,long-context"},
		{"vision model", Inputs{Caps: vision, ContextTokens: 256, Iterations: 20, Image: img}, "short-choice,multi-question,max-options,long-context,vision"},
		{"vision model without a prepared image", Inputs{Caps: vision, ContextTokens: 256, Iterations: 20}, "short-choice,multi-question,max-options,long-context"},
		{"choice only, unknown option limit", Inputs{Caps: choiceOnly, ContextTokens: 256, Iterations: 20}, "short-choice,long-context"},
	} {
		if got := names(c.in); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if _, err := Scenarios(Inputs{Caps: decision.Capabilities{Text: true, Noul: true}, Iterations: 20}); err == nil {
		t.Error("a model without choice questions has no scenario")
	}
}

func TestScenarioRequestsAreServable(t *testing.T) {
	caps := decision.Capabilities{Text: true, Choice: true, Score: true, Noul: true, MaxOptions: 10}
	ss, err := Scenarios(Inputs{Caps: caps, ContextTokens: 256, Iterations: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if err := caps.CheckQuestions(s.Request.Questions); err != nil {
			t.Errorf("%s: the model would reject it: %v", s.Name, err)
		}
		if !json.Valid(s.Request.State) {
			t.Errorf("%s: state is not JSON: %s", s.Name, s.Request.State)
		}
		if s.Requests < 3 {
			t.Errorf("%s: %d timed requests", s.Name, s.Requests)
		}
	}
	wide := ss[2].Request.Questions[0].Body.(*decision.ChoiceQuestion).Criteria
	if len(wide) != caps.MaxOptions || wide[len(wide)-1].Key != billingKey {
		t.Errorf("max-options must use exactly the limit with the answer last: %d options, last %q", len(wide), wide[len(wide)-1].Key)
	}
	var long string
	_ = json.Unmarshal(ss[3].Request.State, &long)
	if len(long) < 256*4 || !strings.HasSuffix(long, ticketText) {
		t.Errorf("long-context text is %d chars and must end with the complaint", len(long))
	}
}

func TestLongContextTokens(t *testing.T) {
	for _, c := range []struct {
		name   string
		length int
		size   int64
		want   int
	}{
		{"small encoder", 512, 0, 256},
		{"huge context capped", 262144, 8192, 2048},
		{"setting below the model context", 2048, 1024, 512},
		{"setting above the model context", 2048, 8192, 1024},
		{"unknown", 0, 0, 512},
		{"tiny", 64, 0, 64},
	} {
		if got := LongContextTokens(c.length, c.size); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestImageDataURI(t *testing.T) {
	uri := ImageDataURI()
	data, ok := strings.CutPrefix(uri, "data:image/png;base64,")
	if !ok {
		t.Fatal(uri[:30])
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 448 || cfg.Height != 448 {
		t.Fatalf("%v %+v", err, cfg)
	}
}

// --- report ---

func TestValidateAcceptsSample(t *testing.T) {
	if err := Validate(sampleReport()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	for name, mutate := range map[string]func(*Report){
		"wrong schema":              func(r *Report) { r.Schema = 99 },
		"no time":                   func(r *Report) { r.CreatedAt = time.Time{} },
		"future benchmark version":  func(r *Report) { r.Benchmark.Version = Version + 1 },
		"missing modality":          func(r *Report) { r.Benchmark.Modality = "" },
		"zero iterations":           func(r *Report) { r.Benchmark.Iterations = 0 },
		"no model":                  func(r *Report) { r.Model.ID = "" },
		"no files":                  func(r *Report) { r.Model.Files = nil },
		"short hash":                func(r *Report) { r.Model.Files[0].SHA256 = "abc" },
		"upper case hash":           func(r *Report) { r.Model.Files[0].SHA256 = strings.Repeat("AB", 32) },
		"no cpu":                    func(r *Report) { r.Hardware.CPU = "" },
		"no device":                 func(r *Report) { r.Device.Requested = "" },
		"gpu used on a cpu run":     func(r *Report) { r.Device.Requested = "cpu" },
		"no load time":              func(r *Report) { r.LoadSeconds = 0 },
		"more probes passed":        func(r *Report) { r.Checks.ProbesPassed = 6 },
		"no results":                func(r *Report) { r.Results = nil },
		"duplicate scenario":        func(r *Report) { r.Results = append(r.Results, r.Results[0]) },
		"no requests":               func(r *Report) { r.Results[0].Requests = 0 },
		"p50 above p95":             func(r *Report) { r.Results[0].Latency.P50 = 9 },
		"mean above max":            func(r *Report) { r.Results[0].Latency.Mean = 9 },
		"zero latency":              func(r *Report) { r.Results[0].Latency.Min = 0 },
		"inconsistent engine stats": func(r *Report) { r.Results[0].Engine.Min = 9 },
		"zero throughput":           func(r *Report) { r.Results[0].RequestsPerSec = 0 },
	} {
		r := sampleReport()
		mutate(&r)
		if err := Validate(r); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	want := sampleReport()
	for name, encode := range map[string]func(Report) ([]byte, error){"indented": Encode, "compact": EncodeCompact} {
		data, err := encode(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: report changed on the way through JSON:\n%+v\n%+v", name, got, want)
		}
	}
	for name, bad := range map[string]string{
		"unknown field": `{"schema":1,"surprise":true}`,
		"trailing data": `{"schema":1} {"schema":1}`,
		"not json":      `nope`,
	} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestWriteAndLoad(t *testing.T) {
	r := sampleReport()
	dir := filepath.Join(t.TempDir(), "nested", "dir") + string(filepath.Separator)
	path := OutputPath(dir, r)
	if err := Write(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("%v\n%+v", err, got)
	}
}

func TestFilenameAndOutputPath(t *testing.T) {
	r := sampleReport()
	if got, want := Filename(r), "kev-0.5b-q4-rtx-5090-20260930-153012.json"; got != want {
		t.Errorf("Filename = %q, want %q", got, want)
	}
	r.Device.GPUUsed = false
	if got := Filename(r); !strings.Contains(got, "-cpu-ryzen-7-5800x3d-") {
		t.Errorf("a run that did not use the GPU must not be filed under it: %q", got)
	}
	r.Model.ID = "decider-vision:2b"
	if got := Filename(r); !strings.HasPrefix(got, "decider-vision-2b-q4-") {
		t.Errorf("Filename = %q", got)
	}

	dir := t.TempDir()
	for _, c := range []struct{ out, want string }{
		{"", Filename(r)},
		{dir, filepath.Join(dir, Filename(r))},
		{"reports/", filepath.Join("reports", Filename(r))},
		{filepath.Join(dir, "mine.json"), filepath.Join(dir, "mine.json")},
	} {
		if got := OutputPath(c.out, r); got != c.want {
			t.Errorf("OutputPath(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

// --- sharing ---

func TestNewFileURL(t *testing.T) {
	r := sampleReport()
	link, ok := NewFileURL(r)
	if !ok || len(link) > maxURL {
		t.Fatalf("ok=%v len=%d", ok, len(link))
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != RepositoryURL+"/new/"+Branch {
		t.Errorf("link goes to %s", got)
	}
	q := u.Query()
	if got, want := q.Get("filename"), "benchmarks/"+Filename(r); got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
	want, _ := Encode(r)
	if q.Get("value") != string(want) {
		t.Errorf("value does not decode to the report file")
	}
	back, err := Decode([]byte(q.Get("value")))
	if err != nil || !reflect.DeepEqual(back, r) {
		t.Errorf("the prefilled content is not the report: %v", err)
	}
}

func TestNewFileURLFallsBackToCompactThenGivesUp(t *testing.T) {
	r := sampleReport()
	var usedCompact bool
	for n := 1; n < 2000 && !usedCompact; n++ {
		r.Model.Settings = map[string]any{}
		for i := 0; i < n; i++ {
			r.Model.Settings["setting_"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26%26))] = float64(i)
		}
		link, ok := NewFileURL(r)
		if !ok {
			t.Fatal("gave up before trying the compact form")
		}
		u, _ := url.Parse(link)
		if pretty, _ := Encode(r); u.Query().Get("value") != string(pretty) {
			usedCompact = true
			if compact, _ := EncodeCompact(r); u.Query().Get("value") != string(compact) || len(link) > maxURL {
				t.Fatal("the fallback is neither the compact report nor short enough")
			}
		}
	}
	if !usedCompact {
		t.Fatal("never needed the compact form")
	}
	for i := 0; i < 5000; i++ {
		r.Model.Settings["big_"+strings.Repeat("y", 40)+string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('a'+i/676%26))] = float64(i)
	}
	if link, ok := NewFileURL(r); ok {
		t.Fatalf("a huge report got a %d character link", len(link))
	}
	if got := UploadPageURL(); got != RepositoryURL+"/upload/"+Branch+"/"+Dir {
		t.Errorf("UploadPageURL = %s", got)
	}
}

// --- hardware ---

func TestGPUUsed(t *testing.T) {
	nvidia := Hardware{GPUs: []GPU{{Name: "NVIDIA GeForce RTX 5090", VRAMMiB: 32607, Driver: "615.71"}}}
	named := Hardware{GPUs: []GPU{{Name: "AMD Radeon RX 7900 XTX"}}}
	const modelBytes = 4 << 30 // 4096 MiB
	for _, c := range []struct {
		name   string
		device string
		hw     Hardware
		mib    int
		want   bool
	}{
		{"auto remains conservative", "auto", nvidia, 4300, false},
		{"explicit gpu in memory", "cuda", nvidia, 4300, true},
		{"explicit gpu quarter of weights", "cuda", nvidia, 1024, true},
		{"silent CPU fallback", "auto", nvidia, 14, false},
		{"explicit cpu", "cpu", nvidia, 4300, false},
		{"no gpu at all", "auto", Hardware{}, 0, false},
		{"user named a gpu", "vulkan", named, 0, true},
		{"user named a gpu but asked for cpu", "cpu", named, 0, false},
	} {
		if got := GPUUsed(c.device, c.hw, c.mib, modelBytes); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSlugAndRigID(t *testing.T) {
	for in, want := range map[string]string{
		"NVIDIA GeForce RTX 5090":                      "rtx-5090",
		"NVIDIA RTX 6000 Ada Generation":               "rtx-6000-ada",
		"NVIDIA A100-SXM4-80GB":                        "a100-sxm4-80gb",
		"AMD Ryzen 7 5800X3D 8-Core Processor":         "ryzen-7-5800x3d",
		"Intel(R) Core(TM) i9-14900K CPU @ 3.20GHz":    "core-i9-14900k",
		"Apple M3 Max":                                 "m3-max",
		"AMD Ryzen 9 7940HS with Radeon 780M Graphics": "ryzen-9-7940hs",
		"  weird // name !! ":                          "weird-name",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	gpu := GPU{Name: "NVIDIA GeForce RTX 5090"}
	hw := Hardware{CPU: "AMD Ryzen 7 5800X3D 8-Core Processor", GPUs: []GPU{gpu}}
	for _, c := range []struct {
		hw      Hardware
		gpuUsed bool
		want    string
	}{
		{hw, true, "rtx-5090"},
		{hw, false, "cpu-ryzen-7-5800x3d"},
		{Hardware{CPU: hw.CPU, GPUs: []GPU{gpu, gpu}}, true, "rtx-5090-x2"},
		{Hardware{}, true, "unknown-rig"},
	} {
		if got := RigID(c.hw, c.gpuUsed); got != c.want {
			t.Errorf("RigID(%+v, %v) = %q, want %q", c.hw, c.gpuUsed, got, c.want)
		}
	}
}

func TestParseNvidiaSMI(t *testing.T) {
	got := parseNvidiaSMI("NVIDIA GeForce RTX 5090, 32607, 615.71.09, 12.0\nNVIDIA GeForce RTX 4090, 24564, 560.35\n\n")
	want := []GPU{
		{Name: "NVIDIA GeForce RTX 5090", VRAMMiB: 32607, Driver: "615.71.09", ComputeCapability: "12.0"},
		{Name: "NVIDIA GeForce RTX 4090", VRAMMiB: 24564, Driver: "560.35"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

// --- submitted reports ---

// Every report in benchmarks/ must be valid and named after its contents, so a
// hand-edited or renamed submission fails in CI instead of polluting the data.
func TestCheckedInReports(t *testing.T) {
	const dir = "../../benchmarks"
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no benchmarks directory")
	}
	for _, p := range paths {
		r, err := Load(p)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		if err := Validate(r); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		if got := filepath.Base(p); got != Filename(r) {
			t.Errorf("%s is named %q but its contents say %q", p, got, Filename(r))
		}
	}
}
