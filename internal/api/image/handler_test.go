package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	apiroot "ai-server/internal/api"
	dom "ai-server/internal/image"
	"ai-server/internal/imageutil"
	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	generated, edited bool
	generatedPrompt   string
}

func (s *fakeService) ModelID() string          { return "test:image" }
func (s *fakeService) Quant() string            { return "q4" }
func (s *fakeService) Info() any                { return nil }
func (s *fakeService) Settings() map[string]any { return nil }
func (s *fakeService) Generate(_ context.Context, req dom.GenerateRequest) (dom.Response, error) {
	s.generated = true
	s.generatedPrompt = req.Prompt
	return dom.Response{OutputFormat: "png", Images: []dom.OutputImage{{Bytes: []byte("png")}}}, nil
}
func (s *fakeService) Edit(_ context.Context, req dom.EditRequest) (dom.Response, error) {
	s.edited = len(req.Images) == 2
	return dom.Response{OutputFormat: "png", Images: []dom.OutputImage{{Bytes: []byte("edited")}}}, nil
}

func testHandler(svc *fakeService) http.Handler {
	r := chi.NewRouter()
	New(svc, imageutil.DefaultLimits()).Mount(r)
	return r
}

func TestGenerationOpenAIShape(t *testing.T) {
	svc := &fakeService{}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"prompt":"a lighthouse","size":"512x512"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	testHandler(svc).ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !svc.generated {
		t.Fatalf("status=%d generated=%v prompt=%q body=%s", resp.Code, svc.generated, svc.generatedPrompt, resp.Body)
	}
	var body struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil || len(body.Data) != 1 {
		t.Fatalf("invalid response: %s", resp.Body)
	}
	if got, _ := base64.StdEncoding.DecodeString(body.Data[0].B64); string(got) != "png" {
		t.Fatalf("decoded image = %q", got)
	}
}

func TestDirectGenerationReturnsImageBytes(t *testing.T) {
	svc := &fakeService{}
	req := httptest.NewRequest(http.MethodGet, "/v1/images/generate?q=a+man+standing+on+a+hill", nil)
	resp := httptest.NewRecorder()
	testHandler(svc).ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !svc.generated || svc.generatedPrompt != "a man standing on a hill" {
		t.Fatalf("status=%d generated=%v prompt=%q body=%s", resp.Code, svc.generated, svc.generatedPrompt, resp.Body)
	}
	if got := resp.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Body.String(); got != "png" {
		t.Fatalf("body = %q, want raw image bytes", got)
	}
}

func TestDirectGenerationRequiresQuery(t *testing.T) {
	resp := httptest.NewRecorder()
	testHandler(&fakeService{}).ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/v1/images/generate", nil))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body)
	}
}

func TestEditAcceptsMultipleMultipartImages(t *testing.T) {
	svc := &fakeService{}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("prompt", "combine these")
	for _, name := range []string{"one.png", "two.png"} {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="image[]"; filename="`+name+`"`)
		h.Set("Content-Type", "image/png")
		part, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		var img bytes.Buffer
		if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(img.Bytes())
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := httptest.NewRecorder()
	testHandler(svc).ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !svc.edited {
		t.Fatalf("status=%d edited=%v body=%s", resp.Code, svc.edited, resp.Body)
	}
	if _, err := io.ReadAll(resp.Result().Body); err != nil {
		t.Fatal(err)
	}
}

var _ apiroot.Mount
