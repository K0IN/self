package models_test

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-server/internal/app"
	dec "ai-server/internal/decision"
	"ai-server/internal/registry"
)

// env is what a check needs: the server under test and what the registry and
// the server say about the model.
type env struct {
	c      *client
	res    registry.Resolved
	doc    *modelDoc // nil when /v1/model failed
	docErr error
}

func newEnv(ctx context.Context, c *client, res registry.Resolved) *env {
	doc, err := c.model(ctx)
	return &env{c: c, res: res, doc: doc, docErr: err}
}

func (e *env) caps() dec.Capabilities { return e.doc.decisionCaps() }

type skipError struct{ reason string }

func (s skipError) Error() string { return s.reason }

func skipf(format string, args ...any) error { return skipError{fmt.Sprintf(format, args...)} }

// check returns a detail line on success, skipError when it does not apply,
// and any other error on failure.
type check struct {
	name string
	run  func(ctx context.Context, e *env) (string, error)
}

const (
	statusPass = "pass"
	statusFail = "fail"
	statusSkip = "skip"
)

// runCheck runs one check and classifies the outcome.
func runCheck(ctx context.Context, e *env, c check) (status, detail string) {
	detail, err := c.run(ctx, e)
	var skip skipError
	switch {
	case errors.As(err, &skip):
		return statusSkip, skip.reason
	case err != nil:
		return statusFail, err.Error()
	}
	return statusPass, detail
}

func modelChecks() []check {
	checks := []check{
		{"api: health", checkHealth},
		{"api: model metadata", checkMetadata},
	}
	for _, p := range app.Probes {
		checks = append(checks, probeCheck(p))
	}
	return append(checks,
		check{"answers: multi-question request", checkMultiQuestion},
		check{"answers: probabilities are well formed", checkProbabilities},
		check{"answers: unicode input", checkUnicode},
		check{"limits: max options accepted", checkMaxOptions},
		check{"limits: too many options rejected", checkTooManyOptions},
		check{"api: invalid requests rejected", checkInvalidRequests},
		check{"api: model field is ignored", checkModelIgnored},
		check{"queue: concurrent requests", checkConcurrent},
		check{"vision: text-only model rejects images", checkTextOnlyRejectsImages},
		check{"vision: solid colors", checkColors},
		check{"vision: broken image rejected", checkBrokenImage},
		check{"vision: too many images rejected", checkTooManyImages},
	)
}

func checkHealth(ctx context.Context, e *env) (string, error) {
	r, err := e.c.get(ctx, "/health")
	if err != nil {
		return "", err
	}
	if r.Status != http.StatusOK || !strings.Contains(string(r.Body), `"status":"ok"`) || !strings.Contains(string(r.Body), `"runner":"ready"`) {
		return "", fmt.Errorf("HTTP %d %s", r.Status, strings.TrimSpace(string(r.Body)))
	}
	return "", nil
}

func checkMetadata(ctx context.Context, e *env) (string, error) {
	if e.docErr != nil {
		return "", e.docErr
	}
	d, m := e.doc, e.res.Model
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if d.ID != e.res.ID() {
		add("id %q, want %q", d.ID, e.res.ID())
	}
	if d.Quant != e.res.Variant.Quant {
		add("quant %q, want %q", d.Quant, e.res.Variant.Quant)
	}
	if d.Type != string(m.Type) || d.Object != "model" {
		add("type %q object %q", d.Type, d.Object)
	}
	in, out := d.Capabilities.Input, d.Capabilities.Output
	if !in.Text {
		add("text input not reported")
	}
	if want := m.Capabilities.Input.Has(registry.CapVision); in.Vision != want {
		add("vision=%v, registry says %v", in.Vision, want)
	}
	for _, c := range []struct {
		name     string
		got      bool
		declared bool
	}{
		{"choice", out.Choice, m.Capabilities.Output.Has(registry.CapChoice)},
		{"score", out.Score, m.Capabilities.Output.Has(registry.CapScore)},
		{"noul", out.Noul, m.Capabilities.Output.Has(registry.CapNoul)},
	} {
		if c.got != c.declared {
			add("%s=%v, registry says %v", c.name, c.got, c.declared)
		}
	}
	if out.MaxOptions <= 0 || (m.Info.MaxOptions > 0 && out.MaxOptions > m.Info.MaxOptions) {
		add("max_options %d (registry %d)", out.MaxOptions, m.Info.MaxOptions)
	}
	r, err := e.c.get(ctx, "/v1/models")
	if err != nil {
		return "", err
	}
	if r.Status != http.StatusOK || !strings.Contains(string(r.Body), fmt.Sprintf("%q", e.res.ID())) {
		add("/v1/models does not list the model (HTTP %d)", r.Status)
	}
	if len(problems) > 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	return fmt.Sprintf("vision=%v max_options=%d", in.Vision, out.MaxOptions), nil
}

