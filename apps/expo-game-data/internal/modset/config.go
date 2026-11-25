package modset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// Config is the maintainer's hand-edited modset list (modsets.json). It names
// portal mods only; versions and dependencies are resolved against the mod
// portal for the pinned game.
type Config struct {
	Modsets []Entry `json:"modsets"`
}

// Entry is one modset to detect: an id, a display title and the mods that
// make it up (dependencies are added automatically).
type Entry struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Mods  []string `json:"mods"`
}

var modsetNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ReadConfig reads and checks the modset list. Names must not reuse a
// built-in mod's name.
func ReadConfig(path string, builtins []Builtin) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read modsets: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode modsets %s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, builtin := range builtins {
		seen[builtin.Name] = true
	}
	for _, entry := range config.Modsets {
		if !modsetNamePattern.MatchString(entry.Name) {
			return Config{}, fmt.Errorf("modset name %q: use lowercase letters, digits and dashes", entry.Name)
		}
		if seen[entry.Name] {
			return Config{}, fmt.Errorf("modset name %q is used twice or is a built-in mod", entry.Name)
		}
		seen[entry.Name] = true
		if entry.Title == "" {
			return Config{}, fmt.Errorf("modset %q has no title", entry.Name)
		}
		if len(entry.Mods) == 0 {
			return Config{}, fmt.Errorf("modset %q lists no mods", entry.Name)
		}
	}
	return config, nil
}
