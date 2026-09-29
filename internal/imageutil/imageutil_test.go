package imageutil

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"ai-server/internal/decision"
	"ai-server/internal/errs"
)

// 1x1 lossless WebP.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testLimits() Limits {
	l := DefaultLimits()
	l.AllowHTTP, l.AllowPrivate = true, true
	return l
}

func TestDecodeFormats(t *testing.T) {
	lim := testLimits()
	red := solid(8, 4, color.RGBA{255, 0, 0, 255})
	for name, c := range map[string]struct {
		data []byte
		mime string
	}{
		"png":   {pngBytes(t, red), "image/png"},
		"jpeg":  {jpegBytes(t, red), "image/jpeg"},
		"octet": {pngBytes(t, red), "application/octet-stream"},
	} {
		img, err := Decode(c.data, c.mime, lim)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 4 {
			t.Fatalf("%s: bounds %v", name, img.Bounds())
		}
	}
	wb, _ := base64.StdEncoding.DecodeString(webp1x1)
	if img, err := Decode(wb, "image/webp", lim); err != nil || img.Bounds().Dx() != 1 {
		t.Fatalf("webp: %v", err)
	}
}

func TestDecodeRejects(t *testing.T) {
	lim := testLimits()
	p := pngBytes(t, solid(2, 2, color.White))
	if _, err := Decode(p, "image/gif", lim); errs.KindOf(err) != errs.UnsupportedImage {
		t.Errorf("gif mime: %v", err)
	}
	if _, err := Decode(p, "image/jpeg", lim); errs.KindOf(err) != errs.UnsupportedImage {
		t.Errorf("mismatch: %v", err)
	}
	if _, err := Decode([]byte("GIF89a....."), "", lim); errs.KindOf(err) != errs.UnsupportedImage {
		t.Errorf("gif data: %v", err)
	}
	if _, err := Decode(p[:30], "image/png", lim); errs.KindOf(err) != errs.UnsupportedImage {
		t.Errorf("truncated: %v", err)
	}
	lim.MaxWidth = 1
	if _, err := Decode(p, "image/png", lim); errs.KindOf(err) != errs.ImageTooLarge {
		t.Errorf("max dims: %v", err)
	}
	lim = testLimits()
	lim.MaxPixels = 3
	if _, err := Decode(p, "image/png", lim); errs.KindOf(err) != errs.ImageTooLarge {
		t.Errorf("max pixels: %v", err)
	}
}

func TestTargetSize(t *testing.T) {
	cases := []struct {
		w, h   int
		g      decision.ImageGeometry
		ww, wh int
	}{
		{1000, 500, decision.ImageGeometry{Mode: decision.GeometryFixed, Width: 448, Height: 448}, 448, 448},
		{2000, 1000, decision.ImageGeometry{Mode: decision.GeometryBounded, MaxWidth: 1024, MaxHeight: 1024}, 1024, 512},
		{500, 300, decision.ImageGeometry{Mode: decision.GeometryBounded, MaxWidth: 1024, MaxHeight: 1024}, 500, 300},
		{256, 240, decision.ImageGeometry{Mode: decision.GeometryDynamic, MaxPixels: 1003520, WidthMultiple: 32, HeightMultiple: 32}, 256, 256},
	}
	for _, c := range cases {
		w, h, err := TargetSize(c.w, c.h, c.g)
		if err != nil || w != c.ww || h != c.wh {
			t.Errorf("%dx%d %s: got %dx%d %v, want %dx%d", c.w, c.h, c.g.Mode, w, h, err, c.ww, c.wh)
		}
	}
	w, h, _ := TargetSize(4000, 3000, decision.ImageGeometry{Mode: decision.GeometryDynamic, MaxPixels: 1003520, WidthMultiple: 28, HeightMultiple: 28})
	if w%28 != 0 || h%28 != 0 || w*h > 1003520 {
		t.Errorf("dynamic: %dx%d", w, h)
	}
	if r := float64(w) / float64(h); r < 1.25 || r > 1.42 {
		t.Errorf("dynamic aspect %f", r)
	}
	if _, _, err := TargetSize(1, 1, decision.ImageGeometry{Mode: "weird"}); err == nil {
		t.Error("unknown mode accepted")
	}
}

