package modset

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-version"
)

// Release is one published version of a portal mod.
type Release struct {
	Version         string
	FactorioVersion string
	Dependencies    []string
	DownloadURL     string
	FileName        string
	SHA1            string
}

// Source lists a portal mod's releases (portal.Client).
type Source interface {
	Releases(ctx context.Context, name string) ([]Release, error)
}

// Mod is one mod picked for a dump. Release is nil for built-in mods.
type Mod struct {
	Name    string
	Version string
	Builtin bool
	Release *Release
}

// Resolver picks mod versions for the pinned game.
type Resolver struct {
	Source   Source
	Builtins []Builtin
	// releases caches Source answers for one run.
	releases map[string][]Release
}

// maxRounds bounds re-picking when a later constraint rules out an earlier
// pick; real mod lists settle in two or three rounds.
const maxRounds = 20

// Resolve returns roots and every dependency they load (closure), built-in mods first
// (in the game's order), then portal mods by name. Each portal mod gets its
// newest release made for the pinned game's major.minor whose built-in
// requirements the pinned game meets and that every dependent allows.
//
// Roots are mod names, optionally with a version condition in info.json
// syntax ("muluna-graphics <= 1.1.18") for mods whose own scripts demand a
// pairing the portal metadata does not state.
func (r *Resolver) Resolve(ctx context.Context, roots []string) ([]Mod, error) {
	rootDependencies, err := parseAll("modset", roots)
	if err != nil {
		return nil, err
	}
	for _, root := range rootDependencies {
		if root.Kind != Required {
			return nil, fmt.Errorf("modset mod %q: list a mod with an optional version condition, no prefix", root.String())
		}
	}
	builtins := map[string]Builtin{}
	for _, builtin := range r.Builtins {
		builtins[builtin.Name] = builtin
	}
	gameVersion := builtins["base"].Version
	picked := map[string]*Release{}
	for range maxRounds {
		needed, constraints, err := r.closure(rootDependencies, builtins, picked)
		if err != nil {
			return nil, err
		}
		changed := false
		for name := range picked {
			if !needed[name] {
				delete(picked, name)
				changed = true
			}
		}
		for name := range needed {
			if _, ok := builtins[name]; ok {
				continue
			}
			release, err := r.pick(ctx, name, constraints[name], builtins, gameVersion)
			if err != nil {
				return nil, err
			}
			if current := picked[name]; current == nil || current.Version != release.Version {
				picked[name] = release
				changed = true
			}
		}
		if !changed {
			if err := checkIncompatible(needed, builtins, picked); err != nil {
				return nil, err
			}
			return modList(needed, r.Builtins, picked), nil
		}
	}
	return nil, fmt.Errorf("mods %s: versions did not settle after %d rounds", strings.Join(roots, ", "), maxRounds)
}

// closure walks required dependencies from roots through built-in info.json
// and the current picks, collecting each mod's version conditions. Then it
// adds the built-in mods some mod lists as plain optional ("?"), unless a mod
// in the set is incompatible with one of them or what they need: a mod's
// optional Space Age content is worth dumping, but not over another mod's
// "! space-age".
func (r *Resolver) closure(
	roots []Dependency, builtins map[string]Builtin, picked map[string]*Release,
) (map[string]bool, map[string][]Dependency, error) {
	needed := map[string]bool{}
	constraints := map[string][]Dependency{}
	excluded := map[string]bool{}
	var optional []Dependency
	// Factorio always loads base, listed or not.
	queue := []string{"base"}
	for _, root := range roots {
		queue = append(queue, root.Name)
		if root.Operator != "" {
			constraints[root.Name] = append(constraints[root.Name], root)
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if needed[name] {
			continue
		}
		needed[name] = true
		var dependencies []Dependency
		if builtin, ok := builtins[name]; ok {
			dependencies = builtin.Dependencies
		} else if release := picked[name]; release != nil {
			parsed, err := parseAll(name, release.Dependencies)
			if err != nil {
				return nil, nil, err
			}
			dependencies = parsed
		}
		for _, dependency := range dependencies {
			switch {
			case dependency.IsRequired():
				constraints[dependency.Name] = append(constraints[dependency.Name], dependency)
				queue = append(queue, dependency.Name)
			case dependency.Kind == Incompatible:
				excluded[dependency.Name] = true
			case loadsOptional(dependency, builtins):
				optional = append(optional, dependency)
			}
		}
	}
	for _, dependency := range optional {
		with := builtinClosure(dependency.Name, builtins)
		if slices.ContainsFunc(with, func(name string) bool { return excluded[name] }) {
			continue
		}
		constraints[dependency.Name] = append(constraints[dependency.Name], dependency)
		for _, name := range with {
			needed[name] = true
		}
	}
	return needed, constraints, nil
}

// loadsOptional reports whether an optional dependency is worth loading: a
// plain optional ("?") one on a built-in mod. Hidden optional ones ("(?)")
// only order loading: Krastorio 2 lists Space Age that way, and its Space Age
// variant is a separate mod.
func loadsOptional(dependency Dependency, builtins map[string]Builtin) bool {
	_, isBuiltin := builtins[dependency.Name]
	return isBuiltin && dependency.Kind == Optional
}

// builtinClosure is a built-in mod and the built-in mods it requires.
func builtinClosure(name string, builtins map[string]Builtin) []string {
	var out []string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
		for _, dependency := range builtins[name].Dependencies {
			if dependency.IsRequired() {
				visit(dependency.Name)
			}
		}
	}
	visit(name)
	return out
}

