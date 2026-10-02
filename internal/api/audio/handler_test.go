package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dom "ai-server/internal/audio"
	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	request dom.Request
	calls   int
}

func (s *fakeService) ModelID() string          { return "test-tts" }
func (s *fakeService) Quant() string            { return "q4" }
func (s *fakeService) Info() any                { return map[string]any{"family": "test"} }
func (s *fakeService) Settings() map[string]any { return nil }
func (s *fakeService) Synthesize(_ context.Context, request dom.Request) (dom.Response, error) {
	s.request = request
	s.calls++
	return dom.Response{WAV: []byte("RIFF")}, nil
}

func TestDecodeAudioRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewBufferString(`{"input":"hello","voice":{"audio":"YXVkaW8=","format":"mp3"}}`))
	req.Header.Set("Content-Type", "application/json")
	decoded, err := decode(req, httptest.NewRecorder())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Speed != 1 || decoded.ResponseFormat != "wav" {
		t.Fatalf("defaults = speed %v, format %q", decoded.Speed, decoded.ResponseFormat)
	}
	voice, err := parseVoice(decoded.Voice, decoded.RefAudio, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(voice.Audio) != "audio" || voice.Format != "mp3" {
		t.Fatalf("voice = %#v", voice)
	}
}

func TestDecodeAudioBinary(t *testing.T) {
	// Real audio encodes to base64 containing '/' and '+'.
	raw := []byte{0xff, 0xfb, 0xfe, 0x3f, 0xbf, 0x00}
	enc := base64.StdEncoding.EncodeToString(raw)
	if !strings.ContainsAny(enc, "/+") {
		t.Fatalf("fixture %q does not exercise '/' or '+'", enc)
	}
	for _, in := range []string{enc, "data:audio/mpeg;base64," + enc} {
		got, _, err := decodeAudio(in, "mp3")
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("decodeAudio(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"/etc/passwd", "./voice.mp3", "~/voice.mp3", "not base64!"} {
		if _, _, err := decodeAudio(in, ""); err == nil {
			t.Fatalf("decodeAudio(%q) accepted", in)
		}
	}
}

func TestDecodeAcceptsOpenAIRequestFields(t *testing.T) {
	for _, body := range []string{
		`{"input":"hello","model":"tts-1","voice":{"id":"voice_123"},"response_format":"mp3","stream_format":"audio","speed":1,"instructions":"warm"}`,
		`{"input":"hello","model":"gpt-4o-mini-tts","voice":"alloy","response_format":"opus","stream_format":"sse"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewBufferString(body))
		if _, err := decode(req, httptest.NewRecorder()); err != nil {
			t.Fatalf("decode rejected OpenAI fields: %v", err)
		}
	}
}

func TestDecodeRejectsUnsupportedControls(t *testing.T) {
	for name, body := range map[string]string{
		"instructions": `{"input":"hello","instructions":"cheerful"}`,
		"speed":        `{"input":"hello","speed":1.25}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewBufferString(body))
			service := &fakeService{}
			router := chi.NewRouter()
			New(service).Mount(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusUnprocessableEntity || service.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
			}
		})
	}
}

func TestSpeechReturnsWAV(t *testing.T) {
	service := &fakeService{}
	router := chi.NewRouter()
	New(service).Mount(router)
	body, _ := json.Marshal(map[string]any{"model": "test-tts", "input": "hello", "voice": map[string]string{"audio": "YXVkaW8=", "format": "mp3"}})
	request := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "audio/wav" {
		t.Fatalf("content type = %q", got)
	}
	if string(response.Body.Bytes()) != "RIFF" || string(service.request.Voice.Audio) != "audio" {
		t.Fatalf("response/request = %q, %#v", response.Body.Bytes(), service.request)
	}
}

func TestSpeechRejectsBeforeSynthesis(t *testing.T) {
	for name, test := range map[string]struct {
		body   string
		status int
	}{
		"format":   {`{"input":"hello","response_format":"mp3"}`, http.StatusUnprocessableEntity},
		"model":    {`{"input":"hello","model":"other"}`, http.StatusNotFound},
		"voice":    {`{"input":"hello","voice":"robot"}`, http.StatusBadRequest},
		"path":     {`{"input":"hello","ref_audio":"/tmp/speaker.mp3"}`, http.StatusBadRequest},
		"stream":   {`{"input":"hello","stream_format":"sse"}`, http.StatusUnprocessableEntity},
		"trailing": {`{"input":"hello"} {}`, http.StatusBadRequest},
		"empty":    {`{"input":" "}`, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			service := &fakeService{}
			router := chi.NewRouter()
			New(service).Mount(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewBufferString(test.body)))
			if response.Code != test.status || service.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
			}
		})
	}
}

