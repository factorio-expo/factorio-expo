package assets

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"factorio-expo/apps/expo-game-data/internal/spritesheet"
)

// staleExtensions are files earlier generator versions wrote into the asset
// directory; they are removed so a deploy does not ship unused sheets.
var staleExtensions = map[string]bool{".webp": true, ".png": true, ".jxl": true, ".json": true}

// Write replaces the sheet images in assetDirectory and writes the TypeScript
// coordinate table to sourceDirectory.
func Write(assetDirectory, sourceDirectory string, sheets map[string]spritesheet.GeneratedSheet, encoded map[string][]byte) error {
	if err := os.MkdirAll(assetDirectory, 0o755); err != nil {
		return fmt.Errorf("create generated icon assets directory: %w", err)
	}
	entries, err := os.ReadDir(assetDirectory)
	if err != nil {
		return fmt.Errorf("list generated icon assets: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && staleExtensions[filepath.Ext(entry.Name())] {
			if err := os.Remove(filepath.Join(assetDirectory, entry.Name())); err != nil {
				return fmt.Errorf("remove old icon asset: %w", err)
			}
		}
	}
	for sheetID := range sheets {
		content := encoded[sheetID]
		if len(content) == 0 {
			return fmt.Errorf("encoded WebP missing for %s sheet", sheetID)
		}
		path := filepath.Join(assetDirectory, sheetID+".webp")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return fmt.Errorf("write generated icon asset %q: %w", path, err)
		}
		log.Printf("generated icon asset %q with size %d kB", path, len(content)/1024)
	}
	iconData, err := spritesheet.TypeScriptData(sheets)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sourceDirectory, 0o755); err != nil {
		return fmt.Errorf("create generated icon data directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDirectory, "game-icons.generated.ts"), iconData, 0o644); err != nil {
		return fmt.Errorf("write generated icon coordinate data: %w", err)
	}
	return nil
}
