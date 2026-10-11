package decision

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"ai-server/internal/jsonx"
)

// Probability is one entry of an ordered probability distribution.
type Probability struct {
	Key string
	P   float64
}

// Probabilities is an ordered distribution; JSON form is an object whose
// key order matches the question's option order.
type Probabilities []Probability

func (ps Probabilities) MarshalJSON() ([]byte, error) {
	var w jsonx.ObjectWriter
	for _, p := range ps {
		w.Add(p.Key, p.P)
	}
	return w.Bytes()
}

func (ps *Probabilities) UnmarshalJSON(data []byte) error {
	var out Probabilities
	err := jsonx.DecodeObject(data, func(k string, v json.RawMessage) error {
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return fmt.Errorf("probability %q must be a number", k)
		}
		out = append(out, Probability{Key: k, P: f})
		return nil
	})
	if err != nil {
		return err
	}
	*ps = out
	return nil
}

// AnswerBody is the sealed union of concrete answers.
type AnswerBody interface {
	Type() QuestionType
	sealedAnswer()
}

// ChoiceAnswer is the result of a choice question.
type ChoiceAnswer struct {
	Choice        string        `json:"choice"`
	Probabilities Probabilities `json:"probabilities"`
	Confidence    float64       `json:"confidence"`
}

// ScoreAnswer is the result of a score question. Score is the expected
// level index; Probabilities are keyed by level index ("0", "1", ...).
type ScoreAnswer struct {
	Score         float64       `json:"score"`
	Probabilities Probabilities `json:"probabilities"`
	Confidence    float64       `json:"confidence"`
}

// NoulAnswer is the result of a noul question: P(statement holds).
type NoulAnswer struct {
	Noul       float64 `json:"noul"`
	Confidence float64 `json:"confidence"`
}

func (*ChoiceAnswer) Type() QuestionType { return QuestionChoice }
func (*ScoreAnswer) Type() QuestionType  { return QuestionScore }
func (*NoulAnswer) Type() QuestionType   { return QuestionNoul }
func (*ChoiceAnswer) sealedAnswer()      {}
func (*ScoreAnswer) sealedAnswer()       {}
func (*NoulAnswer) sealedAnswer()        {}

// Answer is a named answer.
type Answer struct {
	ID        string
	Body      AnswerBody
	Citations []Citation
}

type Citation struct {
	Text        string  `json:"text"`
	Start       int     `json:"start"`
	End         int     `json:"end"`
	Probability float64 `json:"probability"`
}

type NumberCandidate struct {
	Value       float64 `json:"value"`
	Probability float64 `json:"probability"`
	Context     string  `json:"context"`
}

type NumberAnswer struct {
	Value       *float64          `json:"value"`
	Probability float64           `json:"probability"`
	NotStated   float64           `json:"not_stated"`
	Confidence  *float64          `json:"confidence"`
	Candidates  []NumberCandidate `json:"candidates"`
}

type ExcerptAnswer struct {
	Text        *string  `json:"text"`
	Start       *int     `json:"start,omitempty"`
	End         *int     `json:"end,omitempty"`
	Probability float64  `json:"probability"`
	Parts       []string `json:"parts,omitempty"`
}

func (*NumberAnswer) Type() QuestionType  { return QuestionNumber }
func (*ExcerptAnswer) Type() QuestionType { return QuestionExcerpt }
func (*NumberAnswer) sealedAnswer()       {}
func (*ExcerptAnswer) sealedAnswer()      {}

// Answers is ordered; JSON form is an object keyed by question id.
type Answers []Answer

func (as Answers) MarshalJSON() ([]byte, error) {
	var w jsonx.ObjectWriter
	for _, a := range as {
		b, err := marshalAnswer(a.Body)
		if err != nil {
			return nil, err
		}
		if a.Citations != nil {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(b, &fields); err != nil {
				return nil, err
			}
			fields["citations"], err = json.Marshal(a.Citations)
			if err != nil {
				return nil, err
			}
			b, err = json.Marshal(fields)
			if err != nil {
				return nil, err
			}
		}
		w.Add(a.ID, json.RawMessage(b))
	}
	return w.Bytes()
}

func marshalAnswer(a AnswerBody) ([]byte, error) {
	var w jsonx.ObjectWriter
	w.Add("type", a.Type())
	switch v := a.(type) {
	case *NumberAnswer:
		w.Add("value", v.Value)
		w.Add("probability", v.Probability)
		w.Add("not_stated", v.NotStated)
		w.Add("confidence", v.Confidence)
		w.Add("candidates", v.Candidates)
	case *ExcerptAnswer:
		w.Add("text", v.Text)
		w.Add("probability", v.Probability)
		if v.Start != nil {
			w.Add("start", v.Start)
		}
		if v.End != nil {
			w.Add("end", v.End)
		}
		if v.Parts != nil {
			w.Add("parts", v.Parts)
		}
	case *ChoiceAnswer:
		w.Add("choice", v.Choice)
		w.Add("probabilities", v.Probabilities)
		w.Add("confidence", v.Confidence)
	case *ScoreAnswer:
		w.Add("score", v.Score)
		w.Add("probabilities", v.Probabilities)
		w.Add("confidence", v.Confidence)
	case *NoulAnswer:
		w.Add("noul", v.Noul)
		w.Add("confidence", v.Confidence)
	default:
		return nil, fmt.Errorf("unknown answer type %T", a)
	}
	return w.Bytes()
}

