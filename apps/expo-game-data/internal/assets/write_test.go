package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"factorio-expo/apps/expo-game-data/internal/spritesheet"
)

func TestWriteReplacesStaticAssets(t *testing.T) {
	directory := t.TempDir()
	sourceDirectory := filepath.Join(directory, "src", "data")
	assetDirectory := filepath.Join(directory, "public", "game-icons")
	if err := os.MkdirAll(assetDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"fluids.webp", "fluids.png", "fluids.jxl", "fluids.json"} {
		if err := os.WriteFile(filepath.Join(assetDirectory, stale), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sheets := map[string]spritesheet.GeneratedSheet{
		spritesheet.CoreSheet: {Manifest: spritesheet.Manifest{
			Columns: 1, Rows: 1,
			Icons: []spritesheet.Icon{{Name: "water", Sheet: spritesheet.CoreSheet, Prototypes: []string{"fluid"}}},
		}},
		spritesheet.RecyclingSheet: {Manifest: spritesheet.Manifest{Columns: 1, Rows: 1}},
	}
	encoded := map[string][]byte{spritesheet.CoreSheet: []byte("core"), spritesheet.RecyclingSheet: []byte("recycling")}
	if err := Write(assetDirectory, sourceDirectory, sheets, encoded); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(assetDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if got := strings.Join(names, ","); got != "core.webp,recycling.webp" {
		t.Fatalf("asset files = %s, want only the new WebP sheets", got)
	}
	generatedTypeScript, err := os.ReadFile(filepath.Join(sourceDirectory, "game-icons.generated.ts"))
	if err != nil {
		t.Fatalf("read generated TypeScript: %v", err)
	}
	for _, want := range []string{`"fluid":{"water":[0,0,"core"]}`, `"core":{"columns":1,"rows":1}`} {
		if !strings.Contains(string(generatedTypeScript), want) {
			t.Errorf("generated TypeScript does not contain %q", want)
		}
	}
}
