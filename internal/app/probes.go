package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-server/internal/decision"
)

// Probe is one fixed request (state and questions as System One JSON) with a
// verdict on the answers. `self check` and the model benchmark share them.
type Probe struct {
	Name      string
	State     string
	Questions string
	Expect    func(decision.Answers) (bool, string)
}

// probeResult is the outcome of one probe: skipped when the model cannot
// answer that question type, failed when the request errors (err) or the
// answer is wrong (ok false).
type probeResult struct {
	name    string
	skipped bool
	ok      bool
	detail  string
	err     error
	took    time.Duration
}

// runProbes sends every probe the model can answer and judges the answers.
// each is called after every probe. It returns how many probes ran and how many
// of those passed; a malformed probe is a programming error and stops the run.
func runProbes(ctx context.Context, a decision.Adapter, caps decision.Capabilities, each func(probeResult)) (passed, total int, err error) {
	for _, p := range Probes {
		var qs decision.Questions
		if err := json.Unmarshal([]byte(p.Questions), &qs); err != nil {
			return passed, total, fmt.Errorf("probe %q: %w", p.Name, err)
		}
		r := probeResult{name: p.Name}
		if cerr := caps.CheckQuestions(qs); cerr != nil {
			r.skipped, r.detail = true, cerr.Error()
			each(r)
			continue
		}
		total++
		start := time.Now()
		resp, derr := a.Decide(ctx, decision.Request{State: json.RawMessage(p.State), Questions: qs})
		r.took = time.Since(start)
		if derr != nil {
			r.err = derr
		} else {
			r.ok, r.detail = p.Expect(resp.Answers)
			if r.ok {
				passed++
			}
		}
		each(r)
	}
	return passed, total, nil
}

func choiceOf(as decision.Answers, id string) (*decision.ChoiceAnswer, bool) {
	for _, a := range as {
		if a.ID == id {
			c, ok := a.Body.(*decision.ChoiceAnswer)
			return c, ok
		}
	}
	return nil, false
}

func noulOf(as decision.Answers, id string) (float64, bool) {
	for _, a := range as {
		if a.ID == id {
			if n, ok := a.Body.(*decision.NoulAnswer); ok {
				return n.Noul, true
			}
		}
	}
	return 0, false
}

func scoreOf(as decision.Answers, id string) (float64, bool) {
	for _, a := range as {
		if a.ID == id {
			if s, ok := a.Body.(*decision.ScoreAnswer); ok {
				return s.Score, true
			}
		}
	}
	return 0, false
}

func wantChoice(as decision.Answers, id, want string) (bool, string) {
	c, ok := choiceOf(as, id)
	if !ok {
		return false, "no choice answer"
	}
	return c.Choice == want, fmt.Sprintf("choice=%v", c.Choice)
}

// Probes are easy questions any working decision model answers correctly.
// They catch wrong prompt formats, wrong letter readouts and broken engines.
var Probes = []Probe{
	{
		Name:      "choice: obvious routing",
		State:     `"My credit card was charged twice for the same order, please refund one payment."`,
		Questions: `{"dept":{"type":"choice","instructions":"Which department should handle this?","criteria":{"billing":"Payments and refunds","technical":"Bugs and crashes","shipping":"Delivery problems"}}}`,
		Expect:    func(as decision.Answers) (bool, string) { return wantChoice(as, "dept", "billing") },
	},
	{
		Name:      "choice: option order swap",
		State:     `"My credit card was charged twice for the same order, please refund one payment."`,
		Questions: `{"dept":{"type":"choice","instructions":"Which department should handle this?","criteria":{"shipping":"Delivery problems","technical":"Bugs and crashes","billing":"Payments and refunds"}}}`,
		Expect:    func(as decision.Answers) (bool, string) { return wantChoice(as, "dept", "billing") },
	},
	{
		Name:      "noul: clearly true",
		State:     `"The sky is blue on a clear day."`,
		Questions: `{"q":{"type":"noul","instructions":"Does the text describe the color of the sky?"}}`,
		Expect: func(as decision.Answers) (bool, string) {
			n, ok := noulOf(as, "q")
			return ok && n > 0.6, fmt.Sprintf("noul=%.3f (want > 0.6)", n)
		},
	},
	{
		Name:      "noul: clearly false",
		State:     `"The sky is blue on a clear day."`,
		Questions: `{"q":{"type":"noul","instructions":"Does the text talk about cooking pasta?"}}`,
		Expect: func(as decision.Answers) (bool, string) {
			n, ok := noulOf(as, "q")
			return ok && n < 0.4, fmt.Sprintf("noul=%.3f (want < 0.4)", n)
		},
	},
	{
		Name:      "score: ordering",
		State:     `"URGENT: the production database is down and no customer can log in."`,
		Questions: `{"u":{"type":"score","instructions":"How urgent is this?","criteria":["not urgent","somewhat urgent","urgent","critical"]}}`,
		Expect: func(as decision.Answers) (bool, string) {
			s, ok := scoreOf(as, "u")
			return ok && s >= 1.5, fmt.Sprintf("score=%.2f (want >= 1.5)", s)
		},
	},
}
