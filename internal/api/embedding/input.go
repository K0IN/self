package embedding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	dom "ai-server/internal/embedding"
	"ai-server/internal/errs"
	"ai-server/internal/imageutil"
	"ai-server/internal/registry"
)

const maxMediaBytes = 10 << 20

func parseInput(raw json.RawMessage) (dom.Input, error) {
	return parseInputWithFetcher(context.Background(), raw, nil)
}

func parseInputWithFetcher(ctx context.Context, raw json.RawMessage, fetcher *imageutil.Fetcher) (dom.Input, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil && string(raw) != "null" {
		if strings.TrimSpace(text) == "" {
			return dom.Input{}, errs.New(errs.InvalidRequest, "input text must not be empty")
		}
		return dom.Input{Text: text}, nil
	}
	var object struct {
		Content []dom.Part `json:"content"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&object); err != nil || len(object.Content) == 0 {
		return dom.Input{}, errs.New(errs.InvalidRequest, "input must be text or an object with non-empty content")
	}
	for _, part := range object.Content {
		switch part.Type {
		case "text":
			if part.Text == nil || strings.TrimSpace(*part.Text) == "" || part.ImageURL != nil || part.InputAudio != nil {
				return dom.Input{}, errs.New(errs.InvalidRequest, "invalid text part")
			}
		case "image_url":
			if part.ImageURL == nil || part.Text != nil || part.InputAudio != nil {
				return dom.Input{}, errs.New(errs.InvalidRequest, "invalid image part")
			}
			data, mime, err := imageData(ctx, part.ImageURL.URL, fetcher)
			if err != nil {
				return dom.Input{}, err
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || cfg.Width*cfg.Height > 16777216 || (mime != "image/png" && mime != "image/jpeg") || "image/"+format != mime {
				return dom.Input{}, errs.New(errs.InvalidRequest, "image must be valid PNG or JPEG, at most 16 megapixels and 8192 pixels per side")
			}
			if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
				return dom.Input{}, errs.New(errs.InvalidRequest, "invalid image data: %s", err)
			}
			part.ImageURL.URL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
		case "input_audio":
			if part.InputAudio == nil || part.Text != nil || part.ImageURL != nil || part.InputAudio.Format != "wav" {
				return dom.Input{}, errs.New(errs.InvalidRequest, "audio requires input_audio data and format wav")
			}
			value := part.InputAudio.Data
			if !strings.HasPrefix(value, "data:") {
				value = "data:audio/wav;base64," + value
			}
			data, mime, err := inlineData(value)
			if err != nil {
				return dom.Input{}, err
			}
			if mime != "audio/wav" || !validWAV(data) {
				return dom.Input{}, errs.New(errs.InvalidRequest, "audio must be a valid PCM16 WAV")
			}
		default:
			return dom.Input{}, errs.New(errs.UnsupportedCapability, "content type %q is not supported", part.Type)
		}
	}
	return dom.Input{Content: object.Content}, nil
}

func imageData(ctx context.Context, value string, fetcher *imageutil.Fetcher) ([]byte, string, error) {
	if fetcher != nil {
		data, mime, err := fetcher.Fetch(ctx, value)
		if err != nil {
			return nil, "", err
		}
		if len(data) > maxMediaBytes {
			return nil, "", errs.New(errs.ImageTooLarge, "image exceeds %d bytes", maxMediaBytes)
		}
		return data, mime, nil
	}
	return inlineData(value)
}

func inlineData(value string) ([]byte, string, error) {
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") || len(encoded) > base64.StdEncoding.EncodedLen(maxMediaBytes) {
		return nil, "", errs.New(errs.InvalidRequest, "media requires inline base64 data, at most 10 MiB; remote URLs are not allowed")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxMediaBytes {
		return nil, "", errs.New(errs.InvalidRequest, "invalid or oversized base64 media")
	}
	return data, strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), nil
}

func validWAV(data []byte) bool {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return false
	}
	formatOK, samplesOK := false, false
	var alignment uint16
	offset := 12
	for offset+8 <= len(data) {
		length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		if length > len(data)-start {
			return false
		}
		chunk := data[start : start+length]
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if length < 16 {
				return false
			}
			channels := binary.LittleEndian.Uint16(chunk[2:4])
			rate := binary.LittleEndian.Uint32(chunk[4:8])
			alignment = binary.LittleEndian.Uint16(chunk[12:14])
			formatOK = binary.LittleEndian.Uint16(chunk[:2]) == 1 && channels >= 1 && channels <= 2 && rate >= 8000 && rate <= 96000 && binary.LittleEndian.Uint16(chunk[14:16]) == 16 && alignment == channels*2 && binary.LittleEndian.Uint32(chunk[8:12]) == rate*uint32(alignment)
		case "data":
			samplesOK = alignment > 0 && length > 0 && length%int(alignment) == 0
		}
		offset = start + length + length%2
		if offset > len(data) {
			return false
		}
	}
	return offset == len(data) && formatOK && samplesOK
}

func (h *Handler) validate(inputs []dom.Input) error {
	caps := h.svc.Capabilities()
	for _, input := range inputs {
		for _, part := range input.Content {
			capability := registry.CapText
			if part.Type == "image_url" {
				capability = registry.CapVision
			}
			if part.Type == "input_audio" {
				capability = registry.CapAudio
			}
			if !caps.Input.Has(capability) {
				return errs.New(errs.UnsupportedCapability, "loaded model does not support %s input", capability)
			}
		}
	}
	return nil
}
