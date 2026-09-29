package decision

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseQs(t *testing.T, s string) Questions {
	t.Helper()
	var qs Questions
	if err := json.Unmarshal([]byte(s), &qs); err != nil {
		t.Fatal(err)
	}
	return qs
}

func TestParseQuestions(t *testing.T) {
	qs := parseQs(t, `{
		"dept": {"type":"choice","instructions":"Which?","criteria":{"billing":"Payments","technical":null}},
		"urgency": {"type":"score","instructions":"How urgent?","criteria":["low","mid","high"]},
		"refund": {"type":"noul","instructions":"Refund?"},
		"list": {"type":"choice","instructions":"Pick","criteria":["a","b"]}
	}`)
	if len(qs) != 4 || qs[0].ID != "dept" || qs[3].ID != "list" {
		t.Fatalf("order not preserved: %+v", qs)
	}
	c := qs[0].Body.(*ChoiceQuestion)
	if len(c.Criteria) != 2 || c.Criteria[0] != (ChoiceOption{"billing", "Payments"}) || c.Criteria[1].Key != "technical" {
		t.Fatalf("choice = %+v", c)
	}
	s := qs[1].Body.(*ScoreQuestion)
	if len(s.Criteria) != 3 || OptionCount(s) != 3 {
		t.Fatalf("score = %+v", s)
	}
	if _, ok := qs[2].Body.(*NoulQuestion); !ok || OptionCount(qs[2].Body) != 2 {
		t.Fatal("noul")
	}
	if qs[3].Body.(*ChoiceQuestion).Criteria[1].Key != "b" {
		t.Fatal("list criteria")
	}
}

func TestQuestionErrors(t *testing.T) {
	cases := map[string]string{
		"unknown type":      `{"q":{"type":"essay","instructions":"x"}}`,
		"missing type":      `{"q":{"instructions":"x"}}`,
		"empty instr":       `{"q":{"type":"noul","instructions":" "}}`,
		"choice no crit":    `{"q":{"type":"choice","instructions":"x"}}`,
		"choice empty":      `{"q":{"type":"choice","instructions":"x","criteria":{}}}`,
		"choice bad value":  `{"q":{"type":"choice","instructions":"x","criteria":{"a":1}}}`,
		"choice dup list":   `{"q":{"type":"choice","instructions":"x","criteria":["a","a"]}}`,
		"choice dup object": `{"q":{"type":"choice","instructions":"x","criteria":{"a":"","a":""}}}`,
		"score object":      `{"q":{"type":"score","instructions":"x","criteria":{"a":"b"}}}`,
		"score empty":       `{"q":{"type":"score","instructions":"x","criteria":[]}}`,
		"noul criteria":     `{"q":{"type":"noul","instructions":"x","criteria":["a"]}}`,
		"unknown field":     `{"q":{"type":"noul","instructions":"x","bogus":1}}`,
		"not object":        `["q"]`,
		"dup question":      `{"q":{"type":"noul","instructions":"x"},"q":{"type":"noul","instructions":"y"}}`,
	}
	for name, doc := range cases {
		var qs Questions
		if err := json.Unmarshal([]byte(doc), &qs); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestQuestionsRoundTrip(t *testing.T) {
	in := `{"b":{"type":"choice","instructions":"Pick","criteria":{"z":"last","a":"first"}},"a":{"type":"score","instructions":"Rate","criteria":["lo","hi"]},"c":{"type":"noul","instructions":"Yes?"}}`
	qs := parseQs(t, in)
	out, err := json.Marshal(qs)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("round trip:\n got %s\nwant %s", out, in)
	}
}

func TestAnswersMarshalUnmarshal(t *testing.T) {
	as := Answers{
		{ID: "action", Body: &ChoiceAnswer{Choice: "stop", Probabilities: Probabilities{{"go", 0.1}, {"stop", 0.9}}, Confidence: 0.5}},
		{ID: "urgency", Body: &ScoreAnswer{Score: 1.2, Probabilities: Probabilities{{"0", 0.2}, {"1", 0.4}, {"2", 0.4}}, Confidence: 0.1}},
		{ID: "danger", Body: &NoulAnswer{Noul: 0.93, Confidence: 0.88}},
	}
	b, err := json.Marshal(as)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"action":{"type":"choice","choice":"stop","probabilities":{"go":0.1,"stop":0.9},"confidence":0.5},"urgency":{"type":"score","score":1.2,"probabilities":{"0":0.2,"1":0.4,"2":0.4},"confidence":0.1},"danger":{"type":"noul","noul":0.93,"confidence":0.88}}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
	var back Answers
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 || back[0].Body.(*ChoiceAnswer).Probabilities[1].Key != "stop" || back[2].Body.(*NoulAnswer).Noul != 0.93 {
		t.Fatalf("back = %+v", back)
	}
	if _, err := UnmarshalAnswer([]byte(`{"type":"essay"}`)); err == nil {
		t.Fatal("unknown answer type accepted")
	}
}

func TestCheckAnswers(t *testing.T) {
	qs := parseQs(t, `{"a":{"type":"choice","instructions":"x","criteria":["go","stop"]},"b":{"type":"noul","instructions":"y"}}`)
	good := Answers{
		{ID: "b", Body: &NoulAnswer{Noul: 0.5}},
		{ID: "a", Body: &ChoiceAnswer{Choice: "go", Probabilities: Probabilities{{"go", 1}}}},
	}
	if err := CheckAnswers(qs, good); err != nil {
		t.Fatal(err)
	}
	if r := Reorder(qs, good); r[0].ID != "a" {
		t.Fatal("reorder")
	}
	bad := []Answers{
		good[:1],
		{{ID: "a", Body: &NoulAnswer{}}, {ID: "b", Body: &NoulAnswer{}}},
		{{ID: "a", Body: &ChoiceAnswer{Choice: "left"}}, {ID: "b", Body: &NoulAnswer{}}},
		{{ID: "x", Body: &NoulAnswer{}}, {ID: "b", Body: &NoulAnswer{}}},
	}
	for i, as := range bad {
		if err := CheckAnswers(qs, as); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestCapabilitiesCheck(t *testing.T) {
	qs := parseQs(t, `{"a":{"type":"score","instructions":"x","criteria":["1","2","3"]}}`)
	c := Capabilities{Text: true, Choice: true, Noul: true}
	if err := c.CheckQuestions(qs); err == nil || !strings.Contains(err.Error(), "score") {
		t.Fatalf("got %v", err)
	}
	c.Score, c.MaxOptions = true, 2
	if err := c.CheckQuestions(qs); err == nil || !strings.Contains(err.Error(), "at most 2") {
		t.Fatalf("got %v", err)
	}
	i := Capabilities{Vision: true, MultiImage: false, MaxImages: 4}.Intersect(Capabilities{Vision: true, MultiImage: true, MaxImages: 8})
	if i.MaxImages != 1 {
		t.Fatalf("intersect max images = %d", i.MaxImages)
	}
}
