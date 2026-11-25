package spritesheet

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/draw"
	"math"
	"path/filepath"
	"sort"

	"factorio-expo/apps/expo-game-data/internal/prototypes"
)

const IconSize = 64

// Icon is one lookup: a prototype name, the lookup types it serves and the
// cell it points to. Sheet can name an earlier sheet when the pixels match a
// cell already drawn there.
type Icon struct {
	Name       string
	Sheet      string
	X, Y       int
	Prototypes []string
}

type Manifest struct {
	Columns int
	Rows    int
	Count   int // cells drawn on this sheet
	Icons   []Icon
}

// cellRef locates an already drawn cell, keyed by its pixel hash.
type cellRef struct {
	sheet string
	x, y  int
}

type selectedIcon struct {
	name     string
	source   string
	layers   []prototypes.IconLayer
	priority int
	types    map[string]struct{}
}

// generate draws one sheet. Icons whose pixels match a cell in drawn (from
// earlier sheets) or earlier on this sheet reuse that cell; drawn gains this
// sheet's cells.
func generate(root string, prototypesToGenerate []prototypes.Prototype, sheetID string, drawn map[[32]byte]cellRef) (*image.RGBA, Manifest, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, Manifest{}, fmt.Errorf("resolve game root: %w", err)
	}

	selected := make(map[string]*selectedIcon)
	for _, prototype := range prototypesToGenerate {
		category := categoryName(prototype)
		if category == "" {
			continue
		}
		icon := selected[prototype.Name]
		if icon == nil {
			icon = &selectedIcon{name: prototype.Name, priority: sourcePriority(prototype), types: make(map[string]struct{})}
			selected[prototype.Name] = icon
		}
		icon.types[category] = struct{}{}
		candidateLayers, currentLayers := len(prototype.IconLayers), len(icon.layers)
		if icon.source == "" || candidateLayers > currentLayers ||
			(candidateLayers == currentLayers && sourcePriority(prototype) < icon.priority) {
			icon.source = prototype.Icon
			icon.layers = prototype.IconLayers
			icon.priority = sourcePriority(prototype)
		}
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		leftOrder, rightOrder := iconOrder(selected[names[i]].types), iconOrder(selected[names[j]].types)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return names[i] < names[j]
	})

	cells := make([]*image.RGBA, 0, len(names))
	hashes := make([][32]byte, 0, len(names))
	ownCells := make(map[[32]byte]int)
	// placement[i] is a cell index on this sheet, or -1 with alias[i] set.
	placement := make([]int, len(names))
	alias := make([]cellRef, len(names))
	for index, name := range names {
		entry := selected[name]
		layers, err := resolveLayers(root, entry.source, entry.layers)
		if err != nil {
			return nil, Manifest{}, fmt.Errorf("%s: %w", name, err)
		}
		icon, err := compositeIcon(layers)
		if err != nil {
			return nil, Manifest{}, fmt.Errorf("compose icon %q: %w", name, err)
		}
		hash := sha256.Sum256(icon.Pix)
		if ref, exists := drawn[hash]; exists {
			placement[index], alias[index] = -1, ref
			continue
		}
		if cell, exists := ownCells[hash]; exists {
			placement[index] = cell
			continue
		}
		ownCells[hash] = len(cells)
		placement[index] = len(cells)
		cells = append(cells, icon)
		hashes = append(hashes, hash)
	}

	manifest := Manifest{Count: len(cells)}
	manifest.Columns = max(1, int(math.Ceil(math.Sqrt(float64(len(cells))))))
	manifest.Rows = max(1, (len(cells)+manifest.Columns-1)/manifest.Columns)
	sheet := image.NewRGBA(image.Rect(0, 0, manifest.Columns*IconSize, manifest.Rows*IconSize))
	for index, icon := range cells {
		x, y := (index%manifest.Columns)*IconSize, (index/manifest.Columns)*IconSize
		draw.Draw(sheet, image.Rect(x, y, x+IconSize, y+IconSize), icon, image.Point{}, draw.Src)
		drawn[hashes[index]] = cellRef{sheet: sheetID, x: x, y: y}
	}
	manifest.Icons = make([]Icon, 0, len(names))
	for index, name := range names {
		ref := alias[index]
		if placement[index] >= 0 {
			cell := placement[index]
			ref = cellRef{sheet: sheetID, x: (cell % manifest.Columns) * IconSize, y: (cell / manifest.Columns) * IconSize}
		}
		prototypeTypes := make([]string, 0, len(selected[name].types))
		for prototypeType := range selected[name].types {
			prototypeTypes = append(prototypeTypes, prototypeType)
		}
		sort.Slice(prototypeTypes, func(i, j int) bool {
			return prototypeTypeOrder(prototypeTypes[i]) < prototypeTypeOrder(prototypeTypes[j])
		})
		manifest.Icons = append(manifest.Icons, Icon{Name: name, Sheet: ref.sheet, X: ref.x, Y: ref.y, Prototypes: prototypeTypes})
	}
	return sheet, manifest, nil
}
