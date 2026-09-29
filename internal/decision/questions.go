package decision

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"ai-server/internal/jsonx"
)

// QuestionType is the System One question kind.
type QuestionType string

const (
	QuestionChoice QuestionType = "choice"
	QuestionScore  QuestionType = "score"
	QuestionNoul   QuestionType = "noul"
)

// QuestionBody is the sealed union of concrete question forms:
// *ChoiceQuestion, *ScoreQuestion, *NoulQuestion.
type QuestionBody interface {
	Type() QuestionType
	instructions() string
	sealedQuestion()
}

// ChoiceOption is one labeled option of a choice question.
type ChoiceOption struct {
	Key         string
	Description string
}

// ChoiceQuestion picks one of several labeled options. Option order is
// preserved from the request.
type ChoiceQuestion struct {
	Instructions string
	Criteria     []ChoiceOption
}

// ScoreQuestion rates on an ordinal scale; Criteria[i] describes level i.
type ScoreQuestion struct {
	Instructions string
	Criteria     []string
}

// NoulQuestion is a calibrated yes/no ("probability the statement holds").
type NoulQuestion struct {
	Instructions string
}

func (*ChoiceQuestion) Type() QuestionType { return QuestionChoice }
func (*ScoreQuestion) Type() QuestionType  { return QuestionScore }
func (*NoulQuestion) Type() QuestionType   { return QuestionNoul }

func (q *ChoiceQuestion) instructions() string { return q.Instructions }
func (q *ScoreQuestion) instructions() string  { return q.Instructions }
func (q *NoulQuestion) instructions() string   { return q.Instructions }

func (*ChoiceQuestion) sealedQuestion() {}
func (*ScoreQuestion) sealedQuestion()  {}
func (*NoulQuestion) sealedQuestion()   {}

// OptionCount is the number of options the model scores for q.
func OptionCount(q QuestionBody) int {
	switch v := q.(type) {
	case *ChoiceQuestion:
		return len(v.Criteria)
	case *ScoreQuestion:
		return len(v.Criteria)
	case *NoulQuestion:
		return 2
	}
	return 0
}

// Question is a named question.
type Question struct {
	ID   string
	Body QuestionBody
}

// Questions is an ordered set of questions. JSON form is an object keyed by
// question id; key order is preserved.
type Questions []Question

// UnmarshalJSON decodes the System One questions object.
func (qs *Questions) UnmarshalJSON(data []byte) error {
	var out Questions
	err := jsonx.DecodeObject(data, func(id string, raw json.RawMessage) error {
		body, err := parseQuestion(raw)
		if err != nil {
			return fmt.Errorf("questions.%s: %w", id, err)
		}
		out = append(out, Question{ID: id, Body: body})
		return nil
	})
	if err != nil {
		if strings.HasPrefix(err.Error(), "questions.") {
			return err
		}
		return fmt.Errorf("questions: %w", err)
	}
	*qs = out
	return nil
}

// MarshalJSON encodes questions in System One form, preserving order.
func (qs Questions) MarshalJSON() ([]byte, error) {
	var w jsonx.ObjectWriter
	for _, q := range qs {
		w.Add(q.ID, questionJSON{q.Body})
	}
	return w.Bytes()
}

type questionJSON struct{ QuestionBody }

func (q questionJSON) MarshalJSON() ([]byte, error) {
	var w jsonx.ObjectWriter
	w.Add("type", q.Type())
	w.Add("instructions", q.instructions())
	switch v := q.QuestionBody.(type) {
	case *ChoiceQuestion:
		var c jsonx.ObjectWriter
		for _, o := range v.Criteria {
			c.Add(o.Key, o.Description)
		}
		b, err := c.Bytes()
		if err != nil {
			return nil, err
		}
		w.Add("criteria", json.RawMessage(b))
	case *ScoreQuestion:
		w.Add("criteria", v.Criteria)
	}
	return w.Bytes()
}

func parseQuestion(raw json.RawMessage) (QuestionBody, error) {
	var head struct {
		Type         *string         `json:"type"`
		Instructions *string         `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&head); err != nil {
		return nil, fmt.Errorf("must be an object with type, instructions and criteria: %v", err)
	}
	if head.Type == nil {
		return nil, fmt.Errorf("type is required (choice, score, or noul)")
	}
	if head.Instructions == nil || strings.TrimSpace(*head.Instructions) == "" {
		return nil, fmt.Errorf("instructions must be a non-empty string")
	}
	ins := *head.Instructions
	hasCriteria := len(head.Criteria) > 0 && string(head.Criteria) != "null"
	switch QuestionType(*head.Type) {
	case QuestionChoice:
		if !hasCriteria {
			return nil, fmt.Errorf("choice questions require criteria")
		}
		opts, err := parseChoiceCriteria(head.Criteria)
		if err != nil {
			return nil, err
		}
		return &ChoiceQuestion{Instructions: ins, Criteria: opts}, nil
	case QuestionScore:
		if !hasCriteria {
			return nil, fmt.Errorf("score questions require a criteria list")
		}
		var levels []string
		if err := json.Unmarshal(head.Criteria, &levels); err != nil {
			return nil, fmt.Errorf("score criteria must be a list of strings")
		}
		if len(levels) == 0 {
			return nil, fmt.Errorf("score criteria must not be empty")
		}
		return &ScoreQuestion{Instructions: ins, Criteria: levels}, nil
	case QuestionNoul:
		if hasCriteria {
			return nil, fmt.Errorf("noul questions do not take criteria")
		}
		return &NoulQuestion{Instructions: ins}, nil
	}
	return nil, fmt.Errorf("unknown question type %q (choice, score, or noul)", *head.Type)
}

// parseChoiceCriteria accepts {"key": "description"|null, ...} (ordered) or
// ["key", ...].
func parseChoiceCriteria(raw json.RawMessage) ([]ChoiceOption, error) {
	trimmed := bytes.TrimSpace(raw)
	var opts []ChoiceOption
	switch {
	case len(trimmed) > 0 && trimmed[0] == '{':
		err := jsonx.DecodeObject(trimmed, func(k string, v json.RawMessage) error {
			var desc *string
			if err := json.Unmarshal(v, &desc); err != nil {
				return fmt.Errorf("criteria.%s must be a string", k)
			}
			o := ChoiceOption{Key: k}
			if desc != nil {
				o.Description = *desc
			}
			opts = append(opts, o)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("choice criteria: %w", err)
		}
	case len(trimmed) > 0 && trimmed[0] == '[':
		var keys []string
		if err := json.Unmarshal(trimmed, &keys); err != nil {
			return nil, fmt.Errorf("choice criteria list must contain strings")
		}
		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				return nil, fmt.Errorf("duplicate choice option %q", k)
			}
			seen[k] = true
			opts = append(opts, ChoiceOption{Key: k})
		}
	default:
		return nil, fmt.Errorf("choice criteria must be an object or a list")
	}
	if len(opts) == 0 {
		return nil, fmt.Errorf("choice criteria must not be empty")
	}
	for _, o := range opts {
		if strings.TrimSpace(o.Key) == "" {
			return nil, fmt.Errorf("choice option keys must be non-empty")
		}
	}
	return opts, nil
}
