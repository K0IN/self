package ggmlclaya

import (
	"encoding/json"
	"fmt"

	"ai-server/internal/decision"
)

// Upstream `laya daemon` protocol (examples/laya/src/main.cpp, run_daemon):
//
//	stdout line 1:  {"status":"ready","model":"laya"}
//	request line:   {"id":<any>,"state":<json>,"questions":{...}}
//	response line:  {"model":..., "answers":{...}, "usage":{...}, "id":<same>}
//	error line:     {"id":<same>,"error":"..."}   (or without id on parse errors)
//
// One JSON object per line; logs go to stderr. The daemon processes lines
// strictly in order. It has no image input.

type readyLine struct {
	Status string `json:"status"`
	Model  string `json:"model"`
}

type requestLine struct {
	ID        uint64             `json:"id"`
	State     json.RawMessage    `json:"state"`
	Questions decision.Questions `json:"questions"`
}

type responseLine struct {
	ID      *uint64         `json:"id"`
	Model   string          `json:"model"`
	Answers json.RawMessage `json:"answers"`
	Usage   struct {
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		LatencyMS    float64 `json:"latency_ms"`
	} `json:"usage"`
	Error *string `json:"error"`
}

// translateResponse converts an upstream response into the typed domain
// response, validating it against the request's questions.
func translateResponse(line []byte, wantID uint64, qs decision.Questions) (decision.Response, error) {
	var r responseLine
	if err := json.Unmarshal(line, &r); err != nil {
		return decision.Response{}, fmt.Errorf("malformed engine response: %w", err)
	}
	if r.ID == nil {
		if r.Error != nil {
			return decision.Response{}, fmt.Errorf("engine rejected request: %s", *r.Error)
		}
		return decision.Response{}, fmt.Errorf("engine response has no id")
	}
	if *r.ID != wantID {
		return decision.Response{}, fmt.Errorf("engine response id %d does not match request id %d", *r.ID, wantID)
	}
	if r.Error != nil {
		return decision.Response{}, &engineError{msg: *r.Error}
	}
	var answers decision.Answers
	if err := json.Unmarshal(r.Answers, &answers); err != nil {
		return decision.Response{}, fmt.Errorf("malformed engine answers: %w", err)
	}
	if err := decision.CheckAnswers(qs, answers); err != nil {
		return decision.Response{}, err
	}
	return decision.Response{
		Answers: decision.Reorder(qs, answers),
		Usage: decision.Usage{
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
			LatencyMS:    r.Usage.LatencyMS,
		},
	}, nil
}

// engineError is a request-level error reported by the engine (the engine
// itself is still healthy).
type engineError struct{ msg string }

func (e *engineError) Error() string { return e.msg }
