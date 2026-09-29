package imageutil

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
)

// TargetSize computes output dimensions for an image of size (w, h) under
// the engine-provided geometry.
func TargetSize(w, h int, g decision.ImageGeometry) (int, int, error) {
	switch g.Mode {
	case decision.GeometryFixed:
		if g.Width <= 0 || g.Height <= 0 {
			return 0, 0, fmt.Errorf("fixed geometry requires width and height")
		}
		return g.Width, g.Height, nil
	case decision.GeometryBounded:
		if g.MaxWidth <= 0 || g.MaxHeight <= 0 {
			return 0, 0, fmt.Errorf("bounded geometry requires max_width and max_height")
		}
		s := math.Min(1, math.Min(float64(g.MaxWidth)/float64(w), float64(g.MaxHeight)/float64(h)))
		return max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s))), nil
	case decision.GeometryDynamic:
		wm, hm := max(1, g.WidthMultiple), max(1, g.HeightMultiple)
		if g.MaxPixels <= 0 || g.MaxPixels < wm*hm {
			return 0, 0, fmt.Errorf("dynamic geometry requires max_pixels >= one patch")
		}
		// Qwen-VL style smart resize: round to multiples, then scale into
		// [min_pixels, max_pixels] preserving aspect ratio.
		tw := max(wm, roundTo(float64(w), wm))
		th := max(hm, roundTo(float64(h), hm))
		if tw*th > g.MaxPixels {
			beta := math.Sqrt(float64(w*h) / float64(g.MaxPixels))
			tw = max(wm, floorTo(float64(w)/beta, wm))
			th = max(hm, floorTo(float64(h)/beta, hm))
		} else if g.MinPixels > 0 && tw*th < g.MinPixels {
			beta := math.Sqrt(float64(g.MinPixels) / float64(w*h))
			tw = ceilTo(float64(w)*beta, wm)
			th = ceilTo(float64(h)*beta, hm)
		}
		return tw, th, nil
	}
	return 0, 0, fmt.Errorf("unknown geometry mode %q", g.Mode)
}

func roundTo(v float64, m int) int { return int(math.Round(v/float64(m))) * m }
func floorTo(v float64, m int) int { return int(math.Floor(v/float64(m))) * m }
func ceilTo(v float64, m int) int  { return int(math.Ceil(v/float64(m))) * m }

// ResizeRGB scales img to tw x th and returns packed RGB8 bytes. For
// ResizeContain the image is letterboxed on black; for ResizeCover it is
// center-cropped; ResizeStretch ignores aspect ratio. Alpha is composited
// onto black.
func ResizeRGB(img image.Image, tw, th int, mode decision.ResizeMode) []byte {
	src := img.Bounds()
	sw, sh := src.Dx(), src.Dy()
	canvas := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)

	dstRect := canvas.Bounds()
	srcRect := src
	switch mode {
	case decision.ResizeStretch:
	case decision.ResizeCover:
		s := math.Max(float64(tw)/float64(sw), float64(th)/float64(sh))
		cw, ch := int(math.Round(float64(tw)/s)), int(math.Round(float64(th)/s))
		cw, ch = min(max(cw, 1), sw), min(max(ch, 1), sh)
		x0 := src.Min.X + (sw-cw)/2
		y0 := src.Min.Y + (sh-ch)/2
		srcRect = image.Rect(x0, y0, x0+cw, y0+ch)
	default: // contain
		s := math.Min(float64(tw)/float64(sw), float64(th)/float64(sh))
		dw, dh := max(1, int(math.Round(float64(sw)*s))), max(1, int(math.Round(float64(sh)*s)))
		x0, y0 := (tw-dw)/2, (th-dh)/2
		dstRect = image.Rect(x0, y0, x0+dw, y0+dh)
	}
	xdraw.CatmullRom.Scale(canvas, dstRect, img, srcRect, xdraw.Over, nil)

	out := make([]byte, tw*th*3)
	for y := 0; y < th; y++ {
		row := canvas.Pix[y*canvas.Stride : y*canvas.Stride+tw*4]
		o := out[y*tw*3:]
		for x := 0; x < tw; x++ {
			o[x*3], o[x*3+1], o[x*3+2] = row[x*4], row[x*4+1], row[x*4+2]
		}
	}
	return out
}

// Preprocessor implements decision.Preprocessor: fetch → decode → orient →
// resize → RGB, entirely in memory.
type Preprocessor struct {
	Fetcher *Fetcher
	Limits  Limits
}

// NewPreprocessor creates a preprocessor.
func NewPreprocessor(lim Limits) *Preprocessor {
	return &Preprocessor{Fetcher: NewFetcher(lim), Limits: lim}
}

// Prepare implements decision.Preprocessor.
func (p *Preprocessor) Prepare(ctx context.Context, src decision.ImageSource, g decision.ImageGeometry) (decision.Image, error) {
	data, mt, err := p.Fetcher.Fetch(ctx, src.URL)
	if err != nil {
		return decision.Image{}, err
	}
	img, err := Decode(data, mt, p.Limits)
	data = nil // release compressed bytes early
	if err != nil {
		return decision.Image{}, err
	}
	b := img.Bounds()
	tw, th, err := TargetSize(b.Dx(), b.Dy(), g)
	if err != nil {
		return decision.Image{}, errs.Wrap(errs.Internal, err, "engine reported invalid image geometry")
	}
	mode := g.Resize
	if mode == "" {
		mode = decision.ResizeContain
	}
	return decision.Image{
		Name:        src.Name,
		Description: src.Description,
		Width:       tw,
		Height:      th,
		Format:      decision.FormatRGB8,
		Pixels:      ResizeRGB(img, tw, th, mode),
	}, nil
}
