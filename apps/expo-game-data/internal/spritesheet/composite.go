package spritesheet

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"factorio-expo/apps/expo-game-data/internal/prototypes"
)

const insetScaleMultiplier = 2

type imageLayer struct {
	absolute string
	relative string
	scale    float64
	shift    []float64
	tint     prototypes.Color
}

// resolveLayers maps prototype icon layers to files under the game root.
func resolveLayers(root, source string, layers []prototypes.IconLayer) ([]imageLayer, error) {
	if len(layers) == 0 {
		layers = []prototypes.IconLayer{{Icon: source}}
	}
	resolved := make([]imageLayer, 0, len(layers))
	for _, layer := range layers {
		absolute, relative, err := resolveIconPath(root, layer.Icon)
		if err != nil {
			return nil, fmt.Errorf("resolve icon %q: %w", layer.Icon, err)
		}
		scale := layer.Scale
		if scale <= 0 {
			scale = 1
		} else {
			scale = min(1, scale*insetScaleMultiplier)
		}
		resolved = append(resolved, imageLayer{
			absolute: absolute, relative: relative, scale: scale,
			shift: layer.Shift, tint: layer.Tint,
		})
	}
	return resolved, nil
}

func compositeIcon(layers []imageLayer) (*image.RGBA, error) {
	composite := image.NewRGBA(image.Rect(0, 0, IconSize, IconSize))
	for _, layer := range layers {
		file, err := os.Open(layer.absolute)
		if err != nil {
			return nil, fmt.Errorf("open icon layer %q: %w", layer.relative, err)
		}
		source, _, decodeErr := image.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode icon layer %q: %w", layer.relative, decodeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close icon layer %q: %w", layer.relative, closeErr)
		}
		cropped := image.NewRGBA(image.Rect(0, 0, IconSize, IconSize))
		draw.Draw(cropped, cropped.Bounds(), source, source.Bounds().Min, draw.Src)
		scaled := resizeNearest(cropped, layer.scale)
		x, y := (IconSize-scaled.Bounds().Dx())/2, (IconSize-scaled.Bounds().Dy())/2
		if len(layer.shift) > 0 {
			x += int(math.Round(layer.shift[0]))
		}
		if len(layer.shift) > 1 {
			y += int(math.Round(layer.shift[1]))
		}
		tinted := tintImage(scaled, layer.tint)
		draw.Draw(composite, tinted.Bounds().Add(image.Pt(x, y)), tinted, image.Point{}, draw.Over)
	}
	return composite, nil
}

func tintImage(source *image.RGBA, tint prototypes.Color) *image.RGBA {
	if !tint.Set {
		return source
	}
	tinted := image.NewRGBA(source.Bounds())
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			pixel := source.RGBAAt(x, y)
			red, green, blue, alpha := tint.Red, tint.Green, tint.Blue, tint.Alpha
			tinted.SetRGBA(x, y, color.RGBA{
				R: uint8(min(255, math.Round(float64(pixel.R)*red))),
				G: uint8(min(255, math.Round(float64(pixel.G)*green))),
				B: uint8(min(255, math.Round(float64(pixel.B)*blue))),
				A: uint8(min(255, math.Round(float64(pixel.A)*alpha))),
			})
		}
	}
	return tinted
}

func resizeNearest(source *image.RGBA, scale float64) *image.RGBA {
	width := max(1, int(math.Round(float64(source.Bounds().Dx())*scale)))
	height := max(1, int(math.Round(float64(source.Bounds().Dy())*scale)))
	resized := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		sourceY := y * source.Bounds().Dy() / height
		for x := range width {
			sourceX := x * source.Bounds().Dx() / width
			resized.SetRGBA(x, y, source.RGBAAt(sourceX, sourceY))
		}
	}
	return resized
}

func resolveIconPath(root, source string) (string, string, error) {
	module, relative := "base", source
	if strings.HasPrefix(source, "__") {
		end := strings.Index(source[2:], "__")
		if end < 0 {
			return "", "", fmt.Errorf("invalid Factorio module path")
		}
		module = source[2 : end+2]
		relative = strings.TrimPrefix(source[end+4:], "/")
	}
	if filepath.IsAbs(relative) || strings.Contains(relative, "..") {
		return "", "", fmt.Errorf("icon path is not relative")
	}
	path := filepath.Join(root, "data", module, filepath.FromSlash(relative))
	displayPath := filepath.ToSlash(filepath.Join("data", module, relative))
	if _, err := os.Stat(path); err != nil {
		return "", "", err
	}
	return path, displayPath, nil
}
