// Package decision exposes the System One decision API over HTTP.
package decision

import (
	"bytes"
	"encoding/json"
	"fmt"

	dom "ai-server/internal/decision"
)

// SystemOneRequest is the public request body.
type SystemOneRequest struct {
	State     json.RawMessage `json:"state"`
	Images    []ImageInput    `json:"images,omitempty"`
	Questions dom.Questions   `json:"questions"`
	// Model is accepted for TypeSafe SDK compatibility and must match the
	// loaded model when set.
	Model string `json:"model,omitempty"`
}

// ImageInput accepts "https://..." or {"url": ..., "name": ..., "description": ...}.
type ImageInput struct {
	URL         string `json:"url"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

func (im *ImageInput) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &im.URL)
	}
	type plain ImageInput
	var p plain
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("image must be a URL string or an object with url, name, description: %v", err)
	}
	*im = ImageInput(p)
	return nil
}

// SystemOneResponse is the public response body.
type SystemOneResponse struct {
	Model   string      `json:"model"`
	Answers dom.Answers `json:"answers"`
	Usage   dom.Usage   `json:"usage"`
}