func (as *Answers) UnmarshalJSON(data []byte) error {
	var out Answers
	err := jsonx.DecodeObject(data, func(id string, raw json.RawMessage) error {
		body, err := UnmarshalAnswer(raw)
		if err != nil {
			return fmt.Errorf("answers.%s: %w", id, err)
		}
		var metadata struct {
			Citations []Citation `json:"citations"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return err
		}
		out = append(out, Answer{ID: id, Body: body, Citations: metadata.Citations})
		return nil
	})
	if err != nil {
		return err
	}
	*as = out
	return nil
}

// UnmarshalAnswer decodes one tagged answer object. Unknown fields are
// ignored so engine-specific extras do not break decoding.
func UnmarshalAnswer(raw json.RawMessage) (AnswerBody, error) {
	var head struct {
		Type QuestionType `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	var body AnswerBody
	switch head.Type {
	case QuestionNumber:
		body = &NumberAnswer{}
	case QuestionExcerpt:
		body = &ExcerptAnswer{}
	case QuestionChoice:
		body = &ChoiceAnswer{}
	case QuestionScore:
		body = &ScoreAnswer{}
	case QuestionNoul:
		body = &NoulAnswer{}
	default:
		return nil, fmt.Errorf("unknown answer type %q", head.Type)
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(body); err != nil {
		return nil, err
	}
	return body, nil
}

// CheckAnswers verifies that answers match the questions one-to-one (ids,
// types and option keys). Adapters call it on engine output so malformed
// engine responses never reach clients.
func CheckAnswers(qs Questions, as Answers) error {
	if len(as) != len(qs) {
		return fmt.Errorf("engine returned %d answers for %d questions", len(as), len(qs))
	}
	byID := make(map[string]AnswerBody, len(as))
	for _, a := range as {
		if _, exists := byID[a.ID]; exists {
			return fmt.Errorf("duplicate answer %q", a.ID)
		}
		for _, citation := range a.Citations {
			if citation.Start < 0 || citation.End < citation.Start || !validProbability(citation.Probability) {
				return fmt.Errorf("answer %q has invalid citation offsets or probability", a.ID)
			}
		}
		switch body := a.Body.(type) {
		case *NumberAnswer:
			if !validProbability(body.Probability) || !validProbability(body.NotStated) {
				return fmt.Errorf("answer %q has invalid number probability", a.ID)
			}
			if body.Value != nil && (math.IsNaN(*body.Value) || math.IsInf(*body.Value, 0)) {
				return fmt.Errorf("answer %q has invalid number value", a.ID)
			}
			for _, candidate := range body.Candidates {
				if !validProbability(candidate.Probability) || math.IsNaN(candidate.Value) || math.IsInf(candidate.Value, 0) {
					return fmt.Errorf("answer %q has invalid number candidate", a.ID)
				}
			}
		case *ExcerptAnswer:
			if !validProbability(body.Probability) {
				return fmt.Errorf("answer %q has invalid excerpt probability", a.ID)
			}
			if body.Text != nil && (body.Start == nil || body.End == nil || *body.Start < 0 || *body.End < *body.Start) {
				return fmt.Errorf("answer %q has invalid excerpt offsets", a.ID)
			}
		}
		byID[a.ID] = a.Body
	}
	for _, q := range qs {
		a, ok := byID[q.ID]
		if !ok {
			return fmt.Errorf("engine returned no answer for %q", q.ID)
		}
		if a.Type() != q.Body.Type() {
			return fmt.Errorf("answer %q has type %s, question is %s", q.ID, a.Type(), q.Body.Type())
		}
		if q.Cite {
			found := false
			for _, answer := range as {
				if answer.ID == q.ID && answer.Citations != nil {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("answer %q omitted requested citations", q.ID)
			}
		}
		if cq, ok := q.Body.(*ChoiceQuestion); ok {
			ca := a.(*ChoiceAnswer)
			valid := map[string]bool{}
			for _, o := range cq.Criteria {
				valid[o.Key] = true
			}
			if !valid[ca.Choice] {
				return fmt.Errorf("answer %q chose unknown option %q", q.ID, ca.Choice)
			}
			for _, p := range ca.Probabilities {
				if !valid[p.Key] {
					return fmt.Errorf("answer %q has probability for unknown option %q", q.ID, p.Key)
				}
			}
		}
	}
	return nil
}

func validProbability(value float64) bool {
	return !math.IsNaN(value) && value >= 0 && value <= 1
}

// Reorder returns answers in question order.
func Reorder(qs Questions, as Answers) Answers {
	byID := make(map[string]Answer, len(as))
	for _, a := range as {
		byID[a.ID] = a
	}
	out := make(Answers, 0, len(qs))
	for _, q := range qs {
		if b, ok := byID[q.ID]; ok {
			out = append(out, b)
		}
	}
	return out
}
