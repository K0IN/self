package decision

import (
	"encoding/json"
	"fmt"

	"ai-server/internal/registry"
)

// Capabilities are the typed decision capabilities of a loaded model.
type Capabilities struct {
	Text       bool `json:"text"`
	Vision     bool `json:"vision"`
	MultiImage bool `json:"multi_image"`

	Choice bool `json:"choice"`
	Score  bool `json:"score"`
	Noul   bool `json:"noul"`

	// MaxImages is the maximum number of images per request (0 = none).
	MaxImages int `json:"max_images"`
	// MaxOptions is the maximum options per question (0 = unknown/unbounded).
	MaxOptions int `json:"max_options,omitempty"`
}

// CapabilitiesFromRegistry converts registry capability strings.
func CapabilitiesFromRegistry(c registry.Capabilities) Capabilities {
	out := Capabilities{
		Text: c.Input.Has(registry.CapText), Vision: c.Input.Has(registry.CapVision), MultiImage: c.Input.Has(registry.CapMultiImage),
		Choice: c.Output.Has(registry.CapChoice), Score: c.Output.Has(registry.CapScore), Noul: c.Output.Has(registry.CapNoul),
		MaxImages: c.MaxImages,
	}
	if out.Vision && out.MaxImages == 0 {
		out.MaxImages = 1
	}
	if !out.Vision {
		out.MaxImages = 0
	}
	return out
}

func (c Capabilities) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Input struct {
			Text       bool `json:"text"`
			Vision     bool `json:"vision"`
			MultiImage bool `json:"multi_image"`
			MaxImages  int  `json:"max_images,omitempty"`
		} `json:"input"`
		Output struct {
			Choice     bool `json:"choice"`
			Score      bool `json:"score"`
			Noul       bool `json:"noul"`
			MaxOptions int  `json:"max_options,omitempty"`
		} `json:"output"`
	}{Input: struct {
		Text       bool `json:"text"`
		Vision     bool `json:"vision"`
		MultiImage bool `json:"multi_image"`
		MaxImages  int  `json:"max_images,omitempty"`
	}{c.Text, c.Vision, c.MultiImage, c.MaxImages}, Output: struct {
		Choice     bool `json:"choice"`
		Score      bool `json:"score"`
		Noul       bool `json:"noul"`
		MaxOptions int  `json:"max_options,omitempty"`
	}{c.Choice, c.Score, c.Noul, c.MaxOptions}})
}

// Intersect returns capabilities supported by both a and b. Limits take the
// stricter non-zero value.
func (a Capabilities) Intersect(b Capabilities) Capabilities {
	out := Capabilities{
		Text:       a.Text && b.Text,
		Vision:     a.Vision && b.Vision,
		MultiImage: a.MultiImage && b.MultiImage,
		Choice:     a.Choice && b.Choice,
		Score:      a.Score && b.Score,
		Noul:       a.Noul && b.Noul,
		MaxImages:  minPositive(a.MaxImages, b.MaxImages),
		MaxOptions: minPositive(a.MaxOptions, b.MaxOptions),
	}
	if !out.Vision {
		out.MaxImages = 0
	} else if !out.MultiImage && out.MaxImages > 1 {
		out.MaxImages = 1
	}
	return out
}

func minPositive(a, b int) int {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	}
	return b
}

// Supports reports whether a question type is supported.
func (c Capabilities) Supports(t QuestionType) bool {
	switch t {
	case QuestionChoice:
		return c.Choice
	case QuestionScore:
		return c.Score
	case QuestionNoul:
		return c.Noul
	}
	return false
}

// CheckQuestions returns a descriptive error if any question cannot be
// served by a model with these capabilities.
func (c Capabilities) CheckQuestions(qs Questions) error {
	for _, q := range qs {
		t := q.Body.Type()
		if !c.Supports(t) {
			return fmt.Errorf("question %q: the loaded model does not support %s questions", q.ID, t)
		}
		if n := OptionCount(q.Body); c.MaxOptions > 0 && n > c.MaxOptions {
			return fmt.Errorf("question %q has %d options; the loaded model supports at most %d", q.ID, n, c.MaxOptions)
		}
	}
	return nil
}