func (r *Resolver) pick(
	ctx context.Context, name string, constraints []Dependency, builtins map[string]Builtin, gameVersion string,
) (*Release, error) {
	releases, err := r.releasesOf(ctx, name)
	if err != nil {
		return nil, err
	}
	gameMinor := majorMinor(gameVersion)
	var best *Release
	var bestVersion *version.Version
	for i := range releases {
		release := &releases[i]
		if release.FactorioVersion != gameMinor {
			continue
		}
		ok, err := allowed(release, constraints, builtins)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		candidate, err := version.NewVersion(release.Version)
		if err != nil {
			return nil, fmt.Errorf("mod %s release %q: %w", name, release.Version, err)
		}
		if bestVersion == nil || candidate.GreaterThan(bestVersion) {
			best, bestVersion = release, candidate
		}
	}
	if best == nil {
		var conditions []string
		for _, constraint := range constraints {
			conditions = append(conditions, constraint.String())
		}
		return nil, fmt.Errorf("mod %s has no release for Factorio %s (%s) that meets [%s] and the game's built-in mod versions",
			name, gameMinor, gameVersion, strings.Join(conditions, ", "))
	}
	return best, nil
}

// allowed checks a release against its dependents' conditions and its own
// requirements on built-in mods (whose versions are fixed by the game).
func allowed(release *Release, constraints []Dependency, builtins map[string]Builtin) (bool, error) {
	for _, constraint := range constraints {
		ok, err := constraint.Allows(release.Version)
		if err != nil || !ok {
			return false, err
		}
	}
	for _, text := range release.Dependencies {
		dependency, err := ParseDependency(text)
		if err != nil {
			return false, err
		}
		builtin, isBuiltin := builtins[dependency.Name]
		if !isBuiltin || !(dependency.IsRequired() || loadsOptional(dependency, builtins)) {
			continue
		}
		ok, err := dependency.Allows(builtin.Version)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (r *Resolver) releasesOf(ctx context.Context, name string) ([]Release, error) {
	if releases, ok := r.releases[name]; ok {
		return releases, nil
	}
	releases, err := r.Source.Releases(ctx, name)
	if err != nil {
		return nil, err
	}
	if r.releases == nil {
		r.releases = map[string][]Release{}
	}
	r.releases[name] = releases
	return releases, nil
}

func checkIncompatible(needed map[string]bool, builtins map[string]Builtin, picked map[string]*Release) error {
	for name := range needed {
		var dependencies []Dependency
		if builtin, ok := builtins[name]; ok {
			dependencies = builtin.Dependencies
		} else {
			parsed, err := parseAll(name, picked[name].Dependencies)
			if err != nil {
				return err
			}
			dependencies = parsed
		}
		for _, dependency := range dependencies {
			if dependency.Kind == Incompatible && needed[dependency.Name] {
				return fmt.Errorf("mod %s is incompatible with %s, which the same modset needs", name, dependency.Name)
			}
		}
	}
	return nil
}

func modList(needed map[string]bool, order []Builtin, picked map[string]*Release) []Mod {
	mods := make([]Mod, 0, len(needed))
	for _, builtin := range order {
		if needed[builtin.Name] {
			mods = append(mods, Mod{Name: builtin.Name, Version: builtin.Version, Builtin: true})
		}
	}
	names := make([]string, 0, len(picked))
	for name := range picked {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		mods = append(mods, Mod{Name: name, Version: picked[name].Version, Release: picked[name]})
	}
	return mods
}

func parseAll(owner string, texts []string) ([]Dependency, error) {
	dependencies := make([]Dependency, 0, len(texts))
	for _, text := range texts {
		dependency, err := ParseDependency(text)
		if err != nil {
			return nil, fmt.Errorf("mod %s: %w", owner, err)
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, nil
}

// majorMinor turns "2.1.21" into "2.1", the form of info.json factorio_version.
func majorMinor(full string) string {
	parts := strings.SplitN(full, ".", 3)
	if len(parts) < 2 {
		return full
	}
	return parts[0] + "." + parts[1]
}
