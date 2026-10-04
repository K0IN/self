package benchmark

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"ai-server/internal/decision"
)

// Decider is what a benchmark needs from an engine adapter.
type Decider interface {
	Decide(ctx context.Context, req decision.Request) (decision.Response, error)
}

// OperationResult contains the measurable work completed by one request.
// Throughput is expressed in Unit, for example input_tokens_per_sec,
// audio_seconds_per_sec, or pixels_per_sec.
type OperationResult struct {
	InputTokens  int
	OutputTokens int
	Work         float64
	Unit         string
	EngineMS     float64
}

// Operation is one modality-specific benchmark request.
type Operation func(context.Context) (OperationResult, error)

// WorkScenario describes repeatable requests for non-decision modalities.
type WorkScenario struct {
	Name     string
	Requests int
}

// RunOperations measures typed adapter operations using the same warmup and
// caller-visible latency rules as decision benchmarks.
func RunOperations(ctx context.Context, op Operation, scenarios []WorkScenario, warmup int, progress func(Result)) ([]Result, error) {
	results := make([]Result, 0, len(scenarios))
	for _, scenario := range scenarios {
		result, err := measureOperation(ctx, op, scenario, warmup)
		if err != nil {
			return results, fmt.Errorf("scenario %s: %w", scenario.Name, err)
		}
		if progress != nil {
			progress(result)
		}
		results = append(results, result)
	}
	return results, nil
}

func measureOperation(ctx context.Context, op Operation, scenario WorkScenario, warmup int) (Result, error) {
	for i := 0; i < warmup; i++ {
		if _, err := op(ctx); err != nil {
			return Result{}, fmt.Errorf("warmup: %w", err)
		}
	}
	var walls, engine []float64
	var total time.Duration
	input, output := 0, 0
	var work float64
	unit := ""
	for i := 0; i < scenario.Requests; i++ {
		start := time.Now()
		measurement, err := op(ctx)
		wall := time.Since(start)
		if err != nil {
			return Result{}, fmt.Errorf("request %d: %w", i+1, err)
		}
		total += wall
		walls = append(walls, float64(wall)/float64(time.Millisecond))
		input += measurement.InputTokens
		output += measurement.OutputTokens
		work += measurement.Work
		unit = measurement.Unit
		if measurement.EngineMS > 0 {
			engine = append(engine, measurement.EngineMS)
		}
	}
	result := Result{Scenario: scenario.Name, Requests: scenario.Requests, InputTokens: input / scenario.Requests, OutputTokens: output / scenario.Requests, Latency: summarize(walls), ThroughputUnit: unit}
	if total.Seconds() > 0 {
		result.Throughput = round(work/total.Seconds(), 1)
	}
	if len(engine) > 0 {
		stats := summarize(engine)
		result.Engine = &stats
	}
	result.RequestsPerSec = round(float64(scenario.Requests)/total.Seconds(), 2)
	if input > 0 {
		result.InputTokensPerSec = round(float64(input)/total.Seconds(), 1)
	}
	if output > 0 {
		result.OutputTokensPerSec = round(float64(output)/total.Seconds(), 1)
	}
	return result, nil
}

// Run measures every scenario in order: warmup requests that are not
// counted, then the timed ones. progress, if set, is called after each
// scenario. Any failing request stops the run, because timings of a model
// that errors mean nothing.
func Run(ctx context.Context, d Decider, scenarios []Scenario, warmup int, progress func(Result)) ([]Result, error) {
	results := make([]Result, 0, len(scenarios))
	for _, s := range scenarios {
		r, err := measure(ctx, d, s, warmup)
		if err != nil {
			return results, fmt.Errorf("scenario %s: %w", s.Name, err)
		}
		if progress != nil {
			progress(r)
		}
		results = append(results, r)
	}
	return results, nil
}

func measure(ctx context.Context, d Decider, s Scenario, warmup int) (Result, error) {
	for i := 0; i < warmup; i++ {
		if _, err := d.Decide(ctx, s.Request); err != nil {
			return Result{}, fmt.Errorf("warmup: %w", err)
		}
	}
	var (
		walls, engine []float64
		total         time.Duration
		in, out       int
	)
	for i := 0; i < s.Requests; i++ {
		start := time.Now()
		resp, err := d.Decide(ctx, s.Request)
		wall := time.Since(start)
		if err != nil {
			return Result{}, fmt.Errorf("request %d: %w", i+1, err)
		}
		total += wall
		walls = append(walls, float64(wall)/float64(time.Millisecond))
		if resp.Usage.LatencyMS > 0 {
			engine = append(engine, resp.Usage.LatencyMS)
		}
		in += resp.Usage.InputTokens
		out += resp.Usage.OutputTokens
	}
	r := Result{Scenario: s.Name, Requests: s.Requests, InputTokens: in / s.Requests, OutputTokens: out / s.Requests, Latency: summarize(walls)}
	if len(engine) > 0 {
		e := summarize(engine)
		r.Engine = &e
	}
	r.RequestsPerSec, r.InputTokensPerSec, r.OutputTokensPerSec = rates(s.Requests, in, out, total)
	return r, nil
}

// rates derives throughput from the unrounded totals. Rounding requests per
// second first would turn a slow scenario into 0 and skew every tokens/s.
func rates(requests, in, out int, total time.Duration) (req, inTok, outTok float64) {
	s := total.Seconds()
	if s <= 0 {
		return 0, 0, 0
	}
	return round(float64(requests)/s, 2), round(float64(in)/s, 1), round(float64(out)/s, 1)
}

// summarize computes latency statistics rounded to 0.01 ms; percentiles use
// the nearest-rank method.
func summarize(values []float64) Stats {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	rank := func(p float64) float64 {
		i := int(math.Ceil(p/100*float64(len(sorted)))) - 1
		return round(sorted[max(i, 0)], 2)
	}
	return Stats{
		Min: round(sorted[0], 2), Mean: round(sum/float64(len(sorted)), 2),
		P50: rank(50), P95: rank(95), Max: round(sorted[len(sorted)-1], 2),
	}
}

func round(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}
