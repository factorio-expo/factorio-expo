package modset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Builtin is one of the game's own mods (base, elevated-rails, quality,
// recycler, space-age), read from its info.json under the game's data/.
type Builtin struct {
	Name         string
	Title        string
	Version      string
	Dependencies []Dependency
}

type infoJSON struct {
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Version      string   `json:"version"`
	Dependencies []string `json:"dependencies"`
}

// ReadBuiltins lists the built-in mods of the game at root, dependencies
// before dependents (base first). "core" is the engine, not a mod.
func ReadBuiltins(root string) ([]Builtin, error) {
	paths, err := filepath.Glob(filepath.Join(root, "data", "*", "info.json"))
	if err != nil {
		return nil, err
	}
	byName := map[string]Builtin{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var info infoJSON
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if info.Name == "core" {
			continue
		}
		builtin := Builtin{Name: info.Name, Title: info.Title, Version: info.Version}
		for _, text := range info.Dependencies {
			dependency, err := ParseDependency(text)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			builtin.Dependencies = append(builtin.Dependencies, dependency)
		}
		byName[info.Name] = builtin
	}
	if _, ok := byName["base"]; !ok {
		return nil, fmt.Errorf("no base mod under %s", filepath.Join(root, "data"))
	}
	return orderBuiltins(byName)
}

// orderBuiltins sorts topologically by required dependencies, ties by name.
func orderBuiltins(byName map[string]Builtin) ([]Builtin, error) {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)
	ordered := make([]Builtin, 0, len(names))
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("built-in mods depend on each other in a cycle at %q", name)
		case 2:
			return nil
		}
		state[name] = 1
		builtin := byName[name]
		for _, dependency := range builtin.Dependencies {
			if !dependency.IsRequired() {
				continue
			}
			if _, ok := byName[dependency.Name]; !ok {
				return fmt.Errorf("built-in mod %q needs %q, which the game does not have", name, dependency.Name)
			}
			if err := visit(dependency.Name); err != nil {
				return err
			}
		}
		state[name] = 2
		ordered = append(ordered, builtin)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