func TestOpenAIClientRequest(t *testing.T) {
	// The request the OpenAI SDK sends with only model, input and voice set.
	for _, model := range []string{"tts-1", "tts-1-hd", "gpt-4o-mini-tts", "test-tts"} {
		service := &fakeService{}
		router := chi.NewRouter()
		New(service).Mount(router)
		body := `{"model":"` + model + `","input":"Hello","voice":"Alloy","response_format":"wav"}`
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewBufferString(body)))
		if response.Code != http.StatusOK || service.calls != 1 || len(service.request.Voice.Audio) != 0 {
			t.Fatalf("model %s: status=%d calls=%d body=%s", model, response.Code, service.calls, response.Body.String())
		}
	}
}

func TestBodyLimit(t *testing.T) {
	service := &fakeService{}
	router := chi.NewRouter()
	New(service).Mount(router)
	body := `{"input":"x","ref_audio":"` + strings.Repeat("A", MaxBodyBytes) + `"}`
	for _, path := range []string{"/v1/audio/speech", "/v1/audio/voice"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body)))
		if response.Code != http.StatusRequestEntityTooLarge || service.calls != 0 {
			t.Fatalf("%s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

var wavFile = append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 32)...)

func multipartBody(t *testing.T, fields map[string]string, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for k, v := range files {
		fw, _ := mw.CreateFormFile(k, "voice.bin")
		_, _ = fw.Write(v)
	}
	_ = mw.Close()
	return body, mw.FormDataContentType()
}

func TestSpeechMultipartUpload(t *testing.T) {
	service := &fakeService{}
	router := chi.NewRouter()
	New(service).Mount(router)
	body, ct := multipartBody(t, map[string]string{"input": "hello", "voice": "alloy", "language": "en", "speed": "1"}, map[string][]byte{"ref_audio": wavFile})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", body)
	req.Header.Set("Content-Type", ct)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !bytes.Equal(service.request.Voice.Audio, wavFile) || service.request.Voice.Format != "wav" || service.request.Language != "en" {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, service.request, response.Body.String())
	}
	for name, fields := range map[string]map[string]string{
		"unknown field": {"input": "hello", "colour": "blue"},
		"bad speed":     {"input": "hello", "speed": "fast"},
	} {
		body, ct := multipartBody(t, fields, nil)
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", body)
		req.Header.Set("Content-Type", ct)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
}

func TestVoiceUploads(t *testing.T) {
	router := chi.NewRouter()
	New(&fakeService{}).Mount(router)
	mp3 := []byte("ID3\x04\x00\x00\x00\x00\x00\x00audio")
	send := func(body io.Reader, ct string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/voice", body)
		req.Header.Set("Content-Type", ct)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var out struct {
			Voice struct{ Audio, Format string } `json:"voice"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &out)
		return response.Code, out.Voice.Format
	}
	for _, field := range []string{"audio_sample", "file"} {
		body, ct := multipartBody(t, nil, map[string][]byte{field: wavFile})
		if code, format := send(body, ct); code != http.StatusOK || format != "wav" {
			t.Fatalf("multipart %s: status=%d format=%q", field, code, format)
		}
	}
	if code, format := send(bytes.NewReader(mp3), "audio/mpeg"); code != http.StatusOK || format != "mp3" {
		t.Fatalf("raw mp3: status=%d format=%q", code, format)
	}
	if code, _ := send(bytes.NewReader([]byte("%PDF-1.7")), "application/octet-stream"); code != http.StatusBadRequest {
		t.Fatalf("pdf accepted: status=%d", code)
	}
	if code, _ := send(bytes.NewReader(mp3), "text/plain"); code != http.StatusBadRequest {
		t.Fatalf("text/plain accepted: status=%d", code)
	}
}

func TestVoiceEndpointReturnsStatelessPayload(t *testing.T) {
	service := &fakeService{}
	router := chi.NewRouter()
	New(service).Mount(router)
	body := bytes.NewBufferString(`{"audio":"YXVkaW8=","format":"mp3"}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/audio/voice", body))
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte("/")) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
