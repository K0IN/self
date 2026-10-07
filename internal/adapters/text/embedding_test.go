package text

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	domembedding "ai-server/internal/embedding"
	dom "ai-server/internal/text"
)

func TestEmbeddingContentSerialization(t *testing.T) {
	for _, input := range []domembedding.Input{
		{Text: "legacy text"},
		{Content: []domembedding.Part{{Type: "image_url", ImageURL: &domembedding.ImageURL{URL: "data:image/png;base64,aGVsbG8="}}}},
		{Content: []domembedding.Part{{Type: "input_audio", InputAudio: &domembedding.Audio{Data: "aGVsbG8=", Format: "wav"}}}},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/embeddings" {
				t.Errorf("path=%s", request.URL.Path)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			var want []byte
			if input.Content == nil {
				want, _ = json.Marshal(input.Text)
			} else {
				want, _ = json.Marshal([]any{map[string]any{"content": input.Content}})
			}
			if string(body["input"]) != string(want) {
				t.Errorf("input=%s want=%s", body["input"], want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5,0.25]}],"usage":{"prompt_tokens":7}}`))
		}))
		adapter := &Adapter{client: server.Client(), baseURL: server.URL}
		vector, tokens, err := adapter.EmbedInput(context.Background(), input)
		server.Close()
		if err != nil || tokens != 7 || !reflect.DeepEqual(vector, []float32{0.5, 0.25}) {
			t.Fatalf("vector=%v tokens=%d err=%v", vector, tokens, err)
		}
	}
}

func TestEmbeddingArgsWithProjector(t *testing.T) {
	args := strings.Join(Args(dom.RuntimeConfig{Files: dom.ModelFiles{Model: "/model.gguf", MMProj: "/projector.gguf"}, Embedding: true, Pooling: "mean", Device: "cpu"}, 12345), " ")
	for _, flag := range []string{"--embedding", "--pooling mean", "--mmproj /projector.gguf", "--device none"} {
		if !strings.Contains(args, flag) {
			t.Errorf("missing %s in %s", flag, args)
		}
	}
}
