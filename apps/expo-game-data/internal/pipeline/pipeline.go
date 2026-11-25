// Package pipeline runs the whole data generation: resolve and download the
// modsets' mods, dump each modset and the built-in mods it builds on, write
// the mod name table, then draw the icon sheets from the vanilla dump.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"factorio-expo/apps/expo-game-data/internal/dump"
	"factorio-expo/apps/expo-game-data/internal/modset"
	"factorio-expo/apps/expo-game-data/internal/portal"
	"factorio-expo/apps/expo-game-data/internal/prototypes"
	"factorio-expo/internal/modtable"
)

// Config is everything a run needs; cmd/expo-game-data fills it from the
// environment.
type Config struct {
	FactorioRoot   string
	FactorioBin    string
	FactorioConfig string
	// DataDir is the Factorio write-data directory; dumps and downloaded
	// mods are kept under it between runs.
	DataDir string
	// ModsetsFile is the hand-edited modset list.
	ModsetsFile string
	// ModTablePath is where the mod name table is written (empty: skip).
	ModTablePath string
	// AssetDir and SourceDir receive the icon sheets and their TypeScript
	// table (empty AssetDir: skip icons).
	AssetDir  string
	SourceDir string
	Portal    *portal.Client
}

// plan is one modset to dump: its mods and the built-in mods it builds on,
// whose names it does not count as its own.
type plan struct {
	entry  modtable.Modset
	mods   []modset.Mod
	parent []modset.Mod
}

// Run generates everything. It stops at the first error.
func Run(ctx context.Context, config Config) error {
	builtins, err := modset.ReadBuiltins(config.FactorioRoot)
	if err != nil {
		return err
	}
	modsets, err := modset.ReadConfig(config.ModsetsFile, builtins)
	if err != nil {
		return err
	}
	resolver := &modset.Resolver{Source: config.Portal, Builtins: builtins}
	plans, err := planModsets(ctx, resolver, builtins, modsets)
	if err != nil {
		return err
	}
	runner := &dump.Runner{
		Bin: config.FactorioBin, Config: config.FactorioConfig, WriteDir: config.DataDir,
		Builtins: builtins, Files: map[string]string{},
	}
	if err := download(ctx, config.Portal, plans, runner.Files); err != nil {
		return err
	}

	// Every modset is dumped before failing, so one run shows every modset
	// that does not load; the table is written only when all of them do.
	table := modtable.Table{FactorioVersion: builtins[0].Version}
	var failures []error
	for _, plan := range plans {
		log.Printf("processing modset %s...", plan.entry.Name)
		entry, err := distill(ctx, runner, plan)
		if err != nil {
			log.Printf("modset %s failed: %v", plan.entry.Name, err)
			failures = append(failures, fmt.Errorf("modset %s: %w", plan.entry.Name, err))
			continue
		}
		log.Printf("modset %s adds %d entities and %d recipes", entry.Name, len(entry.Entities), len(entry.Recipes))
		table.Modsets = append(table.Modsets, entry)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d modsets failed, nothing written:\n%w", len(failures), errors.Join(failures...))
	}
	if config.ModTablePath != "" {
		data, err := modtable.Marshal(table)
		if err != nil {
			return err
		}
		if err := writeFile(config.ModTablePath, data); err != nil {
			return err
		}
		log.Printf("wrote mod table to %s", config.ModTablePath)
	}

	if config.AssetDir == "" {
		return nil
	}
	vanilla := make([]modset.Mod, 0, len(builtins))
	for _, builtin := range builtins {
		vanilla = append(vanilla, modset.Mod{Name: builtin.Name, Version: builtin.Version, Builtin: true})
	}
	path, err := runner.Dump(ctx, vanilla)
	if err != nil {
		return err
	}
	return writeIcons(config, path)
}

// planModsets lists the built-in mods (base first) and then the configured
// modsets, each with its resolved mods and parent.
func planModsets(
	ctx context.Context, resolver *modset.Resolver, builtins []modset.Builtin, modsets modset.Config,
) ([]plan, error) {
	var plans []plan
	for _, builtin := range builtins {
		mods, err := resolver.Resolve(ctx, []string{builtin.Name})
		if err != nil {
			return nil, err
		}
		plans = append(plans, newPlan(modtable.Modset{Name: builtin.Name, Title: builtin.Title, Builtin: true}, mods, builtin.Name))
	}
	// Resolve every modset before failing, so one run lists every modset
	// the list needs to drop or fix.
	var failures []error
	for _, entry := range modsets.Modsets {
		log.Printf("resolving modset %s...", entry.Name)
		mods, err := resolver.Resolve(ctx, entry.Mods)
		if err != nil {
			failures = append(failures, fmt.Errorf("modset %s: %w", entry.Name, err))
			continue
		}
		plans = append(plans, newPlan(modtable.Modset{Name: entry.Name, Title: entry.Title}, mods, ""))
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return plans, nil
}

// newPlan splits mods into the dump's mods and its parent: the built-in mods
// it needs, minus own (a built-in modset's own mod).
func newPlan(entry modtable.Modset, mods []modset.Mod, own string) plan {
	result := plan{entry: entry, mods: mods}
	for _, mod := range mods {
		entry.Mods = append(entry.Mods, modtable.Mod{Name: mod.Name, Version: mod.Version})
		if mod.Builtin && mod.Name != own {
			result.parent = append(result.parent, mod)
		}
	}
	result.entry = entry
	return result
}

// distill dumps a modset and its parent and keeps the names it adds.
func distill(ctx context.Context, runner *dump.Runner, plan plan) (modtable.Modset, error) {
	names, err := dumpNames(ctx, runner, plan.mods)
	if err != nil {
		return modtable.Modset{}, err
	}
	parent := prototypes.Names{}
	if len(plan.parent) > 0 {
		if parent, err = dumpNames(ctx, runner, plan.parent); err != nil {
			return modtable.Modset{}, err
		}
	}
	entry := plan.entry
	entry.Entities, entry.Recipes = names.Added(parent)
	return entry, nil
}

func download(ctx context.Context, client *portal.Client, plans []plan, files map[string]string) error {
	for _, plan := range plans {
		for _, mod := range plan.mods {
			if mod.Builtin || files[dump.FileKey(mod)] != "" {
				continue
			}
			path, err := client.Download(ctx, mod.Name, mod.Release)
			if err != nil {
				return err
			}
			files[dump.FileKey(mod)] = path
		}
	}
	return nil
}

func dumpNames(ctx context.Context, runner *dump.Runner, mods []modset.Mod) (prototypes.Names, error) {
	path, err := runner.Dump(ctx, mods)
	if err != nil {
		return prototypes.Names{}, err
	}
	raw, err := prototypes.ReadDump(path)
	if err != nil {
		return prototypes.Names{}, err
	}
	return prototypes.ExtractNames(raw)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
