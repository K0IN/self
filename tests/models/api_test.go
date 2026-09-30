package models_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	dec "ai-server/internal/decision"
)

// client talks to one running `self serve`.
type client struct {
	base string
	http *http.Client
}

func newClient(base string) *client {
	return &client{base: base, http: &http.Client{Timeout: 3 * time.Minute}}
}

// decideRequest is the public /v1/systemone body. Empty fields are omitted so
// error cases can leave them out.
type decideRequest struct {
	State     json.RawMessage `json:"state,omitempty"`
	Questions json.RawMessage `json:"questions,omitempty"`
	Images    []string        `json:"images,omitempty"`
	Model     string          `json:"model,omitempty"`
}

func textRequest(state string, questions json.RawMessage) decideRequest {
	return decideRequest{State: str(state), Questions: questions}
}

type reply struct {
	Status int
	Body   []byte
	Wall   time.Duration
}

func (c *client) do(ctx context.Context, method, path, contentType string, body []byte) (reply, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return reply{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return reply{Status: resp.StatusCode, Body: data, Wall: time.Since(start)}, err
}

func (c *client) get(ctx context.Context, path string) (reply, error) {
	return c.do(ctx, http.MethodGet, path, "", nil)
}

// apiError is a non-200 API answer.
type apiError struct {
	Status  int
	Type    string
	Message string
}

func (e *apiError) Error() string {
	if e.Type == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Type, e.Message)
}

func errorOf(r reply) *apiError {
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &body)
	return &apiError{Status: r.Status, Type: body.Error.Type, Message: body.Error.Message}
}

// decided is a successful decision response.
type decided struct {
	Model   string        `json:"model"`
	Answers dec.Answers   `json:"answers"`
	Usage   dec.Usage     `json:"usage"`
	Wall    time.Duration `json:"-"`
}

// post sends a decision request and returns the raw reply.
func (c *client) post(ctx context.Context, req decideRequest) (reply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return reply{}, err
	}
	return c.do(ctx, http.MethodPost, "/v1/systemone", "application/json", body)
}

// decide requires HTTP 200; other statuses come back as *apiError.
func (c *client) decide(ctx context.Context, req decideRequest) (decided, error) {
	r, err := c.post(ctx, req)
	if err != nil {
		return decided{}, err
	}
	if r.Status != http.StatusOK {
		return decided{}, errorOf(r)
	}
	var d decided
	if err := json.Unmarshal(r.Body, &d); err != nil {
		return decided{}, fmt.Errorf("decode response: %w", err)
	}
	d.Wall = r.Wall
	return d, nil
}

// expectStatus reports whether err is an API error with the given status and type.
func expectStatus(err error, status int, typ string) error {
	var se *apiError
	if !errors.As(err, &se) {
		if err == nil {
			return fmt.Errorf("accepted, want HTTP %d %s", status, typ)
		}
		return err
	}
	if se.Status != status || se.Type != typ {
		return fmt.Errorf("got %s, want HTTP %d %s", se, status, typ)
	}
	return nil
}

// modelDoc is the /v1/model body.
type modelDoc struct {
	ID           string `json:"id"`
	Object       string `json:"object"`
	Type         string `json:"type"`
	Quant        string `json:"quant"`
	Capabilities struct {
		Input struct {
			Text       bool `json:"text"`
			Vision     bool `json:"vision"`
			MultiImage bool `json:"multi_image"`
			MaxImages  int  `json:"max_images"`
		} `json:"input"`
		Output struct {
			Choice     bool `json:"choice"`
			Score      bool `json:"score"`
			Noul       bool `json:"noul"`
			MaxOptions int  `json:"max_options"`
		} `json:"output"`
	} `json:"capabilities"`
	Info struct {
		ContextLength int `json:"context_length"`
		MaxOptions    int `json:"max_options"`
	} `json:"info"`
	Settings map[string]any `json:"settings"`
}

func (d *modelDoc) decisionCaps() dec.Capabilities {
	if d == nil {
		return dec.Capabilities{}
	}
	in, out := d.Capabilities.Input, d.Capabilities.Output
	return dec.Capabilities{
		Text: in.Text, Vision: in.Vision, MultiImage: in.MultiImage, MaxImages: in.MaxImages,
		Choice: out.Choice, Score: out.Score, Noul: out.Noul, MaxOptions: out.MaxOptions,
	}
}

func (c *client) model(ctx context.Context) (*modelDoc, error) {
	r, err := c.get(ctx, "/v1/model")
	if err != nil {
		return nil, err
	}
	if r.Status != http.StatusOK {
		return nil, errorOf(r)
	}
	var d modelDoc
	if err := json.Unmarshal(r.Body, &d); err != nil {
		return nil, fmt.Errorf("decode /v1/model: %w", err)
	}
	return &d, nil
}
