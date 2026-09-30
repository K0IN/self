package config

import (
	"flag"
	"io"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestParseServeWithExtraFlags(t *testing.T) {
	var iterations int
	var out string
	extra := func(fs *flag.FlagSet) {
		fs.IntVar(&iterations, "iterations", 20, "")
		fs.StringVar(&out, "out", "", "")
	}
	s, err := ParseServeWith([]string{"--iterations", "5", "kev:0.5b@q8", "--out", "/tmp/r", "--device", "cpu"}, env(nil), io.Discard, extra)
	if err != nil || iterations != 5 || out != "/tmp/r" || s.Model != "kev:0.5b" || s.Quant != "q8" || s.Device != "cpu" {
		t.Fatalf("%+v iterations=%d out=%q %v", s, iterations, out, err)
	}
	// The extra flags exist only for commands that register them.
	if _, err := ParseServe([]string{"kev:0.5b", "--iterations", "5"}, env(nil), io.Discard); err == nil {
		t.Fatal("serve accepted --iterations")
	}
}

func TestDefaults(t *testing.T) {
	s, err := ParseServe([]string{"kev:4b"}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if s.Addr() != "127.0.0.1:8080" || s.Device != "auto" || s.QueueSize != 64 || s.PreprocessConcurrency != 8 || s.Model != "kev:4b" {
		t.Fatalf("%+v", s)
	}
}

func TestModelReferenceQuant(t *testing.T) {
	s, err := ParseServe([]string{"kev:4b@q8"}, env(nil), io.Discard)
	if err != nil || s.Model != "kev:4b" || s.Quant != "q8" {
		t.Fatalf("explicit quant: %+v %v", s, err)
	}
	if _, err := ParseServe([]string{"kev:4b@q8", "--quant", "fp16"}, env(nil), io.Discard); err == nil {
		t.Fatal("accepted quant in both model reference and --quant")
	}
	for _, ref := range []string{"kev:4b@", "@q8", "kev:4b@q8@fp16"} {
		if _, err := ParseServe([]string{ref}, env(nil), io.Discard); err == nil {
			t.Fatalf("accepted invalid model reference %q", ref)
		}
	}
}

func TestPrecedence(t *testing.T) {
	e := env(map[string]string{"AI_SERVER_HOST": "0.0.0.0", "AI_SERVER_PORT": "9000", "AI_SERVER_MODELS": "/m"})
	s, err := ParseServe([]string{"kev:4b"}, e, io.Discard)
	if err != nil || s.Addr() != "0.0.0.0:9000" || s.ModelsDir != "/m" {
		t.Fatalf("env: %+v %v", s, err)
	}
	s, err = ParseServe([]string{"--port", "7000", "kev:4b", "--host", "::1", "--quant", "q8"}, e, io.Discard)
	if err != nil || s.Addr() != "[::1]:7000" || s.Quant != "q8" {
		t.Fatalf("cli: %+v %v", s, err)
	}
}

func TestSetOverrides(t *testing.T) {
	s, err := ParseServe([]string{"kev:4b", "--set", "context_size=4096", "--set", "threads=8"}, env(nil), io.Discard)
	if err != nil || len(s.Set) != 2 || s.Set[0] != "context_size=4096" {
		t.Fatalf("%+v %v", s.Set, err)
	}
	if _, err := ParseServe([]string{"kev:4b", "--set", "novalue"}, env(nil), io.Discard); err == nil {
		t.Fatal("--set without = accepted")
	}
}

func TestInvalid(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"a", "b"},
		{"kev:4b", "--port", "0"},
		{"kev:4b", "--device", "tpu"},
		{"kev:4b", "--queue-size", "0"},
		{"kev:4b", "--nope"},
	} {
		if _, err := ParseServe(args, env(nil), io.Discard); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, err := ParseServe([]string{"x"}, env(map[string]string{"AI_SERVER_PORT": "abc"}), io.Discard); err == nil {
		t.Error("bad env port accepted")
	}
}
