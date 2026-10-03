package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type testService struct {
	inputs []string
}

func (s *testService) ModelID() string          { return "test-embedding" }
func (s *testService) Quant() string            { return "f16" }
func (s *testService) Info() any                { return nil }
func (s *testService) Settings() map[string]any { return nil }
func (s *testService) Embed(_ context.Context, input string) ([]float32, int, error) {
	s.inputs = append(s.inputs, input)
	vectors := map[string][]float32{
		"reference": {1, 0},
		"same":      {1, 0},
		"different": {0, 1},
		"opposite":  {-1, 0},
	}
	if vector, ok := vectors[input]; ok {
		return vector, len(input), nil
	}
	return []float32{float32(len(input)), 0}, len(input), nil
}

func TestEmbeddingsAcceptScalarAndBatchInput(t *testing.T) {
	svc := &testService{}
	r := chi.NewRouter()
	New(svc).Mount(r)

	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"ignored","input":["one","two"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := len(svc.inputs); got != 2 || svc.inputs[0] != "one" || svc.inputs[1] != "two" {
		t.Fatalf("inputs = %#v", svc.inputs)
	}
	if body := response.Body.String(); !containsAll(body, `"object":"list"`, `"index":0`, `"index":1`, `"total_tokens":6`) {
		t.Fatalf("response = %s", body)
	}
}

func TestSimilarityReturnsCosineScoresInReferenceOrder(t *testing.T) {
	svc := &testService{}
	r := chi.NewRouter()
	New(svc).Mount(r)

	request := httptest.NewRequest(http.MethodPost, "/similarity", strings.NewReader(`{"input":"reference","ref":["same","different","opposite"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var scores []float32
	if err := json.Unmarshal(response.Body.Bytes(), &scores); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(scores) != 3 || scores[0] != 1 || scores[1] != 0 || scores[2] != -1 {
		t.Fatalf("scores = %v, want [1 0 -1]", scores)
	}
	if got := strings.Join(svc.inputs, ","); got != "reference,same,different,opposite" {
		t.Fatalf("embed order = %q", got)
	}
}

func TestSimilarityRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing input", `{"ref":["x"]}`},
		{"missing ref", `{"input":"x"}`},
		{"empty ref", `{"input":"x","ref":[]}`},
		{"unknown field", `{"input":"x","ref":["y"],"extra":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			New(&testService{}).Mount(r)
			request := httptest.NewRequest(http.MethodPost, "/similarity", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !contains(value, needle) {
			return false
		}
	}
	return true
}

func contains(value, needle string) bool {
	return strings.Contains(value, needle)
}
