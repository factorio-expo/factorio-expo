// Package dump runs Factorio's --dump-data for one set of enabled mods and
// keeps each result, so a mod list that did not change is never dumped twice.
package dump

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"factorio-expo/apps/expo-game-data/internal/modset"
)

// Timeout bounds one dump; big overhauls take minutes to load.
const Timeout = 20 * time.Minute

// Runner dumps prototype data. WriteDir is the write-data directory of the
// Factorio config (dumps land in its script-output/).
type Runner struct {
	Bin      string
	Config   string
	WriteDir string
	// Builtins are every built-in mod, so the mod list can disable the ones
	// a dump does not need.
	Builtins []modset.Builtin
	// Files maps a portal mod (FileKey) to its downloaded zip.
	Files map[string]string
}

// Dump returns the path of the data-raw dump with exactly these mods enabled.
func (r *Runner) Dump(ctx context.Context, mods []modset.Mod) (string, error) {
	cached := filepath.Join(r.WriteDir, "dumps", Fingerprint(mods)+".json")
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	modDir := filepath.Join(r.WriteDir, "run-mods")
	if err := os.RemoveAll(modDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		return "", err
	}
	if err := r.writeModList(modDir, mods); err != nil {
		return "", err
	}
	log.Printf("dumping data for %s...", describe(mods))
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	command := exec.CommandContext(ctx, r.Bin, "--config", r.Config, "--mod-directory", modDir, "--dump-data")
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("factorio --dump-data with %s failed: %w: %s", describe(mods), err, failure(output))
	}
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(r.WriteDir, "script-output", "data-raw-dump.json"), cached); err != nil {
		return "", fmt.Errorf("keep dump: %w", err)
	}
	return cached, nil
}

type modListEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// writeModList links the portal zips into modDir and writes mod-list.json
// naming every built-in mod (enabled or not) and the portal mods.
func (r *Runner) writeModList(modDir string, mods []modset.Mod) error {
	enabled := map[string]bool{}
	var entries []modListEntry
	for _, mod := range mods {
		enabled[mod.Name] = true
		if mod.Builtin {
			continue
		}
		source, ok := r.Files[FileKey(mod)]
		if !ok {
			return fmt.Errorf("mod %s was not downloaded", mod.Name)
		}
		if err := linkOrCopy(source, filepath.Join(modDir, filepath.Base(source))); err != nil {
			return err
		}
		entries = append(entries, modListEntry{Name: mod.Name, Enabled: true})
	}
	for _, builtin := range r.Builtins {
		entries = append(entries, modListEntry{Name: builtin.Name, Enabled: enabled[builtin.Name]})
	}
	data, err := json.MarshalIndent(map[string]any{"mods": entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(modDir, "mod-list.json"), data, 0o644)
}

// FileKey identifies a portal mod's zip in Runner.Files: two modsets may
// resolve different versions of one mod.
func FileKey(mod modset.Mod) string {
	return mod.Name + "@" + mod.Version
}

// Fingerprint names a dump by its enabled mods and their versions (base's
// version is the game's).
func Fingerprint(mods []modset.Mod) string {
	hash := sha256.New()
	for _, mod := range mods {
		fmt.Fprintf(hash, "%s@%s\n", mod.Name, mod.Version)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func describe(mods []modset.Mod) string {
	names := make([]string, 0, len(mods))
	for _, mod := range mods {
		names = append(names, mod.Name+" "+mod.Version)
	}
	return strings.Join(names, ", ")
}

func linkOrCopy(source, target string) error {
	if err := os.Link(source, target); err == nil {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var logError = regexp.MustCompile(`^\s*\d+\.\d+ Error `)

// failure picks Factorio's error out of its log: a timestamped "Error" log
// line, else the message under an "--- Error ---" banner, else the last 2000
// bytes.
func failure(output []byte) string {
	lines := strings.Split(string(output), "\n")
	for _, line := range slices.Backward(lines) {
		if logError.MatchString(line) {
			return strings.TrimSpace(line)
		}
	}
	for i, line := range lines {
		if strings.Contains(line, "--- Error ---") {
			message := lines[i+1 : min(i+6, len(lines))]
			return strings.TrimSpace(strings.Join(message, " "))
		}
	}
	if len(output) > 2000 {
		output = output[len(output)-2000:]
	}
	return string(output)
}
