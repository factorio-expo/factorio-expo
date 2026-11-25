package modset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDependency(t *testing.T) {
	cases := map[string]Dependency{
		"base >= 2.1.0":       {Kind: Required, Name: "base", Operator: ">=", Version: "2.1.0"},
		"? space-age >= 2.1":  {Kind: Optional, Name: "space-age", Operator: ">=", Version: "2.1"},
		"(?) flib":            {Kind: HiddenOptional, Name: "flib"},
		"! factorioplus":      {Kind: Incompatible, Name: "factorioplus"},
		"~ k2so-assets=1.0.7": {Kind: NoLoadOrder, Name: "k2so-assets", Operator: "=", Version: "1.0.7"},
		"+ quality >= 2.1.0":  {Kind: Plus, Name: "quality", Operator: ">=", Version: "2.1.0"},
		"Squeak Through":      {Kind: Required, Name: "Squeak Through"},
	}
	for text, want := range cases {
		got, err := ParseDependency(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if got != want {
			t.Fatalf("%q = %+v, want %+v", text, got, want)
		}
	}
	if _, err := ParseDependency("base >= banana"); err == nil {
		t.Fatal("a bad version was accepted")
	}
}

func TestAllowsComparesNumerically(t *testing.T) {
	dependency, _ := ParseDependency("k2so-assets >= 1.0.10")
	for candidate, want := range map[string]bool{"1.0.9": false, "1.0.10": true, "1.1.0": true} {
		if got, _ := dependency.Allows(candidate); got != want {
			t.Fatalf("Allows(%s) = %v", candidate, got)
		}
	}
}

func TestReadBuiltinsOrdersDependenciesFirst(t *testing.T) {
	root := t.TempDir()
	writeInfo(t, root, "core", `{"name": "core"}`)
	writeInfo(t, root, "base", `{"name": "base", "title": "Base Mod", "version": "2.1.21", "dependencies": []}`)
	writeInfo(t, root, "quality", `{"name": "quality", "version": "2.1.21", "dependencies": ["base >= 2.1.0", "recycler >= 2.1.0"]}`)
	writeInfo(t, root, "recycler", `{"name": "recycler", "version": "2.1.21", "dependencies": ["base >= 2.1.0"]}`)
	writeInfo(t, root, "space-age", `{"name": "space-age", "version": "2.1.21", "dependencies": ["base", "+ quality >= 2.1.0"]}`)
	builtins, err := ReadBuiltins(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, builtin := range builtins {
		names = append(names, builtin.Name)
	}
	if want := []string{"base", "recycler", "quality", "space-age"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func writeInfo(t *testing.T, root, name, info string) {
	t.Helper()
	directory := filepath.Join(root, "data", name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "info.json"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fakeSource map[string][]Release

func (f fakeSource) Releases(_ context.Context, name string) ([]Release, error) {
	releases, ok := f[name]
	if !ok {
		return nil, fmt.Errorf("mod %q is not on the mod portal", name)
	}
	return releases, nil
}

var testBuiltins = []Builtin{
	{Name: "base", Version: "2.1.21"},
	{Name: "space-age", Version: "2.1.21", Dependencies: []Dependency{{Name: "base"}}},
}

func release(version, factorio string, dependencies ...string) Release {
	return Release{Version: version, FactorioVersion: factorio, Dependencies: dependencies}
}

func resolved(t *testing.T, source fakeSource, roots ...string) string {
	t.Helper()
	mods, err := (&Resolver{Source: source, Builtins: testBuiltins}).Resolve(context.Background(), roots)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, mod := range mods {
		parts = append(parts, mod.Name+"@"+mod.Version)
	}
	return strings.Join(parts, " ")
}

func TestResolvePicksNewestReleaseThePinnedGameRuns(t *testing.T) {
	source := fakeSource{
		"overhaul": {
			release("1.0.0", "2.0", "base >= 2.0.0"),
			release("2.0.18", "2.1", "base >= 2.1.0", "space-age", "assets >= 1.0.7"),
			release("2.0.19", "2.1", "base >= 2.1.22", "space-age"), // needs a newer game
		},
		"assets": {release("1.0.6", "2.1"), release("1.0.8", "2.1"), release("1.1.0", "2.0")},
	}
	if got, want := resolved(t, source, "overhaul"), "base@2.1.21 space-age@2.1.21 assets@1.0.8 overhaul@2.0.18"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveRepicksWhenADependentNarrowsTheVersion(t *testing.T) {
	source := fakeSource{
		"a":   {release("1.0.0", "2.1", "lib")},
		"b":   {release("1.0.0", "2.1", "lib < 2.0.0")},
		"lib": {release("1.5.0", "2.1"), release("2.1.0", "2.1")},
	}
	if got, want := resolved(t, source, "a", "b"), "base@2.1.21 a@1.0.0 b@1.0.0 lib@1.5.0"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveLoadsOptionalBuiltinsOnly(t *testing.T) {
	source := fakeSource{
		"loaders": {release("0.3.0", "2.1", "base >= 2.1.6", "? space-age >= 2.1.0", "? some-mod", "(?) quality")},
	}
	if got, want := resolved(t, source, "loaders"), "base@2.1.21 space-age@2.1.21 loaders@0.3.0"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveHonoursAModsetsVersionCondition(t *testing.T) {
	source := fakeSource{
		"planet":   {release("2.7.34", "2.1", "graphics >= 1.1.0"), release("2.9.6", "2.1", "base >= 2.1.22")},
		"graphics": {release("1.1.18", "2.1"), release("1.1.19", "2.1")},
	}
	if got, want := resolved(t, source, "planet", "graphics <= 1.1.18"), "base@2.1.21 graphics@1.1.18 planet@2.7.34"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveSkipsOptionalBuiltinsAnotherModRulesOut(t *testing.T) {
	source := fakeSource{
		"overhaul": {release("1.0.0", "2.1", "loaders", "! space-age")},
		"loaders":  {release("0.3.0", "2.1", "? space-age")},
	}
	if got, want := resolved(t, source, "overhaul"), "base@2.1.21 loaders@0.3.0 overhaul@1.0.0"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveFailsClearly(t *testing.T) {
	cases := map[string]fakeSource{
		"no release for Factorio 2.1": {"old": {release("1.0.0", "2.0")}},
		"incompatible with":           {"old": {release("1.0.0", "2.1", "! space-age", "other")}, "other": {release("1.0.0", "2.1", "space-age")}},
		"not on the mod portal":       {"old": {release("1.0.0", "2.1", "missing")}},
	}
	for want, source := range cases {
		_, err := (&Resolver{Source: source, Builtins: testBuiltins}).Resolve(context.Background(), []string{"old"})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v, want it to mention %q", err, want)
		}
	}
}
