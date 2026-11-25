package spritesheet

import (
	"fmt"
	"os"
	"path/filepath"

	"factorio-expo/apps/expo-game-data/internal/prototypes"
)

func qualityIcons(root string) ([]prototypes.Prototype, error) {
	qualities := []string{"normal", "uncommon", "rare", "epic", "legendary"}
	icons := make([]prototypes.Prototype, 0, len(qualities)*2)
	found := 0
	for _, quality := range qualities {
		module := "quality"
		if quality == "normal" {
			module = "base"
		}
		relative := "graphics/icons/quality-" + quality + ".png"
		path := filepath.Join(root, "data", module, relative)
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("stat quality icon %q: %w", path, err)
			}
			continue
		}
		if quality != "normal" {
			found++
		}
		source := "__" + module + "__/" + relative
		icons = append(icons,
			prototypes.Prototype{Type: "quality", Name: quality, Icon: source},
			prototypes.Prototype{
				Type: "quality-badge", Name: quality + "-badge", Icon: source,
				IconLayers: []prototypes.IconLayer{{
					Icon: source, Scale: 0.2, Shift: []float64{-16, 16},
				}},
			},
		)
	}
	if found != 0 && found != len(qualities)-1 {
		return nil, fmt.Errorf("Factorio installation has only %d of %d quality module icons", found, len(qualities)-1)
	}
	return icons, nil
}
