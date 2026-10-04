package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	apiroot "ai-server/internal/api"
	"ai-server/internal/errs"
	dom "ai-server/internal/image"
	"ai-server/internal/imageutil"
	"github.com/go-chi/chi/v5"
)

const maxBody = 32 << 20

type Service interface {
	ModelID() string
	Quant() string
	Info() any
	Settings() map[string]any
	Generate(context.Context, dom.GenerateRequest) (dom.Response, error)
	Edit(context.Context, dom.EditRequest) (dom.Response, error)
}
type Handler struct {
	svc     Service
	fetcher *imageutil.Fetcher
	limits  imageutil.Limits
}

func New(svc Service, lim imageutil.Limits) *Handler {
	return &Handler{svc: svc, limits: lim, fetcher: imageutil.NewFetcher(lim)}
}
func (h *Handler) Mount(r chi.Router) apiroot.ModelInfo {
	r.Post("/v1/images/generations", h.generate)
	r.Post("/v1/images/edits", h.edit)
	return apiroot.ModelInfo{ID: h.svc.ModelID(), Object: "model", Type: "image", Quant: h.svc.Quant(), Capabilities: map[string]any{"input": []string{"text", "image", "multi-image"}, "output": []string{"image", "image-edit"}}, Info: h.svc.Info(), Settings: h.svc.Settings()}
}

type generationRequest struct {
	Model          string         `json:"model"`
	Prompt         string         `json:"prompt"`
	NegativePrompt string         `json:"negative_prompt"`
	N              int            `json:"n"`
	Size           string         `json:"size"`
	OutputFormat   string         `json:"output_format"`
	Compression    int            `json:"output_compression"`
	Seed           *int64         `json:"seed"`
	Extra          map[string]any `json:"sd_cpp_extra_args"`
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	var req generationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	if err := validatePrompt(req.Prompt); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	width, height, err := parseSize(req.Size)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	n := req.N
	if n == 0 {
		n = 1
	}
	if n < 1 || n > 8 {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "n must be between 1 and 8"))
		return
	}
	format := req.OutputFormat
	if format == "" {
		format = "png"
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "output_format must be png, jpeg or webp"))
		return
	}
	resp, err := h.svc.Generate(r.Context(), dom.GenerateRequest{Prompt: req.Prompt, NegativePrompt: req.NegativePrompt, N: n, Width: width, Height: height, OutputFormat: format, Compression: req.Compression, Seed: req.Seed, Extra: req.Extra})
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	apiroot.WriteJSON(w, http.StatusOK, openAIResponse(resp))
}
func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	parts, err := readMultipart(w, r)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	prompt := one(parts.fields, "prompt")
	if err = validatePrompt(prompt); err != nil {
		apiroot.WriteError(w, err)
		return
	}
	images := parts.files["image[]"]
	if len(images) == 0 {
		images = parts.files["image"]
	}
	if len(images) == 0 {
		images = parts.files["file"]
	}
	parsed := make([]dom.InputImage, 0, len(images)+len(parts.fields["image_url[]"])+len(parts.fields["image_url"]))
	for _, f := range images {
		img, e := h.resolveInput(r.Context(), f.data, f.mime, f.name)
		if e != nil {
			apiroot.WriteError(w, e)
			return
		}
		parsed = append(parsed, img)
	}
	for _, key := range []string{"image_url[]", "image_url"} {
		for _, src := range parts.fields[key] {
			img, e := h.resolveInput(r.Context(), []byte(src), "", "")
			if e != nil {
				apiroot.WriteError(w, e)
				return
			}
			parsed = append(parsed, img)
		}
	}
	if len(parsed) == 0 {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "at least one image is required"))
		return
	}
	if len(parsed) > 8 {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "at most 8 reference images are accepted"))
		return
	}
	var mask *dom.InputImage
	if ms := parts.files["mask"]; len(ms) > 0 {
		m, e := h.resolveInput(r.Context(), ms[0].data, ms[0].mime, ms[0].name)
		if e != nil {
			apiroot.WriteError(w, e)
			return
		}
		mask = &m
	}
	n, _ := strconv.Atoi(one(parts.fields, "n"))
	if n == 0 {
		n = 1
	}
	if n < 1 || n > 8 {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "n must be between 1 and 8"))
		return
	}
	width, height, e := parseSize(one(parts.fields, "size"))
	if one(parts.fields, "size") == "" {
		width, height = 0, 0
		e = nil
	}
	if e != nil {
		apiroot.WriteError(w, e)
		return
	}
	format := one(parts.fields, "output_format")
	if format == "" {
		format = "png"
	}
	if format != "png" && format != "jpeg" {
		apiroot.WriteError(w, errs.New(errs.InvalidRequest, "edit output_format must be png or jpeg"))
		return
	}
	resp, e := h.svc.Edit(r.Context(), dom.EditRequest{Prompt: prompt, Images: parsed, Mask: mask, N: n, Width: width, Height: height, OutputFormat: format})
	if e != nil {
		apiroot.WriteError(w, e)
		return
	}
	apiroot.WriteJSON(w, http.StatusOK, openAIResponse(resp))
}

