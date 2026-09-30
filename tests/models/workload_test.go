package models_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// kv is one member of an ordered JSON object. Question and criteria order
// is part of the API, so these bodies are built by hand instead of maps.
type kv struct {
	K string
	V json.RawMessage
}

func object(members ...kv) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.K)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.V)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func str(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func strList(items ...string) json.RawMessage {
	b, _ := json.Marshal(items)
	return b
}

// option is one choice criterion.
type option struct{ Key, Desc string }

func choiceQ(instructions string, opts []option) json.RawMessage {
	criteria := make([]kv, len(opts))
	for i, o := range opts {
		criteria[i] = kv{o.Key, str(o.Desc)}
	}
	return object(
		kv{"type", str("choice")},
		kv{"instructions", str(instructions)},
		kv{"criteria", object(criteria...)},
	)
}

func noulQ(instructions string) json.RawMessage {
	return object(kv{"type", str("noul")}, kv{"instructions", str(instructions)})
}

func scoreQ(instructions string, levels ...string) json.RawMessage {
	return object(
		kv{"type", str("score")},
		kv{"instructions", str(instructions)},
		kv{"criteria", strList(levels...)},
	)
}

const (
	ticketText     = "My credit card was charged twice for the same order, please refund one payment."
	billingKey     = "billing"
	deptQuestionID = "dept"
)

var routingOptions = []option{
	{billingKey, "Payments and refunds"},
	{"technical", "Bugs and crashes"},
	{"shipping", "Delivery problems"},
}

func routingQuestions() json.RawMessage {
	return object(kv{deptQuestionID, choiceQ("Which department should handle this?", routingOptions)})
}

var decoyTopics = []string{
	"Gardening tips", "Astronomy trivia", "Cooking recipes", "Sports scores", "Travel planning",
	"Music theory", "Chess strategy", "Weather reports", "Poetry writing", "Car maintenance",
	"Bird watching", "Knitting patterns", "Ancient history", "Board games", "Photography tips",
}

// wideOptions returns n options with the correct routing answer last, so a
// letter readout that is off by one at the limit shows up as a wrong answer.
func wideOptions(n int) []option {
	if n < 2 {
		n = 2
	}
	opts := make([]option, 0, n)
	for i := 0; i < n-1; i++ {
		topic := decoyTopics[i%len(decoyTopics)]
		opts = append(opts, option{fmt.Sprintf("opt_%02d", i+1), topic})
	}
	return append(opts, option{billingKey, "Payments and refunds"})
}

func wideQuestions(n int) json.RawMessage {
	return object(kv{deptQuestionID, choiceQ("Which department should handle this?", wideOptions(n))})
}

func multiQuestions() json.RawMessage {
	return object(
		kv{deptQuestionID, choiceQ("Which department should handle this?", routingOptions)},
		kv{"refund", noulQ("Does the text ask for a refund?")},
		kv{"urgency", scoreQ("How urgent is this?", "low", "medium", "high")},
	)
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

func solidPNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return encodePNG(img)
}

// gradientPNG is a non-trivial image so resize and encode paths do real work.
func gradientPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x + y) * 255 / (w + h)), 255})
		}
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err) // encoding an in-memory RGBA image cannot fail
	}
	return b.Bytes()
}

func dataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func pngURI(data []byte) string { return dataURI("image/png", data) }

var (
	colorRed   = color.RGBA{220, 30, 30, 255}
	colorGreen = color.RGBA{30, 180, 30, 255}
	colorBlue  = color.RGBA{30, 30, 220, 255}
)

var colorOptions = []option{
	{"red", "The image is mostly red"},
	{"green", "The image is mostly green"},
	{"blue", "The image is mostly blue"},
}

func colorQuestions() json.RawMessage {
	return object(kv{"color", choiceQ("What is the main color of the image?", colorOptions)})
}
