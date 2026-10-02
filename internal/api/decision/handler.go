package decision

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ai-server/internal/api"
	dom "ai-server/internal/decision"
	"ai-server/internal/errs"
)

// MaxBodyBytes bounds request bodies (data-URI images included).
const MaxBodyBytes = 32 << 20

// Handler serves decision routes.
type Handler struct {
	svc *dom.Service
}

// New creates a decision handler.
func New(svc *dom.Service) *Handler { return &Handler{svc: svc} }

// Mount registers routes and returns model metadata.
func (h *Handler) Mount(r chi.Router) api.ModelInfo {
	r.Post("/v1/systemone", h.systemOne)
	r.Post("/v1/decide", h.systemOne) // alias used by upstream Laya
	return api.ModelInfo{
		ID:           h.svc.ModelID(),
		Object:       "model",
		Type:         "decision",
		Quant:        h.svc.Quant(),
		Capabilities: h.svc.Capabilities(),
		Info:         h.svc.Info(),
		Settings:     h.svc.Settings(),
	}
}

func (h *Handler) systemOne(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRequest(w, r)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	in := dom.SystemOneInput{State: req.State, Questions: req.Questions}
	for _, im := range req.Images {
		in.Images = append(in.Images, dom.ImageSource{URL: im.URL, Name: im.Name, Description: im.Description})
	}
	resp, err := h.svc.Decide(r.Context(), in)
	if err != nil {
		if r.Context().Err() != nil {
			return // client is gone; nothing to write
		}
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, SystemOneResponse{Model: h.svc.ModelID(), Answers: resp.Answers, Usage: resp.Usage})
}

func decodeRequest(w http.ResponseWriter, r *http.Request) (SystemOneRequest, error) {
	var req SystemOneRequest
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return req, errs.New(errs.InvalidRequest, "Content-Type must be application/json")
	}
	body := http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return req, errs.New(errs.ImageTooLarge, "request body exceeds %d bytes", MaxBodyBytes)
		}
		return req, errs.New(errs.InvalidRequest, "cannot read request body")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, errs.New(errs.InvalidRequest, "invalid request body: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	if dec.More() {
		return req, errs.New(errs.InvalidRequest, "unexpected data after JSON body")
	}
	if len(req.State) == 0 || string(req.State) == "null" {
		return req, errs.New(errs.InvalidRequest, "state is required")
	}
	return req, nil
}
