package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	apiroot "ai-server/internal/api"
	dom "ai-server/internal/audio"
	"ai-server/internal/errs"
	"github.com/go-chi/chi/v5"
)

// MaxBodyBytes bounds request bodies, inline reference audio included.
const MaxBodyBytes = 32 << 20

// openAIModels are OpenAI TTS model names accepted as aliases for the loaded model.
var openAIModels = map[string]bool{"tts-1": true, "tts-1-hd": true, "gpt-4o-mini-tts": true}

// openAIVoices are OpenAI built-in voices; they select the model's default voice.
var openAIVoices = map[string]bool{
	"alloy": true, "ash": true, "ballad": true, "cedar": true, "coral": true, "echo": true, "fable": true,
	"marin": true, "nova": true, "onyx": true, "sage": true, "shimmer": true, "verse": true,
}

type Service interface {
	ModelID() string
	Quant() string
	Info() any
	Settings() map[string]any
	Synthesize(context.Context, dom.Request) (dom.Response, error)
}

type Handler struct {
	svc Service
}

func New(svc Service) *Handler { return &Handler{svc: svc} }

type request struct {
	Model          string          `json:"model"`
	Input          string          `json:"input"`
	Voice          json.RawMessage `json:"voice"`
	Instructions   string          `json:"instructions"`
	ResponseFormat string          `json:"response_format"`
	StreamFormat   string          `json:"stream_format"`
	Speed          float64         `json:"speed"`
	Language       string          `json:"language"`
	RefAudio       string          `json:"ref_audio"` // base64 or data URL, never a path

	refUpload []byte // ref_audio file part of a multipart request
}

func (h *Handler) Mount(r chi.Router) apiroot.ModelInfo {
	r.Post("/v1/audio/speech", h.speech)
	r.Post("/v1/audio/voice", h.voice)
	r.Post("/v1/audio/voices", h.voice)
	return apiroot.ModelInfo{ID: h.svc.ModelID(), Object: "model", Type: "audio", Quant: h.svc.Quant(), Capabilities: map[string]any{"input": []string{"text"}, "output": []string{"audio"}}, Info: h.svc.Info(), Settings: h.svc.Settings()}
}

func (h *Handler) speech(w http.ResponseWriter, r *http.Request) {
	req, err := decode(r, w)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	if req.Model != "" && req.Model != h.svc.ModelID() && !openAIModels[req.Model] && !strings.HasPrefix(req.Model, "gpt-4o-mini-tts-") {
		apiroot.WriteError(w, errs.New(errs.ModelNotFound, "model %q is not loaded", req.Model))
		return
	}
	voice, err := parseVoice(req.Voice, req.RefAudio, req.refUpload)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	if req.StreamFormat != "" && req.StreamFormat != "audio" {
		apiroot.WriteError(w, errs.New(errs.UnsupportedCapability, "stream_format %q is not supported by the loaded audio model", req.StreamFormat))
		return
	}
	if req.ResponseFormat != "wav" {
		apiroot.WriteError(w, errs.New(errs.UnsupportedCapability, "response_format %q is not supported by the loaded audio model", req.ResponseFormat))
		return
	}
	if req.Instructions != "" {
		apiroot.WriteError(w, errs.New(errs.UnsupportedCapability, "instructions are not supported by the loaded audio model"))
		return
	}
	if req.Speed != 1 {
		apiroot.WriteError(w, errs.New(errs.UnsupportedCapability, "speed other than 1 is not supported by the loaded audio model"))
		return
	}
	resp, err := h.svc.Synthesize(r.Context(), dom.Request{Input: req.Input, Voice: voice, Instructions: req.Instructions, Language: req.Language, Speed: req.Speed})
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", "attachment; filename=speech.wav")
	_, _ = w.Write(resp.WAV)
}

