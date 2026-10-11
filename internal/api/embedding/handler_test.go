package embedding

import (
	dom "ai-server/internal/embedding"
	"ai-server/internal/registry"
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
	caps   registry.Capabilities
}

func (s *testService) ModelID() string                     { return "test-embedding" }
func (s *testService) Quant() string                       { return "f16" }
func (s *testService) Info() any                           { return nil }
func (s *testService) Settings() map[string]any            { return nil }
func (s *testService) Capabilities() registry.Capabilities { return s.caps }
func (s *testService) Embed(_ context.Context, value dom.Input) ([]float32, int, error) {
	input := value.Text
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
	var result struct {
		Similarities []float32 `json:"similarities"`
		Usage        struct {
			LatencyMS float64 `json:"latency_ms"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Similarities) != 3 || result.Similarities[0] != 1 || result.Similarities[1] != 0.5 || result.Similarities[2] != 0 {
		t.Fatalf("scores = %v, want [1 0.5 0]", result.Similarities)
	}
	if result.Usage.LatencyMS < 0 {
		t.Fatalf("latency_ms = %v, want non-negative", result.Usage.LatencyMS)
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

type multiTestService struct {
	testService
	purpose dom.EncodingPurpose
	vectors [][]float32
}

func (s *multiTestService) EmbedMulti(_ context.Context, input dom.Input, purpose dom.EncodingPurpose) ([][]float32, int, error) {
	s.inputs = append(s.inputs, input.Text)
	s.purpose = purpose
	return s.vectors, 2, nil
}

func multiCapabilities(t *testing.T) registry.Capabilities {
	t.Helper()
	parsed, err := registry.Parse([]byte(`version: 1
models:
  test:0.6b:
    description: Multi-vector test
    readme: readmes/test/0.6b.md
    type: embedding
    default: f16
    capabilities:
      input: [text]
      output: [multi-embedding]
    f16:
      adapter: test
      repo: test/model
      files:
        - {file: model.gguf, size: 1, sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
`))
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Models["test:0.6b"].Capabilities
}

func TestMultiEmbeddings(t *testing.T) {
	for _, purpose := range []string{"", "query", "document"} {
		svc := &multiTestService{testService: testService{caps: multiCapabilities(t)}, vectors: [][]float32{{1, 0}, {0, 1}}}
		router := chi.NewRouter()
		New(svc).Mount(router)
		request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"input":["one","two"],"multi_embedding":true,"input_type":"`+purpose+`"}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%d %s", response.Code, response.Body.String())
		}
		if len(svc.inputs) != 2 || !containsAll(response.Body.String(), `"embedding":[[1,0],[0,1]]`, `"object":"multi-embedding"`, `"total_tokens":4`) {
			t.Fatalf("lost token matrix or batch: %s", response.Body.String())
		}
		want := dom.EncodingPurpose(purpose)
		if want == "" {
			want = dom.PurposeDocument
		}
		if svc.purpose != want {
			t.Fatalf("purpose=%s want=%s", svc.purpose, want)
		}
	}
}

func TestMultiEmbeddingCapabilityGates(t *testing.T) {
	for _, declared := range []bool{false, true} {
		svc := &testService{}
		if declared {
			svc.caps = multiCapabilities(t)
		}
		router := chi.NewRouter()
		New(svc).Mount(router)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"input":"one","multi_embedding":true}`)))
		if response.Code != 422 || !contains(response.Body.String(), "unsupported_capability") || len(svc.inputs) != 0 {
			t.Fatalf("incompatible service accepted: %d %s", response.Code, response.Body.String())
		}
	}
	svc := &multiTestService{vectors: [][]float32{{1}}}
	router := chi.NewRouter()
	New(svc).Mount(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"input":"one","multi_embedding":true}`)))
	if response.Code != 422 || len(svc.inputs) != 0 {
		t.Fatal("implementation without capability accepted")
	}
}

func TestMultiEmbeddingOnlyRejectsSingleVectorOperations(t *testing.T) {
	for _, test := range []struct {
		path string
		body string
	}{
		{"/v1/embeddings", `{"input":"one"}`},
		{"/similarity", `{"input":"one","ref":["two"]}`},
	} {
		svc := &multiTestService{testService: testService{caps: multiCapabilities(t)}}
		router := chi.NewRouter()
		New(svc).Mount(router)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)))
		if response.Code != 422 || !contains(response.Body.String(), "unsupported_capability") || len(svc.inputs) != 0 {
			t.Fatalf("%s: %d %s, inference calls=%d", test.path, response.Code, response.Body.String(), len(svc.inputs))
		}
	}
}

func TestInvalidMultiEmbeddingRequestsAndResults(t *testing.T) {
	for _, test := range []struct {
		body    string
		vectors [][]float32
		status  int
	}{
		{`{"input":"one","multi_embedding":true,"input_type":"other"}`, [][]float32{{1}}, 400},
		{`{"input":"one","input_type":"query"}`, [][]float32{{1}}, 422},
		{`{"input":"one","multi_embedding":true}`, nil, 503},
		{`{"input":"one","multi_embedding":true}`, [][]float32{{1}, {1, 2}}, 503},
	} {
		svc := &multiTestService{testService: testService{caps: multiCapabilities(t)}, vectors: test.vectors}
		router := chi.NewRouter()
		New(svc).Mount(router)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(test.body)))
		if response.Code != test.status {
			t.Fatalf("%d want %d: %s", response.Code, test.status, response.Body.String())
		}
	}
}
