package dump

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"factorio-expo/apps/expo-game-data/internal/modset"
)

func TestWriteModListEnablesExactlyTheDumpsMods(t *testing.T) {
	directory := t.TempDir()
	zip := filepath.Join(directory, "aai-loaders_0.3.0.zip")
	if err := os.WriteFile(zip, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaders := modset.Mod{Name: "aai-loaders", Version: "0.3.0"}
	runner := &Runner{
		Builtins: []modset.Builtin{{Name: "base"}, {Name: "quality"}, {Name: "space-age"}},
		Files:    map[string]string{FileKey(loaders): zip},
	}
	modDir := filepath.Join(directory, "run-mods")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mods := []modset.Mod{{Name: "base", Version: "2.1.21", Builtin: true}, loaders}
	if err := runner.writeModList(modDir, mods); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modDir, "aai-loaders_0.3.0.zip")); err != nil {
		t.Fatalf("zip not in the mod directory: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(modDir, "mod-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Mods []modListEntry `json:"mods"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	want := []modListEntry{
		{Name: "aai-loaders", Enabled: true},
		{Name: "base", Enabled: true},
		{Name: "quality", Enabled: false},
		{Name: "space-age", Enabled: false},
	}
	if !reflect.DeepEqual(list.Mods, want) {
		t.Fatalf("mod list = %+v, want %+v", list.Mods, want)
	}
}

func TestDumpReusesAKeptDump(t *testing.T) {
	directory := t.TempDir()
	mods := []modset.Mod{{Name: "base", Version: "2.1.21", Builtin: true}}
	kept := filepath.Join(directory, "dumps", Fingerprint(mods)+".json")
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No Factorio binary: a dump that ran would fail.
	runner := &Runner{Bin: filepath.Join(directory, "missing"), WriteDir: directory}
	path, err := runner.Dump(context.Background(), mods)
	if err != nil || path != kept {
		t.Fatalf("dump = %s, %v; want the kept %s", path, err, kept)
	}
	if Fingerprint(mods) == Fingerprint([]modset.Mod{{Name: "base", Version: "2.1.22", Builtin: true}}) {
		t.Fatal("another game version shares the fingerprint")
	}
}