func probeCheck(p app.Probe) check {
	return check{p.Name, func(ctx context.Context, e *env) (string, error) {
		var qs dec.Questions
		if err := qs.UnmarshalJSON([]byte(p.Questions)); err != nil {
			return "", err
		}
		if err := e.caps().CheckQuestions(qs); err != nil {
			return "", skipf("%v", err)
		}
		d, err := e.c.decide(ctx, decideRequest{State: []byte(p.State), Questions: []byte(p.Questions)})
		if err != nil {
			return "", err
		}
		ok, detail := p.Expect(d.Answers)
		if !ok {
			return "", errors.New(detail)
		}
		return detail, nil
	}}
}

// requireTypes skips a check when the model lacks a question type.
func requireTypes(e *env, choice, score, noul bool) error {
	c := e.caps()
	for _, n := range []struct {
		need, have bool
		name       string
	}{{choice, c.Choice, "choice"}, {score, c.Score, "score"}, {noul, c.Noul, "noul"}} {
		if n.need && !n.have {
			return skipf("model does not answer %s questions", n.name)
		}
	}
	return nil
}

func checkMultiQuestion(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, true, true); err != nil {
		return "", err
	}
	d, err := e.c.decide(ctx, textRequest(ticketText, multiQuestions()))
	if err != nil {
		return "", err
	}
	want := []struct {
		id  string
		typ dec.QuestionType
	}{{deptQuestionID, dec.QuestionChoice}, {"refund", dec.QuestionNoul}, {"urgency", dec.QuestionScore}}
	if len(d.Answers) != len(want) {
		return "", fmt.Errorf("%d answers, want %d", len(d.Answers), len(want))
	}
	for i, w := range want {
		a := d.Answers[i]
		if a.ID != w.id || a.Body.Type() != w.typ {
			return "", fmt.Errorf("answer %d is %s (%s), want %s (%s): answers must follow the request order", i, a.ID, a.Body.Type(), w.id, w.typ)
		}
	}
	choice := d.Answers[0].Body.(*dec.ChoiceAnswer)
	refund := d.Answers[1].Body.(*dec.NoulAnswer)
	urgency := d.Answers[2].Body.(*dec.ScoreAnswer)
	switch {
	case choice.Choice != billingKey:
		return "", fmt.Errorf("choice=%q, want %q", choice.Choice, billingKey)
	case refund.Noul <= 0.5:
		return "", fmt.Errorf("refund noul=%.3f, want > 0.5", refund.Noul)
	case urgency.Score < 0 || urgency.Score > 2:
		return "", fmt.Errorf("urgency score=%.2f outside the 3 levels", urgency.Score)
	}
	return fmt.Sprintf("choice=%s noul=%.3f score=%.2f", choice.Choice, refund.Noul, urgency.Score), nil
}

func checkProbabilities(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, false, false); err != nil {
		return "", err
	}
	d, err := e.c.decide(ctx, textRequest(ticketText, routingQuestions()))
	if err != nil {
		return "", err
	}
	if len(d.Answers) != 1 {
		return "", fmt.Errorf("%d answers, want 1", len(d.Answers))
	}
	a, ok := d.Answers[0].Body.(*dec.ChoiceAnswer)
	if !ok {
		return "", fmt.Errorf("answer is %s, want choice", d.Answers[0].Body.Type())
	}
	if len(a.Probabilities) != len(routingOptions) {
		return "", fmt.Errorf("%d probabilities, want %d", len(a.Probabilities), len(routingOptions))
	}
	sum, best, bestKey := 0.0, -1.0, ""
	for i, p := range a.Probabilities {
		if p.Key != routingOptions[i].Key {
			return "", fmt.Errorf("probability %d is %q, want %q (option order must be kept)", i, p.Key, routingOptions[i].Key)
		}
		if p.P < 0 || p.P > 1 {
			return "", fmt.Errorf("probability %s=%.3f is outside [0,1]", p.Key, p.P)
		}
		sum += p.P
		if p.P > best {
			best, bestKey = p.P, p.Key
		}
	}
	switch {
	case sum < 0.95 || sum > 1.05:
		return "", fmt.Errorf("probabilities sum to %.3f, want 1", sum)
	case bestKey != a.Choice:
		return "", fmt.Errorf("choice %q is not the most probable option (%q)", a.Choice, bestKey)
	case a.Confidence < 0 || a.Confidence > 1:
		return "", fmt.Errorf("confidence %.3f is outside [0,1]", a.Confidence)
	case d.Usage.InputTokens <= 0:
		return "", fmt.Errorf("usage.input_tokens is %d", d.Usage.InputTokens)
	}
	return fmt.Sprintf("sum=%.3f confidence=%.3f input_tokens=%d", sum, a.Confidence, d.Usage.InputTokens), nil
}

