package config

import (
	"io"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	s, err := ParseServe([]string{"kev:4b"}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if s.Addr() != "127.0.0.1:8080" || s.Device != "auto" || s.QueueSize != 64 || s.PreprocessConcurrency != 8 || s.Model != "kev:4b" {
		t.Fatalf("%+v", s)
	}
}

func TestPrecedence(t *testing.T) {
	e := env(map[string]string{"AI_SERVER_HOST": "0.0.0.0", "AI_SERVER_PORT": "9000", "AI_SERVER_MODELS": "/m"})
	s, err := ParseServe([]string{"kev:4b"}, e, io.Discard)
	if err != nil || s.Addr() != "0.0.0.0:9000" || s.ModelsDir != "/m" {
		t.Fatalf("env: %+v %v", s, err)
	}
	s, err = ParseServe([]string{"--port", "7000", "kev:4b", "--host", "::1", "--quant", "8bit"}, e, io.Discard)
	if err != nil || s.Addr() != "[::1]:7000" || s.Quant != "8bit" {
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
