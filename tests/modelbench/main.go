// Command models runs the real-engine probe for every supported registry model.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-server/internal/registry"
)

type result struct {
	Model    string   `json:"model"`
	Duration string   `json:"duration"`
	Seconds  int64    `json:"seconds"`
	Passed   bool     `json:"passed"`
	Choice   string   `json:"choice,omitempty"`
	Expected string   `json:"expected,omitempty"`
	Checks   []string `json:"checks,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type systemOneRequest struct {
	State     map[string]string         `json:"state"`
	Questions map[string]choiceQuestion `json:"questions"`
}

type choiceQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type systemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]answer `json:"answers"`
	Usage   struct {
		LatencyMS float64 `json:"latency_ms"`
	} `json:"usage"`
}

type answer struct {
	Type       string  `json:"type"`
	Choice     string  `json:"choice"`
	Confidence float64 `json:"confidence"`
}

const questionID = "department"

func main() {
	if os.Getenv("SELF_MODEL_TEST") != "1" {
		fmt.Fprintln(os.Stderr, "set SELF_MODEL_TEST=1 to run real model tests")
		os.Exit(2)
	}

	bin := flag.String("bin", "./bin/self", "path to the self binary")
	out := flag.String("out", ".tmp/model-benchmark", "benchmark report directory")
	registryPath := flag.String("registry", "models/registry.yml", "path to the model registry")
	engineDir := flag.String("engine-dir", "bin/libexec/ai-server", "directory containing runtime engines")
	flag.Parse()

	data, err := os.ReadFile(*registryPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	parsed, err := registry.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ids := make([]string, 0, len(parsed.Models))
	for id := range parsed.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	results := make([]result, 0, len(ids))
	for _, id := range ids {
		results = append(results, run(context.Background(), *bin, *engineDir, id))
		if !results[len(results)-1].Passed && os.Getenv("MODEL_TEST_STOP_ON_ERROR") == "1" {
			break
		}
	}

	if err := writeReports(*out, results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	failed := 0
	for _, item := range results {
		status := "PASS"
		if !item.Passed {
			status = "FAIL"
			failed++
		}
		fmt.Printf("%-5s %-24s got=%-12s want=%-12s %s\n", status, item.Model, item.Choice, item.Expected, item.Duration)
	}
	if failed != 0 {
		os.Exit(1)
	}
}

func run(ctx context.Context, bin, engineDir, id string) result {
	started := time.Now()
	expected := expectedChoice(id)
	item := result{Model: id, Expected: expected}
	port, releasePort := freePort()
	defer releasePort()
	cmd := exec.CommandContext(ctx, bin, "serve", id, "--port", fmt.Sprint(port), "--runtime-dir", engineDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return finish(item, started, fmt.Errorf("start app: %w", err))
	}
	defer func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 30 * time.Second}
	if err := waitReady(ctx, client, baseURL); err != nil {
		return finish(item, started, err)
	}
	if err := checkModel(client, baseURL, id); err != nil {
		return finish(item, started, err)
	}
	choice, latency, err := callSystemOne(client, baseURL)
	item.Choice = choice
	item.Seconds = int64(time.Since(started) / time.Second)
	if err != nil {
		return finish(item, started, err)
	}
	item.Checks = append(item.Checks, "health", "model", "systemone", "choice")
	item.Passed = choice == expected
	if !item.Passed {
		return finish(item, started, fmt.Errorf("wrong choice: got %q, want %q (api latency %.1f ms)", choice, expected, latency))
	}
	return finish(item, started, nil)
}

func finish(item result, started time.Time, err error) result {
	item.Duration = time.Since(started).Round(time.Millisecond).String()
	item.Seconds = int64(time.Since(started) / time.Second)
	if err != nil {
		item.Error = err.Error()
	}
	return item
}

func expectedChoice(string) string { return "billing" }

func freePort() (int, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, func() {}
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, func() { _ = listener.Close() }
}

func waitReady(ctx context.Context, client *http.Client, baseURL string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return nil
			}
			if len(body) > 0 && resp.StatusCode != http.StatusServiceUnavailable {
				return fmt.Errorf("health failed: HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for app API")
}

func checkModel(client *http.Client, baseURL, want string) error {
	resp, err := client.Get(baseURL + "/v1/model")
	if err != nil {
		return fmt.Errorf("model request: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode model response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || body.ID != want {
		return fmt.Errorf("model identity: got %q, want %q", body.ID, want)
	}
	return nil
}

func callSystemOne(client *http.Client, baseURL string) (string, float64, error) {
	request := systemOneRequest{State: map[string]string{"message": "I was charged twice."}, Questions: map[string]choiceQuestion{questionID: {
		Type:         "choice",
		Instructions: "Which department should handle this request?",
		Criteria:     map[string]string{"billing": "Payments", "technical": "Technical support"},
	}}}
	payload, err := json.Marshal(request)
	if err != nil {
		return "", 0, err
	}
	resp, err := client.Post(baseURL+"/v1/systemone", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("systemone request: %w", err)
	}
	defer resp.Body.Close()
	var body systemOneResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", 0, fmt.Errorf("decode systemone response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("systemone failed: HTTP %s", resp.Status)
	}
	answer, ok := body.Answers[questionID]
	if !ok {
		return "", body.Usage.LatencyMS, fmt.Errorf("systemone response has no answer for %q", questionID)
	}
	if answer.Type != "choice" || answer.Confidence < 0 || answer.Confidence > 1 {
		return "", body.Usage.LatencyMS, fmt.Errorf("invalid answer: type=%q confidence=%.3f", answer.Type, answer.Confidence)
	}
	if answer.Choice != "billing" && answer.Choice != "technical" {
		return "", body.Usage.LatencyMS, fmt.Errorf("unknown choice %q", answer.Choice)
	}
	return answer.Choice, body.Usage.LatencyMS, nil
}

func writeReports(dir string, results []result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "model-benchmark.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}

	var report strings.Builder
	report.WriteString("# Model benchmark\n\n| Model | Status | Duration | Error |\n| --- | --- | ---: | --- |\n")
	for _, item := range results {
		status, message := "pass", ""
		if !item.Passed {
			status = "fail"
			message = strings.ReplaceAll(item.Error, "|", "\\|")
			message = strings.ReplaceAll(message, "\n", " ")
		}
		fmt.Fprintf(&report, "| `%s` | %s | %s | %s |\n", item.Model, status, item.Duration, message)
	}
	if err := os.WriteFile(filepath.Join(dir, "model-benchmark.md"), []byte(report.String()), 0o644); err != nil {
		return err
	}
	return nil
}
