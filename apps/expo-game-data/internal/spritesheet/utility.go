package spritesheet

import (
	"fmt"
	"os"
	"path/filepath"

	"factorio-expo/apps/expo-game-data/internal/prototypes"
)

// utilitySprites are the engine's own rich-text images (utility sprites, not
// prototypes), published under the "utility" lookup type by rich-text tag.
var utilitySprites = []struct{ name, path string }{
	// [space-age]: the utility sprite space_age_icon.
	{name: "space-age", path: "graphics/icons/space-age.png"},
}

func utilityIcons(root string) ([]prototypes.Prototype, error) {
	icons := make([]prototypes.Prototype, 0, len(utilitySprites))
	for _, sprite := range utilitySprites {
		path := filepath.Join(root, "data", "core", sprite.path)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat utility sprite %q: %w", path, err)
		}
		icons = append(icons, prototypes.Prototype{Type: "utility", Name: sprite.name, Icon: "__core__/" + sprite.path})
	}
	return icons, nil
}