func checkUnicode(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, false, false); err != nil {
		return "", err
	}
	text := "Kundin schreibt: Meine Kreditkarte wurde doppelt belastet. 请退款。 お願いします 💳 \"quotes\" \\ <tag> \u00e9\u00e8"
	d, err := e.c.decide(ctx, textRequest(text, routingQuestions()))
	if err != nil {
		return "", err
	}
	a, ok := d.Answers[0].Body.(*dec.ChoiceAnswer)
	if !ok {
		return "", fmt.Errorf("answer is %s, want choice", d.Answers[0].Body.Type())
	}
	for _, o := range routingOptions {
		if a.Choice == o.Key {
			return "choice=" + a.Choice, nil
		}
	}
	return "", fmt.Errorf("choice %q is not one of the options", a.Choice)
}

func checkMaxOptions(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, false, false); err != nil {
		return "", err
	}
	n := e.caps().MaxOptions
	d, err := e.c.decide(ctx, textRequest(ticketText, wideQuestions(n)))
	if err != nil {
		return "", err
	}
	a, ok := d.Answers[0].Body.(*dec.ChoiceAnswer)
	if !ok {
		return "", fmt.Errorf("answer is %s, want choice", d.Answers[0].Body.Type())
	}
	if len(a.Probabilities) != n {
		return "", fmt.Errorf("%d probabilities for %d options", len(a.Probabilities), n)
	}
	if a.Choice != billingKey {
		return "", fmt.Errorf("choice=%q with %d options, want %q (the last option)", a.Choice, n, billingKey)
	}
	return fmt.Sprintf("%d options, choice=%s", n, a.Choice), nil
}

func checkTooManyOptions(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, false, false); err != nil {
		return "", err
	}
	n := e.caps().MaxOptions + 1
	_, err := e.c.decide(ctx, textRequest(ticketText, wideQuestions(n)))
	if err := expectStatus(err, http.StatusUnprocessableEntity, "unsupported_capability"); err != nil {
		return "", fmt.Errorf("%d options: %w", n, err)
	}
	return fmt.Sprintf("%d options -> 422", n), nil
}

// checkModelIgnored: one server runs one model, so the model named in a request is not checked.
func checkModelIgnored(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, false, false); err != nil {
		return "", err
	}
	req := textRequest(ticketText, routingQuestions())
	req.Model = "nope:1b"
	if _, err := e.c.decide(ctx, req); err != nil {
		return "", err
	}
	return "model nope:1b ignored", nil
}

func checkInvalidRequests(ctx context.Context, e *env) (string, error) {
	valid := string(routingQuestions())
	const json = "application/json"
	cases := []struct {
		name, contentType, body string
		status                  int
		typ                     string
	}{
		{"unknown field", json, `{"state":"x","questions":` + valid + `,"bogus":1}`, 400, "invalid_request"},
		{"missing state", json, `{"questions":` + valid + `}`, 400, "invalid_request"},
		{"no questions", json, `{"state":"x","questions":{}}`, 400, "invalid_request"},
		{"unknown question type", json, `{"state":"x","questions":{"q":{"type":"essay","instructions":"?"}}}`, 400, "invalid_request"},
		{"malformed JSON", json, `{"state":`, 400, "invalid_request"},
		{"wrong content type", "text/plain", `{"state":"x","questions":` + valid + `}`, 400, "invalid_request"},
	}
	var failures []string
	for _, c := range cases {
		r, err := e.c.do(ctx, http.MethodPost, "/v1/systemone", c.contentType, []byte(c.body))
		if err != nil {
			return "", fmt.Errorf("%s: %w", c.name, err)
		}
		if got := errorOf(r); r.Status != c.status || got.Type != c.typ {
			failures = append(failures, fmt.Sprintf("%s: got HTTP %d %q, want %d %q", c.name, r.Status, got.Type, c.status, c.typ))
		}
	}
	if len(failures) > 0 {
		return "", errors.New(strings.Join(failures, "; "))
	}
	return fmt.Sprintf("%d cases", len(cases)), nil
}

