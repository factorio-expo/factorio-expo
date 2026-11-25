package pipeline

import (
	"fmt"
	"log"

	"factorio-expo/apps/expo-game-data/internal/assets"
	"factorio-expo/apps/expo-game-data/internal/imageformats"
	"factorio-expo/apps/expo-game-data/internal/prototypes"
	"factorio-expo/apps/expo-game-data/internal/spritesheet"
)

// writeIcons draws the sprite sheets from a dump and writes them with their
// TypeScript lookup table, then writes the icon picker lists.
func writeIcons(config Config, dumpPath string) error {
	if config.SourceDir == "" {
		return fmt.Errorf("SOURCE_OUTPUT_DIR is required when ASSET_OUTPUT_DIR is set")
	}
	raw, err := prototypes.ReadDump(dumpPath)
	if err != nil {
		return err
	}
	catalog, err := prototypes.Extract(raw)
	if err != nil {
		return err
	}
	log.Printf("creating icon sheets...")
	sheets, err := spritesheet.GenerateSheets(config.FactorioRoot, catalog)
	if err != nil {
		return fmt.Errorf("generate game icon sheets: %w", err)
	}
	encoded := make(map[string][]byte, len(sheets))
	for sheetID, sheet := range sheets {
		log.Printf("encoding %s sheet...", sheetID)
		webp, err := imageformats.EncodeWebP(sheet.Image)
		if err != nil {
			return fmt.Errorf("encode %s icon sheet: %w", sheetID, err)
		}
		encoded[sheetID] = webp
		log.Printf("drew %d %s cells (%d lookups) into a %dx%d grid", sheet.Manifest.Count, sheetID, len(sheet.Manifest.Icons), sheet.Manifest.Columns, sheet.Manifest.Rows)
	}
	if err := assets.Write(config.AssetDir, config.SourceDir, sheets, encoded); err != nil {
		return err
	}
	log.Printf("wrote static icon assets to %s", config.AssetDir)
	picker, err := prototypes.ExtractPicker(raw)
	if err != nil {
		return fmt.Errorf("build icon picker lists: %w", err)
	}
	if err := assets.WritePicker(config.SourceDir, picker); err != nil {
		return err
	}
	log.Printf("wrote icon picker lists to %s", config.SourceDir)
	return nil
}
