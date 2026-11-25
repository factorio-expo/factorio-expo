package imageformats

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
)

// webpArguments are lossy WebP settings measured against the alternatives on
// the real sheets (see the expo-game-data README): quality 82 with sharp YUV and
// lossless alpha scores ~82 SSIMULACRA2, at a third of lossless WebP's size
// and a quarter of JPEG XL's decode time for the same size and quality.
var webpArguments = []string{"-quiet", "-q", "82", "-m", "6", "-sharp_yuv", "-alpha_q", "100", "-metadata", "none"}

// EncodeWebP encodes a sheet with cwebp.
func EncodeWebP(sheet image.Image) ([]byte, error) {
	directory, err := os.MkdirTemp("", "expo-game-data-")
	if err != nil {
		return nil, fmt.Errorf("create image encoding directory: %w", err)
	}
	defer os.RemoveAll(directory)

	inputPath := filepath.Join(directory, "sheet.png")
	input, err := os.Create(inputPath)
	if err != nil {
		return nil, fmt.Errorf("create sheet PNG: %w", err)
	}
	encodeErr := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(input, sheet)
	closeErr := input.Close()
	if encodeErr != nil {
		return nil, fmt.Errorf("encode sheet PNG: %w", encodeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("write sheet PNG: %w", closeErr)
	}

	outputPath := filepath.Join(directory, "sheet.webp")
	arguments := append(append([]string{}, webpArguments...), inputPath, "-o", outputPath)
	if output, err := exec.Command("cwebp", arguments...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("run cwebp: %w: %s", err, output)
	}
	webp, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read encoded WebP sheet: %w", err)
	}
	return webp, nil
}
