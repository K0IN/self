package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"ai-server/internal/adapters"
	"ai-server/internal/config"
	"ai-server/internal/decision"
	"ai-server/internal/errs"
	"ai-server/internal/models"
	"ai-server/internal/registry"
	"ai-server/internal/runtime"
	"ai-server/internal/settings"
)

// Check onboards a model end to end without HTTP: resolve, download, start
// the engine, send a fixed set of probe questions and verify the answers are
// well-formed and non-degenerate. Used by `self check` and `just check-model`.
func Check(ctx context.Context, cfg config.Serve, out io.Writer, isTTY bool) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	res, err := reg.Resolve(cfg.Model, registry.ResolveOptions{Quant: cfg.Quant, AdapterKnown: adapters.Known})
	if err != nil {
		return err
	}
	if res.Model.Type != registry.TypeDecision {
		return errs.New(errs.UnsupportedModel, "check supports decision models only")
	}
	entry, err := adapters.Decision(res.Variant.Adapter)
	if err != nil {
		return err
	}
	engine, err := runtime.Find(entry.Engine, runtime.SearchDirs(cfg.RuntimeDir))
	if err != nil {
		return err
	}
	set, _, err := ResolveSettings(cfg, res)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Model    %s\nQuant    %s\nAdapter  %s\nEngine   %s\n", res.ID(), res.Variant.Quant, res.Variant.Adapter, engine.Path)
	if len(set) > 0 {
		fmt.Fprintf(out, "Settings %s\n", settings.Format(set))
	}
	files, err := models.Ensure(ctx, models.Store{Root: cfg.ModelsDir}, models.NewDownloader(), res, func() models.Progress {
		fmt.Fprintln(out)
		return &models.TerminalProgress{W: out, TTY: isTTY}
	})
	if err != nil {
		return err
	}

	a := entry.New()
	t0 := time.Now()
	if err := a.Start(ctx, decision.RuntimeConfig{ModelID: res.ID(), Files: files, Device: cfg.Device, EnginePath: engine.Path, LibDir: engine.LibDir, Settings: set}); err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		a.Close(c)
	}()
	info, _ := a.Info(ctx)
	caps := modelCaps(res).Intersect(info.Capabilities)
	fmt.Fprintf(out, "\nLoaded in %s  (engine model %q, device %s)\n", time.Since(t0).Round(time.Millisecond), info.EngineModel, info.Device)
	fmt.Fprintf(out, "Capabilities  choice=%v score=%v noul=%v vision=%v max_options=%d\n\n", caps.Choice, caps.Score, caps.Noul, caps.Vision, caps.MaxOptions)
	if res.Model.Capabilities.Has(registry.CapVision) && !caps.Vision {
		return errs.New(errs.UnsupportedModel, "registry declares vision but the engine reports no image input")
	}

	failed := 0
	for _, p := range probes {
		var qs decision.Questions
		if err := json.Unmarshal([]byte(p.questions), &qs); err != nil {
			return err
		}
		if err := caps.CheckQuestions(qs); err != nil {
			fmt.Fprintf(out, "  SKIP  %-28s %v\n", p.name, err)
			continue
		}
		t := time.Now()
		resp, err := a.Decide(ctx, decision.Request{State: json.RawMessage(p.state), Questions: qs})
		if err != nil {
			failed++
			fmt.Fprintf(out, "  FAIL  %-28s %v\n", p.name, err)
			continue
		}
		verdict, detail := p.expect(resp.Answers)
		if !verdict {
			failed++
		}
		fmt.Fprintf(out, "  %s  %-28s %s  (%s)\n", map[bool]string{true: "ok  ", false: "FAIL"}[verdict], p.name, detail, time.Since(t).Round(time.Millisecond))
	}
	if failed > 0 {
		return errs.New(errs.UnsupportedModel, "%d probe(s) failed: the model loads but its answers look wrong for this adapter", failed)
	}
	fmt.Fprintf(out, "\nAll probes passed. %s is ready: self serve %s\n", res.ID(), res.ID())
	return nil
}

type probe struct {
	name      string
	state     string
	questions string
	expect    func(decision.Answers) (bool, string)
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

// probes are easy questions any working decision model answers correctly.
// They catch wrong prompt formats, wrong letter readouts and broken engines.
var probes = []probe{
	{
		name:      "choice: obvious routing",
		state:     `"My credit card was charged twice for the same order, please refund one payment."`,
		questions: `{"dept":{"type":"choice","instructions":"Which department should handle this?","criteria":{"billing":"Payments and refunds","technical":"Bugs and crashes","shipping":"Delivery problems"}}}`,
		expect: func(as decision.Answers) (bool, string) {
			c, ok := choiceOf(as, "dept")
			return ok && c.Choice == "billing", fmt.Sprintf("choice=%v", c.Choice)
		},
	},
	{
		name:      "choice: option order swap",
		state:     `"My credit card was charged twice for the same order, please refund one payment."`,
		questions: `{"dept":{"type":"choice","instructions":"Which department should handle this?","criteria":{"shipping":"Delivery problems","technical":"Bugs and crashes","billing":"Payments and refunds"}}}`,
		expect: func(as decision.Answers) (bool, string) {
			c, ok := choiceOf(as, "dept")
			return ok && c.Choice == "billing", fmt.Sprintf("choice=%v", c.Choice)
		},
	},
	{
		name:      "noul: clearly true",
		state:     `"The sky is blue on a clear day."`,
		questions: `{"q":{"type":"noul","instructions":"Does the text describe the color of the sky?"}}`,
		expect: func(as decision.Answers) (bool, string) {
			n, ok := noulOf(as, "q")
			return ok && n > 0.6, fmt.Sprintf("noul=%.3f (want > 0.6)", n)
		},
	},
	{
		name:      "noul: clearly false",
		state:     `"The sky is blue on a clear day."`,
		questions: `{"q":{"type":"noul","instructions":"Does the text talk about cooking pasta?"}}`,
		expect: func(as decision.Answers) (bool, string) {
			n, ok := noulOf(as, "q")
			return ok && n < 0.4, fmt.Sprintf("noul=%.3f (want < 0.4)", n)
		},
	},
	{
		name:      "score: ordering",
		state:     `"URGENT: the production database is down and no customer can log in."`,
		questions: `{"u":{"type":"score","instructions":"How urgent is this?","criteria":["not urgent","somewhat urgent","urgent","critical"]}}`,
		expect: func(as decision.Answers) (bool, string) {
			s, ok := scoreOf(as, "u")
			return ok && s >= 1.5, fmt.Sprintf("score=%.2f (want >= 1.5)", s)
		},
	},
}
