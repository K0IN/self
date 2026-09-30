package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Report is the result of one `self benchmark` run.
type Report struct {
	Schema      int       `json:"schema"`
	CreatedAt   time.Time `json:"created_at"`
	Benchmark   Info      `json:"benchmark"`
	Model       Model     `json:"model"`
	Hardware    Hardware  `json:"hardware"`
	Device      Device    `json:"device"`
	LoadSeconds float64   `json:"load_seconds"`
	Checks      Checks    `json:"checks"`
	Results     []Result  `json:"results"`
}

// Info says how the numbers were produced.
type Info struct {
	Version      int    `json:"version"` // Version of the scenarios
	Iterations   int    `json:"iterations"`
	Warmup       int    `json:"warmup"`
	SelfCommit   string `json:"self_commit,omitempty"`
	SelfModified bool   `json:"self_modified,omitempty"`
}

// Model identifies exactly what was measured.
type Model struct {
	ID       string         `json:"id"`
	Quant    string         `json:"quant"`
	Adapter  string         `json:"adapter"`
	Files    []File         `json:"files"`
	Settings map[string]any `json:"settings,omitempty"`
}

// File is one model file; SHA256 was computed from the file that was run.
type File struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Hardware describes the machine. It holds no host name, user or path.
type Hardware struct {
	OS         string  `json:"os"`
	CPU        string  `json:"cpu"`
	CPUThreads int     `json:"cpu_threads"`
	RAMGiB     float64 `json:"ram_gib,omitempty"`
	GPUs       []GPU   `json:"gpus,omitempty"`
}

// GPU is one graphics card.
type GPU struct {
	Name              string `json:"name"`
	VRAMMiB           int    `json:"vram_mib,omitempty"`
	Driver            string `json:"driver,omitempty"`
	ComputeCapability string `json:"compute_capability,omitempty"`
}

// Device tells where the model ran. GPUMemoryMiB is the NVIDIA memory the
// load added: about the model size on a GPU, about 0 when it runs on the CPU.
// GPUUsed is the verdict, see GPUUsed.
type Device struct {
	Requested    string `json:"requested"`
	Engine       string `json:"engine,omitempty"`
	GPUMemoryMiB int    `json:"gpu_memory_mib,omitempty"`
	GPUUsed      bool   `json:"gpu_used"`
}

// Checks records whether the model answered the `self check` probes
// correctly while it was measured.
type Checks struct {
	ProbesPassed int `json:"probes_passed"`
	ProbesTotal  int `json:"probes_total"`
}

// Result is the timing of one scenario.
type Result struct {
	Scenario           string  `json:"scenario"`
	Requests           int     `json:"requests"`
	InputTokens        int     `json:"input_tokens"` // per request, as reported by the engine
	OutputTokens       int     `json:"output_tokens,omitempty"`
	Latency            Stats   `json:"latency_ms"`
	Engine             *Stats  `json:"engine_latency_ms,omitempty"`
	RequestsPerSec     float64 `json:"requests_per_sec"`
	InputTokensPerSec  float64 `json:"input_tokens_per_sec"`
	OutputTokensPerSec float64 `json:"output_tokens_per_sec,omitempty"`
}

// Stats summarizes latencies in milliseconds.
type Stats struct {
	Min  float64 `json:"min"`
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Max  float64 `json:"max"`
}

// Encode renders the report as indented JSON.
func Encode(r Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// EncodeCompact renders the report without whitespace (for URLs).
func EncodeCompact(r Report) ([]byte, error) { return json.Marshal(r) }

// Decode reads a report, rejecting unknown fields and trailing data.
func Decode(data []byte) (Report, error) {
	var r Report
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return Report{}, err
	}
	if d.More() {
		return Report{}, errors.New("unexpected data after the report")
	}
	return r, nil
}

