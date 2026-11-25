package previewimage

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"golang.org/x/image/webp"
)

func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 13), B: uint8(x ^ y), A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// requireEncoder skips the tests that run cwebp when it is not installed (the
// API image carries it; see the Dockerfile).
func requireEncoder(t *testing.T) {
	t.Helper()
	if err := Check(); err != nil {
		t.Skipf("cwebp unavailable: %v", err)
	}
}

func TestEncodeFullAndThumbnail(t *testing.T) {
	requireEncoder(t)
	source := pngOf(t, 1000, 400)
	encoded, err := Encode(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.Width != 1000 || encoded.Height != 400 {
		t.Fatalf("size = %dx%d", encoded.Width, encoded.Height)
	}

	// "VP8 " is WebP's lossy bitstream (VP8L is lossless).
	if len(encoded.Full) < 16 || string(encoded.Full[12:16]) != "VP8 " {
		t.Fatalf("full preview is not lossy WebP: % x", encoded.Full[:min(16, len(encoded.Full))])
	}
	full, err := webp.DecodeConfig(bytes.NewReader(encoded.Full))
	if err != nil || full.Width != 1000 || full.Height != 400 {
		t.Fatalf("full preview = %+v, %v", full, err)
	}

	thumbnail, err := webp.DecodeConfig(bytes.NewReader(encoded.Thumbnail))
	if err != nil {
		t.Fatal(err)
	}
	if thumbnail.Width != 800 || thumbnail.Height != 320 {
		t.Fatalf("thumbnail = %dx%d, want 800x320", thumbnail.Width, thumbnail.Height)
	}
}

// The encoded image keeps the source's pixels within lossy tolerance: the PPM
// stream is not transposed, shifted or channel-swapped.
func TestEncodeKeepsThePixels(t *testing.T) {
	requireEncoder(t)
	source := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			c := color.NRGBA{R: 220, G: 30, B: 30, A: 255}
			if x >= 32 {
				c = color.NRGBA{R: 30, G: 30, B: 220, A: 255}
			}
			source.SetNRGBA(x, y, c)
		}
	}
	var png64 bytes.Buffer
	if err := png.Encode(&png64, source); err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(context.Background(), png64.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := webp.Decode(bytes.NewReader(encoded.Thumbnail))
	if err != nil {
		t.Fatal(err)
	}
	near := func(got color.Color, want color.NRGBA) bool {
		r, g, b, _ := got.RGBA()
		d := func(a uint32, b uint8) int { return int(a>>8) - int(b) }
		return abs(d(r, want.R)) < 24 && abs(d(g, want.G)) < 24 && abs(d(b, want.B)) < 24
	}
	if !near(decoded.At(8, 16), color.NRGBA{R: 220, G: 30, B: 30}) || !near(decoded.At(56, 16), color.NRGBA{R: 30, G: 30, B: 220}) {
		t.Fatalf("pixels changed: left %v, right %v", decoded.At(8, 16), decoded.At(56, 16))
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// A preview that fits within ThumbnailMax is stored as its thumbnail alone.
func TestEncodeSmallPreviewIsThumbnailOnly(t *testing.T) {
	requireEncoder(t)
	encoded, err := Encode(context.Background(), pngOf(t, 800, 300))
	if err != nil {
		t.Fatal(err)
	}
	if encoded.Full != nil || HasFull(800, 300) {
		t.Fatalf("small preview has a full file (%d bytes)", len(encoded.Full))
	}
	thumbnail, err := webp.DecodeConfig(bytes.NewReader(encoded.Thumbnail))
	if err != nil || thumbnail.Width != 800 || thumbnail.Height != 300 {
		t.Fatalf("thumbnail = %+v, %v", thumbnail, err)
	}
	if !HasFull(801, 300) || !HasFull(10, 801) {
		t.Fatal("previews larger than ThumbnailMax need a full file")
	}
}

func TestEncodeRejectsInvalidImages(t *testing.T) {
	if _, err := Encode(context.Background(), []byte("not a png")); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("garbage: %v", err)
	}
}

func TestThumbnailSize(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{640, 480, 640, 480},
		{800, 800, 800, 800},
		{1600, 2272, 563, 800},
		{2944, 1376, 800, 374},
		{8000, 3, 800, 1},
	}
	for _, c := range cases {
		if w, h := ThumbnailSize(c.w, c.h); w != c.wantW || h != c.wantH {
			t.Errorf("ThumbnailSize(%d, %d) = %d, %d; want %d, %d", c.w, c.h, w, h, c.wantW, c.wantH)
		}
	}
}