func (h *Handler) voice(w http.ResponseWriter, r *http.Request) {
	audio, format, err := voiceUpload(w, r)
	if err != nil {
		apiroot.WriteError(w, err)
		return
	}
	apiroot.WriteJSON(w, http.StatusOK, map[string]any{"object": "voice", "voice": map[string]any{"audio": base64.StdEncoding.EncodeToString(audio), "format": format}})
}

// voiceUpload reads reference audio sent as a multipart file, a raw audio body, or JSON base64.
func voiceUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	switch ct := mediaType(r); ct {
	case "multipart/form-data":
		parts, err := readParts(w, r)
		if err != nil {
			return nil, "", err
		}
		for name := range parts {
			if name != "audio_sample" && name != "file" && name != "format" {
				return nil, "", errs.New(errs.InvalidRequest, "unknown form field %q; send the audio as audio_sample or file", name)
			}
		}
		audio := parts["audio_sample"]
		if audio == nil {
			audio = parts["file"]
		}
		return checkAudio(audio, string(parts["format"]))
	case "", "application/json":
		var req struct {
			Audio  string `json:"audio"`
			Format string `json:"format"`
		}
		b, err := readBody(w, r)
		if err != nil {
			return nil, "", err
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			return nil, "", errs.New(errs.InvalidRequest, "invalid request body: %s", err)
		}
		if req.Audio == "" {
			return nil, "", errs.New(errs.InvalidRequest, "audio is required as base64 or a data URL")
		}
		return decodeAudio(req.Audio, req.Format)
	default:
		if !strings.HasPrefix(ct, "audio/") && ct != "application/octet-stream" {
			return nil, "", errs.New(errs.InvalidRequest, "Content-Type must be multipart/form-data, audio/*, or application/json")
		}
		b, err := readBody(w, r)
		if err != nil {
			return nil, "", err
		}
		return checkAudio(b, "")
	}
}

func mediaType(r *http.Request) string {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return strings.ToLower(ct)
}

// readParts reads a multipart body into memory; unlike ParseMultipartForm it never spills files to disk.
func readParts(w http.ResponseWriter, r *http.Request) (map[string][]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, errs.New(errs.InvalidRequest, "invalid multipart body: %s", err)
	}
	parts := map[string][]byte{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return parts, nil
		}
		if err != nil {
			return nil, bodyError(err)
		}
		b, err := io.ReadAll(p)
		if err != nil {
			return nil, bodyError(err)
		}
		if _, dup := parts[p.FormName()]; dup {
			return nil, errs.New(errs.InvalidRequest, "form field %q is given twice", p.FormName())
		}
		parts[p.FormName()] = b
	}
}

func bodyError(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return errs.New(errs.RequestTooLarge, "request body exceeds %d bytes", MaxBodyBytes)
	}
	return errs.New(errs.InvalidRequest, "cannot read request body: %s", err)
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		return nil, bodyError(err)
	}
	return b, nil
}

func decode(r *http.Request, w http.ResponseWriter) (request, error) {
	var req request
	var err error
	switch mediaType(r) {
	case "", "application/json":
		err = decodeJSON(r, w, &req)
	case "multipart/form-data":
		err = decodeForm(r, w, &req)
	default:
		err = errs.New(errs.InvalidRequest, "Content-Type must be application/json or multipart/form-data")
	}
	if err != nil {
		return req, err
	}
	if strings.TrimSpace(req.Input) == "" || len([]rune(req.Input)) > 4096 {
		return req, errs.New(errs.InvalidRequest, "input is required and must be at most 4096 characters")
	}
	if req.ResponseFormat == "" {
		req.ResponseFormat = "wav"
	}
	if req.Speed == 0 {
		req.Speed = 1
	}
	if req.Speed < .25 || req.Speed > 4 {
		return req, errs.New(errs.InvalidRequest, "speed must be between 0.25 and 4")
	}
	return req, nil
}

