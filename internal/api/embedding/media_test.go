package embedding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	dom "ai-server/internal/embedding"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
	"github.com/go-chi/chi/v5"
)

func imageFixture(t *testing.T, alternate bool) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for row := 0; row < 64; row++ {
		for column := 0; column < 64; column++ {
			shade := color.RGBA{255, 20, 20, 255}
			if alternate {
				shade = color.RGBA{20, 20, 255, 255}
			}
			if column > 16 && column < 48 && row > 16 && row < 48 {
				shade = color.RGBA{20, 255, 20, 255}
			}
			img.SetRGBA(column, row, shade)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func audioFixture(frequency float64) string {
	const samples = 16000
	data := make([]byte, 44+samples*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 16000)
	binary.LittleEndian.PutUint32(data[28:], 32000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], samples*2)
	for sample := 0; sample < samples; sample++ {
		value := int16(12000 * math.Sin(2*math.Pi*frequency*float64(sample)/16000))
		binary.LittleEndian.PutUint16(data[44+sample*2:], uint16(value))
	}
	return base64.StdEncoding.EncodeToString(data)
}

func mediaInput(kind, value string) dom.Input {
	part := dom.Part{Type: kind}
	if kind == "image_url" {
		part.ImageURL = &dom.ImageURL{URL: value}
	}
	if kind == "input_audio" {
		part.InputAudio = &dom.Audio{Data: value, Format: "wav"}
	}
	return dom.Input{Content: []dom.Part{part}}
}

func TestMediaParser(t *testing.T) {
	for _, input := range []dom.Input{mediaInput("image_url", imageFixture(t, false)), mediaInput("input_audio", audioFixture(440)), mediaInput("input_audio", "data:audio/wav;base64,"+audioFixture(440))} {
		raw, _ := json.Marshal(map[string]any{"content": input.Content})
		if _, err := parseInput(raw); err != nil {
			t.Fatalf("valid %s: %v", input.Content[0].Type, err)
		}
	}
	for _, body := range []string{
		`{"content":[{"type":"input_video"}]}`,
		`null`, `""`, `{}`, `{"content":[]}`, `{"content":[{"type":"text","text":"x","extra":1}]}`,
		`{"content":[{"type":"image_url","image_url":{"url":"http://127.0.0.1/a.png"}}]}`,
		`{"content":[{"type":"image_url","image_url":{"url":"file:///tmp/a.png"}}]}`,
		`{"content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,bm90YW5pbWFnZQ=="}}]}`,
		`{"content":[{"type":"input_audio","input_audio":{"data":"abcd","format":"mp3"}}]}`,
		`{"content":[{"type":"input_audio","input_audio":{"data":"abcd","format":"wav","url":"x"}}]}`,
		`{"content":[{"type":"input_audio","input_audio":{"data":"abcd","format":"wav"}}]}`,
	} {
		if _, err := parseInput(json.RawMessage(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	mismatch := strings.Replace(imageFixture(t, false), "image/png", "image/jpeg", 1)
	raw, _ := json.Marshal(map[string]any{"content": mediaInput("image_url", mismatch).Content})
	if _, err := parseInput(raw); err == nil {
		t.Error("accepted mismatched MIME")
	}
	if _, _, err := inlineData("data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxMediaBytes)+4)); err == nil {
		t.Error("accepted oversized media")
	}
}

type mediaService struct {
	testService
	caps registry.Capabilities
	seen []dom.Input
}

func (s *mediaService) Capabilities() registry.Capabilities { return s.caps }
func (s *mediaService) Embed(_ context.Context, input dom.Input) ([]float32, int, error) {
	s.seen = append(s.seen, input)
	return []float32{1, 2}, 3, nil
}

func TestMediaHandlerCapabilitiesAndBatchAtomicValidation(t *testing.T) {
	reg, err := registry.LoadFile("../../../models/registry.yml")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve("embeddinggemma-2:740m", registry.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"image_url", "input_audio"} {
		value := audioFixture(440)
		if kind == "image_url" {
			value = imageFixture(t, false)
		}
		media := mediaInput(kind, value)
		for _, supported := range []bool{false, true} {
			svc := &mediaService{}
			if supported {
				svc.caps = resolved.Model.Capabilities
			}
			router := chi.NewRouter()
			info := New(svc).Mount(router)
			body, _ := json.Marshal(map[string]any{"input": []any{"text", map[string]any{"content": media.Content}}})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/v1/embeddings", bytes.NewReader(body)))
			if supported {
				if response.Code != 200 || len(svc.seen) != 2 {
					t.Fatalf("media request: %d %s", response.Code, response.Body.String())
				}
				metadata, _ := json.Marshal(info.Capabilities)
				if !strings.Contains(string(metadata), "vision") || !strings.Contains(string(metadata), "audio") {
					t.Fatalf("metadata: %s", metadata)
				}
				body, _ = json.Marshal(map[string]any{"input": map[string]any{"content": media.Content}, "ref": []any{"text", map[string]any{"content": media.Content}}})
				response = httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("POST", "/similarity", bytes.NewReader(body)))
				if response.Code != 200 {
					t.Fatalf("similarity: %s", response.Body.String())
				}
			} else if response.Code != 422 || len(svc.seen) != 0 {
				t.Fatalf("unsupported media inferred: %d calls=%d", response.Code, len(svc.seen))
			}
		}
	}
}

func TestRemoteEmbeddingImages(t *testing.T) {
	inline := imageFixture(t, false)
	imageBytes, _, err := inlineData(inline)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		switch r.URL.Path {
		case "/bad":
			_, _ = w.Write([]byte("not an image"))
		case "/large":
			w.Header().Set("Content-Length", "10485761")
		default:
			_, _ = w.Write(imageBytes)
		}
	}))
	defer server.Close()
	reg, err := registry.LoadFile("../../../models/registry.yml")
	if err != nil {
		t.Fatal(err)
	}
	model, err := reg.Resolve("embeddinggemma-2:740m", registry.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/v1/embeddings", "/similarity"} {
		for _, scenario := range []struct {
			name                    string
			path                    string
			allowHTTP, allowPrivate bool
			wantOK                  bool
		}{
			{"allowed", "/ok", true, true, true},
			{"http blocked", "/ok", false, true, false},
			{"private blocked", "/ok", true, false, false},
			{"malformed", "/bad", true, true, false},
			{"oversized", "/large", true, true, false},
		} {
			t.Run(route+"/"+scenario.name, func(t *testing.T) {
				limits := imageutil.DefaultLimits()
				limits.MaxSourceBytes = maxMediaBytes
				limits.AllowHTTP, limits.AllowPrivate = scenario.allowHTTP, scenario.allowPrivate
				svc := &mediaService{caps: model.Model.Capabilities}
				router := chi.NewRouter()
				NewWithFetcher(svc, imageutil.NewFetcher(limits)).Mount(router)
				media := map[string]any{"content": mediaInput("image_url", server.URL+scenario.path).Content}
				payload := map[string]any{"input": []any{media}}
				if route == "/similarity" {
					payload = map[string]any{"input": media, "ref": []any{"reference"}}
				}
				body, _ := json.Marshal(payload)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("POST", route, bytes.NewReader(body)))
				if scenario.wantOK {
					if response.Code != 200 || len(svc.seen) == 0 || svc.seen[0].Content[0].ImageURL.URL != inline {
						t.Fatalf("remote image not normalized: status=%d body=%s inputs=%v", response.Code, response.Body.String(), svc.seen)
					}
				} else if response.Code == 200 || len(svc.seen) != 0 {
					t.Fatalf("invalid remote image inferred: status=%d calls=%d", response.Code, len(svc.seen))
				}
			})
		}
	}
}

