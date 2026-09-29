package imageutil

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/webp"

	"ai-server/internal/errs"
)

var allowedMIME = map[string]string{
	"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/pjpeg": "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
	// generic types servers commonly send; the decoder decides.
	"application/octet-stream": "", "binary/octet-stream": "", "": "",
}

// Decode validates and decodes JPEG, PNG or WebP bytes. Dimensions are
// checked from the header before full decode (decompression-bomb guard).
// EXIF orientation is applied for JPEG.
func Decode(data []byte, declaredMIME string, lim Limits) (image.Image, error) {
	declared, ok := allowedMIME[declaredMIME]
	if !ok {
		return nil, errs.New(errs.UnsupportedImage, "unsupported image type %q (JPEG, PNG or WebP)", declaredMIME)
	}
	format := sniff(data)
	if format == "" {
		return nil, errs.New(errs.UnsupportedImage, "data is not a JPEG, PNG or WebP image")
	}
	if declared != "" && declared != format {
		return nil, errs.New(errs.UnsupportedImage, "declared type %s does not match %s content", declaredMIME, format)
	}
	var cfg image.Config
	var err error
	switch format {
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case "webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errs.New(errs.UnsupportedImage, "malformed %s image: %v", format, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errs.New(errs.UnsupportedImage, "image has invalid dimensions")
	}
	if cfg.Width > lim.MaxWidth || cfg.Height > lim.MaxHeight || int64(cfg.Width)*int64(cfg.Height) > lim.MaxPixels {
		return nil, errs.New(errs.ImageTooLarge, "image is %dx%d (limits: %dx%d, %d pixels)", cfg.Width, cfg.Height, lim.MaxWidth, lim.MaxHeight, lim.MaxPixels)
	}
	var img image.Image
	switch format {
	case "jpeg":
		img, err = jpeg.Decode(bytes.NewReader(data))
	case "png":
		img, err = png.Decode(bytes.NewReader(data))
	case "webp":
		img, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errs.New(errs.UnsupportedImage, "malformed %s image: %v", format, err)
	}
	if format == "jpeg" {
		img = applyOrientation(img, jpegOrientation(data))
	}
	return img, nil
}

// jpegOrientation extracts the EXIF orientation tag (1..8), default 1.
func jpegOrientation(data []byte) int {
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA { // EOI / SOS
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2:]))
		if segLen < 2 || i+2+segLen > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+segLen]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + segLen
	}
	return 1
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(tiff[4:]))
	if off < 8 || off+2 > len(tiff) {
		return 1
	}
	n := int(bo.Uint16(tiff[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(tiff) {
			return 1
		}
		if bo.Uint16(tiff[e:]) == 0x0112 {
			v := int(bo.Uint16(tiff[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// applyOrientation returns img transformed so that it displays upright.
func applyOrientation(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