func checkConcurrent(ctx context.Context, e *env) (string, error) {
	if err := requireTypes(e, true, false, false); err != nil {
		return "", err
	}
	const n = 8
	type outcome struct {
		choice string
		p      float64
		err    error
	}
	outs := make([]outcome, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := e.c.decide(ctx, textRequest(ticketText, routingQuestions()))
			if err != nil {
				outs[i].err = err
				return
			}
			if a, ok := d.Answers[0].Body.(*dec.ChoiceAnswer); ok && len(a.Probabilities) > 0 {
				outs[i].choice, outs[i].p = a.Choice, a.Probabilities[0].P
			}
		}()
	}
	wg.Wait()
	wall := time.Since(start)
	minP, maxP := 1.0, 0.0
	for i, o := range outs {
		switch {
		case o.err != nil:
			return "", fmt.Errorf("request %d: %w", i, o.err)
		case o.choice != outs[0].choice:
			return "", fmt.Errorf("request %d chose %q, request 0 chose %q: identical requests must agree", i, o.choice, outs[0].choice)
		}
		minP, maxP = min(minP, o.p), max(maxP, o.p)
	}
	if maxP-minP > 0.05 {
		return "", fmt.Errorf("identical requests differ in probability by %.3f", maxP-minP)
	}
	return fmt.Sprintf("%d requests in %s, all chose %s", n, wall.Round(time.Millisecond), outs[0].choice), nil
}

// requireVision skips checks that need an image-capable model.
func requireVision(e *env) error {
	if !e.caps().Vision {
		return skipf("text-only model")
	}
	return nil
}

func checkTextOnlyRejectsImages(ctx context.Context, e *env) (string, error) {
	if e.caps().Vision {
		return "", skipf("vision model")
	}
	req := textRequest(ticketText, routingQuestions())
	req.Images = []string{pngURI(solidPNG(16, 16, colorRed))}
	_, err := e.c.decide(ctx, req)
	if err := expectStatus(err, http.StatusUnprocessableEntity, "unsupported_capability"); err != nil {
		return "", err
	}
	return "image -> 422", nil
}

func checkColors(ctx context.Context, e *env) (string, error) {
	if err := requireVision(e); err != nil {
		return "", err
	}
	var got, wrong []string
	for _, c := range []struct {
		name string
		rgba color.RGBA
	}{{"red", colorRed}, {"green", colorGreen}, {"blue", colorBlue}} {
		req := textRequest("Look at the image.", colorQuestions())
		req.Images = []string{pngURI(solidPNG(448, 448, c.rgba))}
		d, err := e.c.decide(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s image: %w", c.name, err)
		}
		a, ok := d.Answers[0].Body.(*dec.ChoiceAnswer)
		if !ok {
			return "", fmt.Errorf("%s image: answer is %s, want choice", c.name, d.Answers[0].Body.Type())
		}
		got = append(got, c.name+"->"+a.Choice)
		if a.Choice != c.name {
			wrong = append(wrong, c.name+"->"+a.Choice)
		}
	}
	if len(wrong) > 0 {
		return "", fmt.Errorf("wrong color: %s", strings.Join(wrong, ", "))
	}
	return strings.Join(got, " "), nil
}

func checkBrokenImage(ctx context.Context, e *env) (string, error) {
	if err := requireVision(e); err != nil {
		return "", err
	}
	req := textRequest("Look at the image.", colorQuestions())
	req.Images = []string{dataURI("image/png", []byte("this is not an image"))}
	_, err := e.c.decide(ctx, req)
	if err := expectStatus(err, http.StatusBadRequest, "unsupported_image"); err != nil {
		return "", err
	}
	return "garbage -> 400", nil
}

func checkTooManyImages(ctx context.Context, e *env) (string, error) {
	if err := requireVision(e); err != nil {
		return "", err
	}
	limit := e.caps().MaxImages
	if limit <= 0 || limit > 16 {
		return "", skipf("max_images=%d", limit)
	}
	req := textRequest("Look at the images.", colorQuestions())
	img := pngURI(solidPNG(64, 64, colorRed))
	for i := 0; i <= limit; i++ {
		req.Images = append(req.Images, img)
	}
	_, err := e.c.decide(ctx, req)
	if err := expectStatus(err, http.StatusUnprocessableEntity, "unsupported_capability"); err != nil {
		return "", fmt.Errorf("%d images: %w", limit+1, err)
	}
	return fmt.Sprintf("%d images -> 422", limit+1), nil
}
