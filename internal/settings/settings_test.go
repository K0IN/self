package settings

import (
	"reflect"
	"strings"
	"testing"
)

var schema = Schema{
	{Name: "context_size", Flag: "--ctx", Kind: Int, Min: 512, Max: 262144},
	{Name: "temperature", Flag: "--temperature", Kind: Float, Min: 0.01, Max: 100},
	{Name: "cuda_graph", Flag: "--cuda-graph", Kind: Bool},
	{Name: "flash_attn", Flag: "--flash-attn", Kind: String, Enum: []string{"auto", "on", "off"}},
}

func TestValidateAndArgs(t *testing.T) {
	v, err := schema.Validate(map[string]any{"context_size": 8192, "temperature": "1.5", "cuda_graph": "on", "flash_attn": "off"})
	if err != nil {
		t.Fatal(err)
	}
	if v["context_size"] != int64(8192) || v["temperature"] != 1.5 || v["cuda_graph"] != true {
		t.Fatalf("values = %#v", v)
	}
	want := []string{"--ctx", "8192", "--temperature", "1.5", "--cuda-graph", "--flash-attn", "off"}
	if got := schema.Args(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v", got)
	}
	v, _ = schema.Validate(map[string]any{"cuda_graph": false})
	if got := schema.Args(v); len(got) != 0 {
		t.Fatalf("false bool must not emit a flag: %v", got)
	}
}

func TestValidateRejects(t *testing.T) {
	for name, in := range map[string]map[string]any{
		"unknown":     {"nope": 1},
		"too small":   {"context_size": 100},
		"not int":     {"context_size": 1.5},
		"bad string":  {"context_size": "big"},
		"enum":        {"flash_attn": "maybe"},
		"bool":        {"cuda_graph": "perhaps"},
		"wrong type":  {"flash_attn": 3},
		"float range": {"temperature": 0},
	} {
		if _, err := schema.Validate(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	_, err := schema.Validate(map[string]any{"nope": 1})
	if !strings.Contains(err.Error(), "known: context_size") {
		t.Errorf("error should list known settings: %v", err)
	}
}

func TestOverridesMergeFormat(t *testing.T) {
	o, err := ParseOverrides([]string{"context_size=4096", "flash_attn=on"})
	if err != nil {
		t.Fatal(err)
	}
	m := Merge(map[string]any{"context_size": 8192, "temperature": 1.0}, o)
	v, err := schema.Validate(m)
	if err != nil {
		t.Fatal(err)
	}
	if v["context_size"] != int64(4096) {
		t.Fatalf("override lost: %v", v)
	}
	if got := Format(v); got != "context_size=4096 flash_attn=on temperature=1" {
		t.Fatalf("format = %q", got)
	}
	if _, err := ParseOverrides([]string{"novalue"}); err == nil {
		t.Fatal("expected error")
	}
}