type upload struct {
	data       []byte
	mime, name string
}
type parsedParts struct {
	fields map[string][]string
	files  map[string][]upload
}

func readMultipart(w http.ResponseWriter, r *http.Request) (parsedParts, error) {
	out := parsedParts{fields: map[string][]string{}, files: map[string][]upload{}}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	mr, err := r.MultipartReader()
	if err != nil {
		return out, errs.New(errs.InvalidRequest, "expected multipart/form-data")
	}
	for {
		p, e := mr.NextPart()
		if e == io.EOF {
			return out, nil
		}
		if e != nil {
			return out, errs.New(errs.InvalidRequest, "invalid multipart request")
		}
		data, e := io.ReadAll(io.LimitReader(p, maxBody+1))
		if e != nil || len(data) > maxBody {
			return out, errs.New(errs.RequestTooLarge, "multipart request too large")
		}
		if p.FileName() != "" {
			out.files[p.FormName()] = append(out.files[p.FormName()], upload{data, p.Header.Get("Content-Type"), p.FileName()})
		} else {
			out.fields[p.FormName()] = append(out.fields[p.FormName()], string(data))
		}
	}
}
func one(m map[string][]string, k string) string {
	if len(m[k]) == 0 {
		return ""
	}
	return m[k][0]
}
func (h *Handler) resolveInput(ctx context.Context, data []byte, mt, name string) (dom.InputImage, error) {
	raw := strings.TrimSpace(string(data))
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "data:") {
		var err error
		data, mt, err = h.fetcher.Fetch(ctx, raw)
		if err != nil {
			return dom.InputImage{}, err
		}
	}
	if mt == "" {
		mt = http.DetectContentType(data)
		mt, _, _ = mime.ParseMediaType(mt)
	}
	decoded, err := imageutil.Decode(data, mt, h.limits)
	if err != nil {
		return dom.InputImage{}, err
	}
	var normalized bytes.Buffer
	if err := png.Encode(&normalized, decoded); err != nil {
		return dom.InputImage{}, errs.Wrap(errs.Internal, err, "cannot encode normalized image")
	}
	return dom.InputImage{Bytes: normalized.Bytes(), MIME: "image/png", Name: "image.png"}, nil
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "" && mt != "application/json" {
		return errs.New(errs.InvalidRequest, "Content-Type must be application/json")
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if e != nil {
		return errs.New(errs.RequestTooLarge, "request body exceeds limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return errs.New(errs.InvalidRequest, "invalid JSON: %s", e)
	}
	if d.Decode(new(any)) != io.EOF {
		return errs.New(errs.InvalidRequest, "request body must contain one JSON object")
	}
	return nil
}
func validatePrompt(p string) error {
	if strings.TrimSpace(p) == "" || len([]rune(p)) > 4096 {
		return errs.New(errs.InvalidRequest, "prompt is required and must be at most 4096 characters")
	}
	return nil
}
func parseSize(s string) (int, int, error) {
	if s == "" {
		return 1024, 1024, nil
	}
	wstr, hstr, ok := strings.Cut(strings.ToLower(s), "x")
	if !ok {
		return 0, 0, errs.New(errs.InvalidRequest, "size must use WIDTHxHEIGHT")
	}
	w, e1 := strconv.Atoi(wstr)
	h, e2 := strconv.Atoi(hstr)
	if e1 != nil || e2 != nil || w < 64 || h < 64 || w > 4096 || h > 4096 || int64(w)*int64(h) > 16_000_000 {
		return 0, 0, errs.New(errs.InvalidRequest, "size dimensions must be 64..4096 and at most 16 megapixels")
	}
	return w, h, nil
}
func openAIResponse(r dom.Response) map[string]any {
	data := make([]map[string]string, 0, len(r.Images))
	for _, img := range r.Images {
		data = append(data, map[string]string{"b64_json": base64.StdEncoding.EncodeToString(img.Bytes)})
	}
	created := r.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	return map[string]any{"created": created, "output_format": r.OutputFormat, "data": data}
}
