// Package previewimage turns a runner's PNG preview into the files the API
// stores: a lossy WebP of the full preview and a lossy WebP thumbnail that fits
// within ThumbnailMax px. A preview that already fits (HasFull false) is stored
// as the thumbnail alone, which then serves as its full image too.
//
// Both files are encoded by the cwebp binary (libwebp's own tool, a static
// build in the API image, see the Dockerfile): quality 80 with sharp YUV, which
// keeps the saturated reds and yellows of Factorio sprites from bleeding at
// about the size of plain q85 (docs/architecture.md#preview-files). The image
// is piped to cwebp as a PPM, so the binary needs no PNG library, and every
// call is its own process: they run in parallel, and a runaway encode cannot
// take the API down.
package previewimage

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

const (
	// MediaType is the type of both stored files.
	MediaType = "image/webp"
	// ThumbnailMax bounds the thumbnail's longer side; smaller images keep
	// their size (never upscaled).
	ThumbnailMax = 800
	// Quality is the lossy WebP quality of both files.
	Quality = 80
	// MaxPixels and MaxBytes match the runner's artifact limits
	// (RUNNER_MAX_ARTIFACT_PIXELS, RUNNER_MAX_ARTIFACT_BYTES defaults).
	MaxPixels = 4096 * 4096
	MaxBytes  = 80 << 20

	// binary is cwebp's name on PATH.
	binary = "cwebp"
	// encodeTimeout bounds one cwebp run (a 16 MP preview takes seconds).
	encodeTimeout = 5 * time.Minute
)

// arguments read a PPM from stdin and write the WebP to stdout. Method 6 is
// the slowest and smallest; metadata is dropped.
var arguments = []string{
	"-quiet", "-q", fmt.Sprint(Quality), "-m", "6", "-sharp_yuv", "-metadata", "none", "-o", "-", "--", "-",
}

// ErrInvalidImage marks a source that is not a PNG this package accepts; it is
// permanent, so retrying the same bytes cannot help.
var ErrInvalidImage = errors.New("invalid preview image")

// Encoded is one preview's stored files. Full is nil when the preview is
// small enough to be its own thumbnail (HasFull false).
type Encoded struct {
	Full          []byte
	Thumbnail     []byte
	Width, Height int
}

// HasFull reports whether a width x height preview is stored as a full image
// besides its thumbnail: only when it is larger than ThumbnailMax. The API
// derives this from the stored dimensions; nothing else records it.
func HasFull(width, height int) bool { return max(width, height) > ThumbnailMax }

// Check reports whether the cwebp binary can run; the API refuses to start
// without it.
func Check() error {
	if output, err := exec.Command(binary, "-version").CombinedOutput(); err != nil {
		return fmt.Errorf("run %s: %w: %s", binary, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Encode decodes a PNG and encodes the thumbnail and, when HasFull, the full
// preview. The source's alpha is dropped: Factorio previews are opaque.
func Encode(ctx context.Context, source []byte) (Encoded, error) {
	if len(source) > MaxBytes {
		return Encoded{}, fmt.Errorf("%w: %d bytes exceeds %d", ErrInvalidImage, len(source), MaxBytes)
	}
	config, err := png.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return Encoded{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > MaxPixels {
		return Encoded{}, fmt.Errorf("%w: %dx%d exceeds %d pixels", ErrInvalidImage, config.Width, config.Height, MaxPixels)
	}
	decoded, err := png.Decode(bytes.NewReader(source))
	if err != nil {
		return Encoded{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}

	encoded := Encoded{Width: config.Width, Height: config.Height}
	if HasFull(config.Width, config.Height) {
		if encoded.Full, err = encodeWebP(ctx, decoded); err != nil {
			return Encoded{}, fmt.Errorf("encode full preview: %w", err)
		}
	}
	if encoded.Thumbnail, err = encodeThumbnail(ctx, decoded); err != nil {
		return Encoded{}, fmt.Errorf("encode thumbnail: %w", err)
	}
	return encoded, nil
}

func encodeThumbnail(ctx context.Context, source image.Image) ([]byte, error) {
	bounds := source.Bounds()
	width, height := ThumbnailSize(bounds.Dx(), bounds.Dy())
	scaled := source
	if width != bounds.Dx() || height != bounds.Dy() {
		target := image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Src, nil)
		scaled = target
	}
	return encodeWebP(ctx, scaled)
}

// encodeWebP runs cwebp on img, streaming it as a PPM.
func encodeWebP(ctx context.Context, img image.Image) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, encodeTimeout)
	defer cancel()

	reader, writer := io.Pipe()
	var out, stderr bytes.Buffer
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Stdin, command.Stdout, command.Stderr = reader, &out, &stderr
	go func() { writer.CloseWithError(writePPM(writer, img)) }()

	err := command.Run()
	reader.Close() // unblocks the writer when cwebp stopped reading early
	if err != nil {
		return nil, fmt.Errorf("run %s: %w: %s", binary, err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

// writePPM writes img as a binary PPM (P6), row by row so a 16 MP preview
// never needs a second full copy in memory.
func writePPM(w io.Writer, img image.Image) error {
	bounds := img.Bounds()
	buffered := bufio.NewWriterSize(w, 1<<20)
	if _, err := fmt.Fprintf(buffered, "P6\n%d %d\n255\n", bounds.Dx(), bounds.Dy()); err != nil {
		return err
	}
	row := make([]byte, 3*bounds.Dx())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		switch src := img.(type) {
		case *image.NRGBA:
			pixels := src.Pix[src.PixOffset(bounds.Min.X, y):]
			for x := range bounds.Dx() {
				row[3*x], row[3*x+1], row[3*x+2] = pixels[4*x], pixels[4*x+1], pixels[4*x+2]
			}
		case *image.RGBA:
			pixels := src.Pix[src.PixOffset(bounds.Min.X, y):]
			for x := range bounds.Dx() {
				row[3*x], row[3*x+1], row[3*x+2] = pixels[4*x], pixels[4*x+1], pixels[4*x+2]
			}
		default:
			for x := range bounds.Dx() {
				r, g, b, _ := img.At(bounds.Min.X+x, y).RGBA()
				row[3*x], row[3*x+1], row[3*x+2] = byte(r>>8), byte(g>>8), byte(b>>8)
			}
		}
		if _, err := buffered.Write(row); err != nil {
			return err
		}
	}
	return buffered.Flush()
}

// ThumbnailSize is the thumbnail's size for a full preview of width x height:
// fit within ThumbnailMax, keep the aspect ratio, never upscale. The API
// derives the thumbnail's dimensions from this instead of storing them.
func ThumbnailSize(width, height int) (int, int) {
	longer := max(width, height)
	if longer <= ThumbnailMax {
		return width, height
	}
	scale := func(side int) int {
		return max(1, (side*ThumbnailMax+longer/2)/longer)
	}
	return scale(width), scale(height)
}
