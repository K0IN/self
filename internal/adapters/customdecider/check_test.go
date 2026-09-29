package customdecider

import (
	"strings"
	"testing"

	"ai-server/internal/decision"
	"ai-server/internal/ggufmeta"
)

func TestCheckModel(t *testing.T) {
	ok := ggufmeta.Metadata{"general.architecture": "qwen35", "tokenizer.ggml.tokens": []string{"a"}}
	if err := CheckModel(ok, ""); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		md   ggufmeta.Metadata
		want string
	}{
		{ggufmeta.Metadata{"general.architecture": "ggmlc", "tokenizer.ggml.tokens": []string{"a"}}, "ggmlc-laya"},
		{ggufmeta.Metadata{"general.architecture": "clip"}, "role: mmproj"},
		{ggufmeta.Metadata{"general.architecture": "qwen35"}, "tokenizer"},
		{ggufmeta.Metadata{}, "general.architecture"},
	}
	for _, c := range cases {
		err := CheckModel(c.md, "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("want error containing %q, got %v", c.want, err)
		}
	}
}

func TestArgs(t *testing.T) {
	got := strings.Join(Spec.Args(decision.RuntimeConfig{
		Device: "cuda:0",
		Files:  decision.ModelFiles{Model: "/m/model.gguf", MMProj: "/m/mmproj.gguf"},
	}), " ")
	want := "--model /m/model.gguf --device cuda:0 --mmproj /m/mmproj.gguf"
	if got != want {
		t.Fatalf("args = %q", got)
	}
}