func TestResizeContainPreservesAspect(t *testing.T) {
	// 4x2 white image into 4x4: rows 0 and 3 are black padding.
	out := ResizeRGB(solid(4, 2, color.White), 4, 4, decision.ResizeContain)
	if len(out) != 4*4*3 {
		t.Fatalf("len %d", len(out))
	}
	px := func(x, y int) byte { return out[(y*4+x)*3] }
	if px(1, 0) != 0 || px(1, 3) != 0 || px(1, 1) < 200 || px(1, 2) < 200 {
		t.Fatalf("letterbox wrong: %v", out)
	}
	cov := ResizeRGB(solid(4, 2, color.White), 4, 4, decision.ResizeCover)
	for _, b := range cov {
		if b < 200 {
			t.Fatal("cover should have no padding")
		}
	}
}

func TestOrientation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 0, color.RGBA{0, 0, 255, 255})
	r := applyOrientation(img, 6) // rotate 90° CW → 1x2, red on top
	if r.Bounds().Dx() != 1 || r.Bounds().Dy() != 2 {
		t.Fatalf("bounds %v", r.Bounds())
	}
	if red, _, _, _ := r.At(0, 0).RGBA(); red < 0xff00 {
		t.Fatal("expected red at top")
	}
}

func TestPrepareDataURIAndURL(t *testing.T) {
	data := pngBytes(t, solid(20, 10, color.RGBA{0, 255, 0, 255}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
		case "/big":
			w.Header().Set("Content-Type", "image/png")
			w.Write(make([]byte, 2048))
		case "/redirect":
			http.Redirect(w, r, "/redirect", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	lim := testLimits()
	p := NewPreprocessor(lim)
	g := decision.ImageGeometry{Mode: decision.GeometryFixed, Width: 16, Height: 16}
	ctx := context.Background()

	im, err := p.Prepare(ctx, decision.ImageSource{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), Name: "front"}, g)
	if err != nil || im.Width != 16 || len(im.Pixels) != 16*16*3 || im.Name != "front" || im.Format != decision.FormatRGB8 {
		t.Fatalf("data uri: %v %+v", err, im.Width)
	}
	if _, err := p.Prepare(ctx, decision.ImageSource{URL: srv.URL + "/ok.png"}, g); err != nil {
		t.Fatalf("url: %v", err)
	}
	if _, err := p.Prepare(ctx, decision.ImageSource{URL: srv.URL + "/missing"}, g); errs.KindOf(err) != errs.ImageFetchFailed {
		t.Fatalf("404: %v", err)
	}
	if _, err := p.Prepare(ctx, decision.ImageSource{URL: srv.URL + "/redirect"}, g); errs.KindOf(err) != errs.ImageFetchFailed {
		t.Fatalf("redirect loop: %v", err)
	}
	small := lim
	small.MaxSourceBytes = 1024
	if _, err := NewPreprocessor(small).Prepare(ctx, decision.ImageSource{URL: srv.URL + "/big"}, g); errs.KindOf(err) != errs.ImageTooLarge {
		t.Fatalf("oversize: %v", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "ftp://x/y.png", "data:image/png,notbase64", "data:image/png;base64,@@@"} {
		if _, err := p.Prepare(ctx, decision.ImageSource{URL: bad}, g); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestPrivateAddressBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	lim := DefaultLimits()
	lim.AllowHTTP = true // but not private
	_, _, err := NewFetcher(lim).Fetch(context.Background(), srv.URL+"/x.png")
	if errs.KindOf(err) != errs.ImageFetchFailed {
		t.Fatalf("loopback fetch allowed: %v", err)
	}
	lim.AllowHTTP = false
	if _, _, err := NewFetcher(lim).Fetch(context.Background(), srv.URL); errs.KindOf(err) != errs.InvalidRequest {
		t.Fatalf("http allowed: %v", err)
	}
	for addr, want := range map[string]bool{"8.8.8.8": true, "10.0.0.1": false, "127.0.0.1": false, "169.254.169.254": false, "::1": false, "100.64.1.1": false, "2606:4700::1111": true} {
		if got := IsPublicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: got %v", addr, got)
		}
	}
}