func decodeJSON(r *http.Request, w http.ResponseWriter, req *request) error {
	b, err := readBody(w, r)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return errs.New(errs.InvalidRequest, "invalid request body: %s", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errs.New(errs.InvalidRequest, "request body must contain one JSON object")
	}
	return nil
}

// decodeForm reads the speech fields from multipart/form-data; ref_audio is a file part.
func decodeForm(r *http.Request, w http.ResponseWriter, req *request) error {
	parts, err := readParts(w, r)
	if err != nil {
		return err
	}
	for name, value := range parts {
		s := string(value)
		switch name {
		case "input":
			req.Input = s
		case "model":
			req.Model = s
		case "voice":
			req.Voice, _ = json.Marshal(s)
		case "instructions":
			req.Instructions = s
		case "response_format":
			req.ResponseFormat = s
		case "stream_format":
			req.StreamFormat = s
		case "language":
			req.Language = s
		case "speed":
			if req.Speed, err = strconv.ParseFloat(s, 64); err != nil {
				return errs.New(errs.InvalidRequest, "speed must be a number")
			}
		case "ref_audio":
			req.refUpload = value
		default:
			return errs.New(errs.InvalidRequest, "unknown form field %q", name)
		}
	}
	return nil
}

func parseVoice(raw json.RawMessage, ref string, upload []byte) (dom.Voice, error) {
	v := dom.Voice{}
	if upload != nil {
		audio, format, err := checkAudio(upload, "")
		if err != nil {
			return v, err
		}
		v.Audio, v.Format = audio, format
	}
	if ref != "" {
		audio, format, err := decodeAudio(ref, "")
		if err != nil {
			return v, err
		}
		v.Audio, v.Format = audio, format
	}
	if len(raw) == 0 {
		return v, nil
	}
	var id string
	if json.Unmarshal(raw, &id) == nil {
		if openAIVoices[strings.ToLower(strings.TrimSpace(id))] {
			return v, nil
		}
		return v, errs.New(errs.InvalidRequest, "unknown voice %q; use an OpenAI voice name or send voice.audio inline", id)
	}
	var obj struct {
		ID     string `json:"id"`
		Audio  string `json:"audio"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || (obj.ID == "" && obj.Audio == "") {
		return v, errs.New(errs.InvalidRequest, "voice must be an object with inline audio")
	}
	if obj.ID != "" {
		return v, errs.New(errs.InvalidRequest, "voice IDs are not supported; send voice.audio inline")
	}
	audio, format, err := decodeAudio(obj.Audio, obj.Format)
	if err != nil {
		return v, err
	}
	v.Audio, v.Format = audio, format
	return v, nil
}

func decodeAudio(value, format string) ([]byte, string, error) {
	if strings.HasPrefix(value, "data:") {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return nil, "", errs.New(errs.InvalidRequest, "audio data URL must be base64")
		}
		value = parts[1]
	}
	audio, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(audio) == 0 {
		if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "~") {
			return nil, "", errs.New(errs.InvalidRequest, "audio paths are not supported; send base64 audio or upload the file")
		}
		return nil, "", errs.New(errs.InvalidRequest, "audio must be non-empty base64")
	}
	return checkAudio(audio, format)
}

// checkAudio validates reference audio; the format comes from the file's magic bytes, else from the declared format.
func checkAudio(audio []byte, format string) ([]byte, string, error) {
	if len(audio) == 0 {
		return nil, "", errs.New(errs.InvalidRequest, "reference audio is empty")
	}
	if sniffed := sniffAudio(audio); sniffed != "" {
		format = sniffed
	}
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if format != "mp3" && format != "wav" {
		return nil, "", errs.New(errs.InvalidRequest, "reference audio must be an MP3 or WAV file")
	}
	return audio, format, nil
}

func sniffAudio(b []byte) string {
	switch {
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WAVE":
		return "wav"
	case len(b) >= 3 && string(b[:3]) == "ID3", len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0:
		return "mp3"
	}
	return ""
}