// Load reads and decodes a report file.
func Load(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	r, err := Decode(data)
	if err != nil {
		return Report{}, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// ModelSlug turns a model id into a file name part: "decider-vision:2b" ->
// "decider-vision-2b".
func ModelSlug(id string) string {
	return strings.ToLower(strings.NewReplacer(":", "-", "/", "-", " ", "-").Replace(id))
}

// Filename is the file name a report is stored and shared under:
// <model>-<quant>-<rig>-<UTC time>.json. The rig is the GPU only when the
// model ran on it, so a CPU run on a machine with a GPU is not filed under it.
func Filename(r Report) string {
	return fmt.Sprintf("%s-%s-%s-%s.json", ModelSlug(r.Model.ID), Slug(r.Model.Quant),
		RigID(r.Hardware, r.Device.GPUUsed), r.CreatedAt.UTC().Format("20060102-150405"))
}

// OutputPath resolves --out: an existing directory or a path ending in a
// separator gets the generated file name, anything else is the file itself.
func OutputPath(out string, r Report) string {
	if out == "" {
		return Filename(r)
	}
	if st, err := os.Stat(out); (err == nil && st.IsDir()) || strings.HasSuffix(out, string(filepath.Separator)) || strings.HasSuffix(out, "/") {
		return filepath.Join(out, Filename(r))
	}
	return out
}

// Write saves the report to path, creating the directory if needed.
func Write(path string, r Report) error {
	data, err := Encode(r)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks that a report is complete and internally consistent. It is
// what CI runs on every submitted report.
func Validate(r Report) error {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if r.Schema != SchemaVersion {
		bad("schema is %d, want %d", r.Schema, SchemaVersion)
	}
	if r.CreatedAt.IsZero() {
		bad("created_at is missing")
	}
	if r.Benchmark.Version < 1 || r.Benchmark.Version > Version {
		bad("benchmark.version is %d, this build knows 1..%d", r.Benchmark.Version, Version)
	}
	if r.Benchmark.Iterations < 1 {
		bad("benchmark.iterations must be at least 1")
	}
	if r.Model.ID == "" || r.Model.Quant == "" || r.Model.Adapter == "" {
		bad("model id, quant and adapter are required")
	}
	if len(r.Model.Files) == 0 {
		bad("model.files is empty")
	}
	for _, f := range r.Model.Files {
		if f.Name == "" || f.Size <= 0 || !sha256RE.MatchString(f.SHA256) {
			bad("model file %q needs a name, a size and a lowercase hex sha256", f.Name)
		}
	}
	if r.Hardware.OS == "" || r.Hardware.CPU == "" || r.Hardware.CPUThreads < 1 {
		bad("hardware needs os, cpu and cpu_threads")
	}
	if r.Device.Requested == "" {
		bad("device.requested is missing")
	}
	if r.Device.Requested == "cpu" && r.Device.GPUUsed {
		bad("device.gpu_used is true although the run asked for the CPU")
	}
	if r.LoadSeconds <= 0 {
		bad("load_seconds must be positive")
	}
	if r.Checks.ProbesTotal < 1 || r.Checks.ProbesPassed < 0 || r.Checks.ProbesPassed > r.Checks.ProbesTotal {
		bad("checks: %d of %d probes", r.Checks.ProbesPassed, r.Checks.ProbesTotal)
	}
	if len(r.Results) == 0 {
		bad("results is empty")
	}
	seen := map[string]bool{}
	for _, res := range r.Results {
		switch {
		case res.Scenario == "":
			bad("a result has no scenario name")
		case seen[res.Scenario]:
			bad("scenario %q appears twice", res.Scenario)
		}
		seen[res.Scenario] = true
		if res.Requests < 1 || res.InputTokens < 0 || res.RequestsPerSec <= 0 || res.InputTokensPerSec < 0 {
			bad("scenario %q has impossible counts or rates", res.Scenario)
		}
		if err := res.Latency.check(); err != nil {
			bad("scenario %q latency: %v", res.Scenario, err)
		}
		if res.Engine != nil {
			if err := res.Engine.check(); err != nil {
				bad("scenario %q engine latency: %v", res.Scenario, err)
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (s Stats) check() error {
	if s.Min <= 0 || !(s.Min <= s.P50 && s.P50 <= s.P95 && s.P95 <= s.Max) || s.Mean < s.Min || s.Mean > s.Max {
		return fmt.Errorf("min %v p50 %v p95 %v max %v mean %v are not consistent", s.Min, s.P50, s.P95, s.Max, s.Mean)
	}
	return nil
}
