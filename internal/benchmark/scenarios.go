package benchmark

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"ai-server/internal/decision"
)

// Scenario names. They are part of the report, so renaming one is a change of
// Version.
const (
	ShortChoice   = "short-choice"   // one choice question, a short text
	MultiQuestion = "multi-question" // choice, noul and score in one request
	MaxOptions    = "max-options"    // a choice with the most options the model supports
	LongContext   = "long-context"   // a long text, half of the usable context
	Vision        = "vision"         // one image and a choice (vision models)
)

// Scenario is one repeatable request.
type Scenario struct {
	Name     string
	Request  decision.Request
	Requests int // timed requests
}

// Inputs is what the scenarios depend on: what the model can do and how big
// its context is.
type Inputs struct {
	Caps decision.Capabilities
	// ContextTokens is the prompt size of the long-context scenario, see
	// LongContextTokens.
	ContextTokens int
	// Image is a prepared image for vision models, nil for text-only ones.
	Image      *decision.Image
	Iterations int
}

const (
	ticketText  = "My credit card was charged twice for the same order, please refund one payment."
	billingKey  = "billing"
	deptID      = "dept"
	maxLongToks = 2048
)

var routing = []decision.ChoiceOption{
	{Key: billingKey, Description: "Payments and refunds"},
	{Key: "technical", Description: "Bugs and crashes"},
	{Key: "shipping", Description: "Delivery problems"},
}

var decoyTopics = []string{
	"Gardening tips", "Astronomy trivia", "Cooking recipes", "Sports scores", "Travel planning",
	"Music theory", "Chess strategy", "Weather reports", "Poetry writing", "Car maintenance",
	"Bird watching", "Knitting patterns", "Ancient history", "Board games", "Photography tips",
}

var fillerSentences = []string{
	"Thanks for getting back to me about the delivery window.",
	"I checked the tracking page again this morning and it still shows the parcel at the regional depot.",
	"The app asked me to update before I could open my order history, and the update itself was slow.",
	"Last month I changed my address, so please make sure the new one is on file.",
	"My colleague ordered the same item and hers arrived in two days without any trouble.",
	"The confirmation email listed three items, but only two of them were on the invoice.",
	"I would also like to know whether the warranty covers accidental damage during shipping.",
	"Please note that I am travelling next week and will only be reachable by email.",
}

// LongContextTokens picks the prompt size of the long-context scenario: half
// of the usable context, at most 2048 tokens, so the run stays short and never
// overflows the window. contextSize is the context_size setting, 0 if unset.
func LongContextTokens(contextLength int, contextSize int64) int {
	limit := contextLength
	if contextSize > 0 && (limit <= 0 || int(contextSize) < limit) {
		limit = int(contextSize)
	}
	if limit <= 0 {
		limit = 1024
	}
	return min(max(limit/2, 64), maxLongToks)
}

// Scenarios lists the requests that apply to a model. It fails when the model
// answers none of the question types used here.
func Scenarios(in Inputs) ([]Scenario, error) {
	c := in.Caps
	iters := max(in.Iterations, 1)
	var out []Scenario
	add := func(name string, req decision.Request, n int) {
		out = append(out, Scenario{Name: name, Request: req, Requests: n})
	}
	if c.Choice {
		add(ShortChoice, request(ticketText, routingQuestion(routing)), iters)
	}
	if c.Choice && c.Score && c.Noul {
		add(MultiQuestion, request(ticketText, multiQuestions()), iters)
	}
	if c.Choice && c.MaxOptions > 0 {
		add(MaxOptions, request(ticketText, routingQuestion(wideOptions(c.MaxOptions))), iters)
	}
	if c.Choice {
		add(LongContext, request(longTicket(in.ContextTokens), routingQuestion(routing)), max(3, iters/4))
	}
	if c.Choice && c.Vision && in.Image != nil {
		req := request("Look at the image.", colorQuestion())
		req.Images = []decision.Image{*in.Image}
		add(Vision, req, max(5, iters/2))
	}
	if len(out) == 0 {
		return nil, errors.New("the model answers no choice questions, which every scenario needs")
	}
	return out, nil
}

func request(state string, qs decision.Questions) decision.Request {
	raw, _ := json.Marshal(state)
	return decision.Request{State: raw, Questions: qs}
}

func routingQuestion(opts []decision.ChoiceOption) decision.Questions {
	return decision.Questions{{ID: deptID, Body: &decision.ChoiceQuestion{Instructions: "Which department should handle this?", Criteria: opts}}}
}

func multiQuestions() decision.Questions {
	return decision.Questions{
		{ID: deptID, Body: &decision.ChoiceQuestion{Instructions: "Which department should handle this?", Criteria: routing}},
		{ID: "refund", Body: &decision.NoulQuestion{Instructions: "Does the text ask for a refund?"}},
		{ID: "urgency", Body: &decision.ScoreQuestion{Instructions: "How urgent is this?", Criteria: []string{"low", "medium", "high"}}},
	}
}

func colorQuestion() decision.Questions {
	return decision.Questions{{ID: "color", Body: &decision.ChoiceQuestion{
		Instructions: "What is the main color of the image?",
		Criteria: []decision.ChoiceOption{
			{Key: "red", Description: "The image is mostly red"},
			{Key: "green", Description: "The image is mostly green"},
			{Key: "blue", Description: "The image is mostly blue"},
		},
	}}}
}

// wideOptions returns n options with the right answer last, so the engine
// reads out its highest letter.
func wideOptions(n int) []decision.ChoiceOption {
	n = max(n, 2)
	opts := make([]decision.ChoiceOption, 0, n)
	for i := 0; i < n-1; i++ {
		opts = append(opts, decision.ChoiceOption{Key: fmt.Sprintf("opt_%02d", i+1), Description: decoyTopics[i%len(decoyTopics)]})
	}
	return append(opts, decision.ChoiceOption{Key: billingKey, Description: "Payments and refunds"})
}

// longTicket builds a support message of roughly the requested token count
// (about four characters per token) that ends with the billing complaint.
func longTicket(tokens int) string {
	var b strings.Builder
	for i := 0; b.Len() < tokens*4; i++ {
		b.WriteString(fillerSentences[i%len(fillerSentences)])
		b.WriteByte(' ')
	}
	b.WriteString(ticketText)
	return b.String()
}

// ImageDataURI is the image of the vision scenario: a 448x448 gradient, as a
// PNG data URI ready for the image pipeline.
func ImageDataURI() string {
	const w, h = 448, 448
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x + y) * 255 / (w + h)), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory RGBA image cannot fail
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
