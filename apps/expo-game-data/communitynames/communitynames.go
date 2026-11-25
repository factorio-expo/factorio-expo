// Package communitynames is the hand-written list of names players use for
// game items ("red science" for automation-science-pack). The game files hold
// none of them, so it is edited by hand (names.json), never generated. expo-api
// embeds it through Names and loads it into Typesense as synonyms
// (docs/search.md#community-names); it lives beside the generated game data
// because it is game knowledge, not search code.
package communitynames

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed names.json
var embedded []byte

// Entry gives one item (an internal prototype name) the names players call it.
type Entry struct {
	// Item is the internal name, such as "automation-science-pack".
	Item string `json:"item"`
	// Names are the community names, lowercase words without hyphens.
	Names []string `json:"names"`
}

// Synonym is one group of phrases that find the same results: the community
// names, the item's words and its internal name.
func (e Entry) Synonym() []string {
	words := strings.ReplaceAll(e.Item, "-", " ")
	return append(append([]string{}, e.Names...), words, e.Item)
}

// Names is the list embedded in this binary. A broken file panics: it is
// checked in, and TestNamesFileIsValid catches it first.
var Names = sync.OnceValue(func() []Entry {
	entries, err := Parse(embedded)
	if err != nil {
		panic(fmt.Sprintf("embedded community names: %v", err))
	}
	return entries
})

// Parse decodes and checks a list: every item and name appears once, names
// are lowercase single-spaced phrases, and no name is the item's own words.
func Parse(data []byte) ([]Entry, error) {
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode community names: %w", err)
	}
	seen := map[string]string{}
	items := map[string]bool{}
	for _, entry := range entries {
		if entry.Item == "" || len(entry.Names) == 0 {
			return nil, fmt.Errorf("community names: entry %q needs an item and names", entry.Item)
		}
		if items[entry.Item] {
			return nil, fmt.Errorf("community names: duplicate item %q", entry.Item)
		}
		items[entry.Item] = true
		for _, name := range entry.Names {
			if name != strings.Join(strings.Fields(strings.ToLower(name)), " ") || strings.Contains(name, "-") {
				return nil, fmt.Errorf("community names: %q must be lowercase words without hyphens", name)
			}
			if name == strings.ReplaceAll(entry.Item, "-", " ") {
				return nil, fmt.Errorf("community names: %q is already the item's name", name)
			}
			if owner, ok := seen[name]; ok {
				return nil, fmt.Errorf("community names: %q names both %s and %s", name, owner, entry.Item)
			}
			seen[name] = entry.Item
		}
	}
	return entries, nil
}