func TestRealEmbeddingMedia(t *testing.T) {
	endpoint := os.Getenv("SELF_EMBEDDING_TEST_URL")
	if endpoint == "" {
		t.Skip("set SELF_EMBEDDING_TEST_URL to a running EmbeddingGemma 2 self server")
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	inputs := []any{"A colorful square.", map[string]any{"content": mediaInput("image_url", imageFixture(t, false)).Content}, map[string]any{"content": mediaInput("image_url", imageFixture(t, true)).Content}, map[string]any{"content": mediaInput("input_audio", audioFixture(440)).Content}, map[string]any{"content": mediaInput("input_audio", "data:audio/wav;base64,"+audioFixture(880)).Content}}
	var vectors [][]float64
	for index, input := range inputs {
		value := input
		if index != 0 {
			value = []any{input}
		}
		body, _ := json.Marshal(map[string]any{"input": value})
		started := time.Now()
		response, err := client.Post(endpoint+"/v1/embeddings", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
			Error any `json:"error"`
		}
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || len(result.Data) != 1 {
			t.Fatalf("input %d status=%d error=%v decode=%v", index, response.StatusCode, result.Error, err)
		}
		vector := result.Data[0].Embedding
		if len(vector) != 768 {
			t.Fatalf("input %d dimensions=%d", index, len(vector))
		}
		norm := 0.0
		for _, value := range vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatal("nonfinite vector")
			}
			norm += value * value
		}
		if norm == 0 {
			t.Fatal("zero vector")
		}
		vectors = append(vectors, vector)
		t.Logf("input=%d dimensions=%d norm=%.6f elapsed=%s", index, len(vector), math.Sqrt(norm), time.Since(started))
	}
	for _, pair := range [][2]int{{0, 1}, {1, 2}, {0, 3}, {3, 4}} {
		distance := 0.0
		for index, value := range vectors[pair[0]] {
			delta := value - vectors[pair[1]][index]
			distance += delta * delta
		}
		if distance < 1e-10 {
			t.Fatal(fmt.Sprintf("media did not affect vector: %v", pair))
		}
		t.Logf("pair=%v L2=%.6f", pair, math.Sqrt(distance))
	}
	for _, route := range []string{"/v1/embeddings", "/similarity"} {
		payload := map[string]any{"input": inputs}
		if route == "/similarity" {
			payload = map[string]any{"input": inputs[1], "ref": inputs}
		}
		body, _ := json.Marshal(payload)
		response, err := client.Post(endpoint+route, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Data         []json.RawMessage `json:"data"`
			Similarities []float64         `json:"similarities"`
			Error        any               `json:"error"`
		}
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("%s status=%d error=%v decode=%v", route, response.StatusCode, result.Error, err)
		}
		if route == "/v1/embeddings" && len(result.Data) != len(inputs) {
			t.Fatalf("batch length=%d", len(result.Data))
		}
		if route == "/similarity" && (len(result.Similarities) != len(inputs) || math.Abs(result.Similarities[1]-1) > 1e-6) {
			t.Fatalf("similarities=%v", result.Similarities)
		}
		t.Logf("%s mixed media success similarities=%v", route, result.Similarities)
	}
}
