package selfipc

import (
	"bytes"
	"encoding/json"
	"fmt"

	"ai-server/internal/decision"
	"ai-server/internal/ipc"
	"ai-server/internal/jsonx"
)

// encodeRequest builds the systemone request frame. Questions are sent as an
// ordered array so option order survives any JSON implementation.
func encodeRequest(id uint64, req decision.Request) (ipc.Frame, error) {
	qs, err := encodeQuestions(req.Questions)
	if err != nil {
		return ipc.Frame{}, err
	}
	state := req.State
	if len(bytes.TrimSpace(state)) == 0 {
		state = json.RawMessage("null")
	}
	var images []map[string]any
	var atts [][]byte
	for i, im := range req.Images {
		if im.Format != decision.FormatRGB8 || len(im.Pixels) != im.Width*im.Height*3 {
			return ipc.Frame{}, fmt.Errorf("image %d: invalid pixel buffer", i)
		}
		m := map[string]any{"attachment": i, "width": im.Width, "height": im.Height, "format": string(im.Format)}
		if im.Name != "" {
			m["name"] = im.Name
		}
		if im.Description != "" {
			m["description"] = im.Description
		}
		images = append(images, m)
		atts = append(atts, im.Pixels)
	}
	var params jsonx.ObjectWriter
	params.Add("state", state)
	params.Add("questions", json.RawMessage(qs))
	if len(images) > 0 {
		params.Add("images", images)
	}
	pb, err := params.Bytes()
	if err != nil {
		return ipc.Frame{}, err
	}
	h, err := json.Marshal(ipc.Request{ID: id, Method: "systemone", Params: pb})
	if err != nil {
		return ipc.Frame{}, err
	}
	return ipc.Frame{Header: h, Attachments: atts}, nil
}

func encodeQuestions(qs decision.Questions) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, q := range qs {
		if i > 0 {
			buf.WriteByte(',')
		}
		var w jsonx.ObjectWriter
		w.Add("id", q.ID)
		w.Add("type", q.Body.Type())
		switch b := q.Body.(type) {
		case *decision.ChoiceQuestion:
			w.Add("instructions", b.Instructions)
			var c jsonx.ObjectWriter
			for _, o := range b.Criteria {
				if o.Description == "" {
					c.Add(o.Key, nil)
				} else {
					c.Add(o.Key, o.Description)
				}
			}
			cb, err := c.Bytes()
			if err != nil {
				return nil, err
			}
			w.Add("criteria", json.RawMessage(cb))
		case *decision.ScoreQuestion:
			w.Add("instructions", b.Instructions)
			w.Add("criteria", b.Criteria)
		case *decision.NoulQuestion:
			w.Add("instructions", b.Instructions)
		default:
			return nil, fmt.Errorf("unsupported question type %T", q.Body)
		}
		qb, err := w.Bytes()
		if err != nil {
			return nil, err
		}
		buf.Write(qb)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

type engineError struct {
	Type    string
	Message string
}

func (e *engineError) Error() string { return e.Type + ": " + e.Message }

func decodeResponse(f ipc.Frame, id uint64, qs decision.Questions) (decision.Response, error) {
	resp, err := ipc.ParseResponse(f, id)
	if err != nil {
		return decision.Response{}, err
	}
	if resp.Error != nil {
		return decision.Response{}, &engineError{Type: resp.Error.Type, Message: resp.Error.Message}
	}
	var res struct {
		Answers decision.Answers `json:"answers"`
		Usage   struct {
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			LatencyMS    float64 `json:"latency_ms"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return decision.Response{}, fmt.Errorf("malformed engine result: %w", err)
	}
	if err := decision.CheckAnswers(qs, res.Answers); err != nil {
		return decision.Response{}, err
	}
	return decision.Response{
		Answers: decision.Reorder(qs, res.Answers),
		Usage: decision.Usage{
			InputTokens:  res.Usage.InputTokens,
			OutputTokens: res.Usage.OutputTokens,
			LatencyMS:    res.Usage.LatencyMS,
		},
	}, nil
}
