package blueprint

import (
	"encoding/json"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/icontoken"
	"sort"
	"strings"
)

// Kind distinguishes the node types the catalog stores: book rows,
// renderable leaf blueprint rows, and planner rows (listed, never rendered).
// Planner kinds are the exchange-string envelope keys.
type Kind string

const (
	KindBook                  Kind = "book"
	KindBlueprint             Kind = "blueprint"
	KindUpgradePlanner        Kind = "upgrade_planner"
	KindDeconstructionPlanner Kind = "deconstruction_planner"
)

// IsPlanner reports whether k is an upgrade or deconstruction planner.
func (k Kind) IsPlanner() bool {
	return k == KindUpgradePlanner || k == KindDeconstructionPlanner
}

// Envelope keys used by Factorio. A decoded payload is an object with exactly
// one of these keys at its root, and the same holds for every child of a book.
const (
	keyBook      = "blueprint_book"
	keyBlueprint = "blueprint"
)

// ErrLimitExceeded marks a submission that breaches a traversal bound.
var ErrLimitExceeded = fault.New(fault.Unprocessable, "limit_exceeded", "This blueprint is too large.")

// Node is one hydrated node of a decoded submission. Exactly one of Book,
// Blueprint or Planner is set, matching Kind.
type Node struct {
	Kind      Kind
	Book      *Book
	Blueprint *Blueprint
	Planner   *Planner
}

// Book is a (possibly nested) blueprint book node. It never carries an
// exchange string; sub-book strings are regenerated on demand at export time.
type Book struct {
	Label       string
	Description string
	Icons       string
	ActiveIndex *int32
	Children    []Node
	// Version is the game version that exported this node, as provenance.
	Version string
}

// Blueprint is a renderable leaf. ExchangeString is self-contained so the
// preview runner can render it without the surrounding book.
type Blueprint struct {
	Label          string
	Description    string
	Icons          string
	ExchangeString string
	EntityCount    int32
	// Entities is the aggregated entity prototype name/count breakdown,
	// computed structurally from the decoded blueprint (no live game
	// required).
	Entities []EntityStat
	// Recipes counts the entities set to each recipe, per recipe and recipe
	// quality, sorted.
	Recipes []RecipeStat
	// SnapToGrid is the blueprint's grid snapping; nil when it has none.
	SnapToGrid *SnapToGrid
	// Version is the game version that exported this node, as provenance.
	Version string
	// Compat21 is the leaf's Factorio 2.1 compatibility (compat.go); empty when
	// it was made in 2.1 or carries no version.
	Compat21 string
}

// SnapToGrid is a blueprint's snap-to-grid setting: the grid cell in tiles
// and, for absolute snapping, the blueprint's offset within the world grid.
type SnapToGrid struct {
	Width    int  `json:"width" doc:"The grid's width in tiles."`
	Height   int  `json:"height" doc:"The grid's height in tiles."`
	Absolute bool `json:"absolute" doc:"Whether the grid is absolute."`
	// OffsetX and OffsetY are position-relative-to-grid; only meaningful
	// with absolute snapping.
	OffsetX int `json:"offset_x" doc:"The horizontal offset of a position-relative grid; only meaningful when not absolute."`
	OffsetY int `json:"offset_y" doc:"The vertical offset of a position-relative grid; only meaningful when not absolute."`
}

// EntityStat is one aggregated entity prototype count within a blueprint,
// using Factorio internal entity prototype names.
type EntityStat struct {
	Name    string `json:"name" doc:"The entity's name."`
	Quality string `json:"quality" doc:"The entity's quality."`
	Count   int32  `json:"count" doc:"How many of the entity the blueprint has."`
}

// RecipeStat is one recipe and quality pair and how many entities in the
// blueprint are set to it.
type RecipeStat struct {
	Name    string `json:"name" doc:"The recipe's name."`
	Quality string `json:"quality" doc:"The recipe's quality."`
	Count   int32  `json:"count" doc:"How many entities are set to the recipe."`
}

// Limits bounds traversal of untrusted submissions.
type Limits struct {
	MaxDecodedBytes int64
	MaxDepth        int
	MaxNodes        int
	MaxLeaves       int
}

// DefaultLimits returns the bounds applied to public submissions.
func DefaultLimits() Limits {
	return Limits{
		MaxDecodedBytes: 8 << 20,
		MaxDepth:        16,
		MaxNodes:        2000,
		MaxLeaves:       100,
	}
}

// walkState carries the running counters that bound a single traversal.
type walkState struct {
	limits Limits
	nodes  int
	leaves int
}

// Parse decodes an exchange string and walks the whole recursive structure,
// producing the node tree the API persists. Leaf exchange strings are rebuilt
// individually so every blueprint stays independently renderable, and the
// traversal is bounded by size, depth, node count, and leaf count.
func Parse(exchange string, limits Limits) (Node, error) {
	payload, err := Decode(exchange, limits.MaxDecodedBytes)
	if err != nil {
		return Node{}, err
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Node{}, ErrInvalidExchangeString
	}

	state := &walkState{limits: limits}
	return state.walk(envelope, 1)
}

// walk converts one decoded envelope into a Node, recursing into book
// children. depth is 1-based for the root envelope.
func (s *walkState) walk(envelope map[string]json.RawMessage, depth int) (Node, error) {
	if depth > s.limits.MaxDepth {
		return Node{}, ErrLimitExceeded.Withf("This blueprint book is nested too deeply (at most %d levels).", s.limits.MaxDepth)
	}
	s.nodes++
	if s.nodes > s.limits.MaxNodes {
		return Node{}, ErrLimitExceeded.Withf("This blueprint book has too many items (at most %d).", s.limits.MaxNodes)
	}

	switch {
	case envelope[keyBook] != nil:
		book, err := s.walkBook(envelope[keyBook], depth)
		if err != nil {
			return Node{}, err
		}
		return Node{Kind: KindBook, Book: book}, nil
	case envelope[keyBlueprint] != nil:
		leaf, err := s.walkBlueprint(envelope[keyBlueprint])
		if err != nil {
			return Node{}, err
		}
		return Node{Kind: KindBlueprint, Blueprint: leaf}, nil
	case envelope[string(KindUpgradePlanner)] != nil:
		planner, err := s.walkPlanner(KindUpgradePlanner, envelope[string(KindUpgradePlanner)])
		if err != nil {
			return Node{}, err
		}
		return Node{Kind: KindUpgradePlanner, Planner: planner}, nil
	case envelope[string(KindDeconstructionPlanner)] != nil:
		planner, err := s.walkPlanner(KindDeconstructionPlanner, envelope[string(KindDeconstructionPlanner)])
		if err != nil {
			return Node{}, err
		}
		return Node{Kind: KindDeconstructionPlanner, Planner: planner}, nil
	default:
		return Node{}, ErrInvalidExchangeString
	}
}

// bookPayload is the subset of a decoded book object the catalog persists.
// Children keep their raw JSON so nested envelopes can be walked recursively.
type bookPayload struct {
	Label       string                       `json:"label"`
	Description string                       `json:"description"`
	Icons       json.RawMessage              `json:"icons"`
	ActiveIndex *int32                       `json:"active_index"`
	Version     uint64                       `json:"version"`
	Blueprints  []map[string]json.RawMessage `json:"blueprints"`
}

// walkBook hydrates a book node and its children in source order. Empty books
// are preserved rather than pruned, because an empty book is a meaningful part
// of the submitted structure.
func (s *walkState) walkBook(raw json.RawMessage, depth int) (*Book, error) {
	var payload bookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, ErrInvalidExchangeString
	}

	icons, err := iconsToRichText(payload.Icons)
	if err != nil {
		return nil, err
	}
	book := &Book{
		Label:       payload.Label,
		Description: payload.Description,
		Icons:       icons,
		ActiveIndex: payload.ActiveIndex,
		Version:     formatVersion(payload.Version),
		Children:    make([]Node, 0, len(payload.Blueprints)),
	}
	for _, child := range payload.Blueprints {
		if len(child) == 0 {
			return nil, ErrInvalidExchangeString
		}
		node, err := s.walk(child, depth+1)
		if err != nil {
			return nil, err
		}
		book.Children = append(book.Children, node)
	}
	return book, nil
}

// blueprintPayload is the subset of a decoded blueprint the catalog persists
// directly; the untouched raw object is what gets re-encoded for the runner.
type blueprintPayload struct {
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Icons       json.RawMessage   `json:"icons"`
	Version     uint64            `json:"version"`
	Entities    []json.RawMessage `json:"entities"`
	SnapToGrid  *gridPosition     `json:"snap-to-grid"`
	Absolute    bool              `json:"absolute-snapping"`
	GridOffset  *gridPosition     `json:"position-relative-to-grid"`
}

type gridPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// snapToGrid reads a blueprint's grid snapping; nil when it has none.
func (p blueprintPayload) snapToGrid() *SnapToGrid {
	if p.SnapToGrid == nil || p.SnapToGrid.X < 1 || p.SnapToGrid.Y < 1 {
		return nil
	}
	snap := &SnapToGrid{
		Width: int(p.SnapToGrid.X), Height: int(p.SnapToGrid.Y), Absolute: p.Absolute,
	}
	if p.Absolute && p.GridOffset != nil {
		snap.OffsetX, snap.OffsetY = int(p.GridOffset.X), int(p.GridOffset.Y)
	}
	return snap
}

// blueprintEntity is the subset of a decoded entity used to compute
// structural facts: the aggregated entity/recipe breakdown. Malformed or
// unrecognized entities simply contribute no name/recipe rather than failing
// the whole submission, since this is best-effort structural bookkeeping, not
// validation.
type blueprintEntity struct {
	Name          string `json:"name"`
	Quality       string `json:"quality"`
	Recipe        string `json:"recipe"`
	RecipeQuality string `json:"recipe_quality"`
}

// summarizeEntities counts entities and the entities set to each recipe, both
// per quality, sorting both for deterministic storage.
func summarizeEntities(raw []json.RawMessage) ([]EntityStat, []RecipeStat) {
	counts := make(map[[2]string]int32, len(raw))
	recipeCounts := make(map[[2]string]int32)
	for _, entityRaw := range raw {
		var entity blueprintEntity
		if err := json.Unmarshal(entityRaw, &entity); err != nil || entity.Name == "" {
			continue
		}
		quality := entity.Quality
		if quality == "" {
			quality = "normal"
		}
		counts[[2]string{entity.Name, quality}]++
		if entity.Recipe != "" {
			recipeQuality := entity.RecipeQuality
			if recipeQuality == "" {
				recipeQuality = "normal"
			}
			recipeCounts[[2]string{entity.Recipe, recipeQuality}]++
		}
	}

	entities := make([]EntityStat, 0, len(counts))
	for key, count := range counts {
		entities = append(entities, EntityStat{Name: key[0], Quality: key[1], Count: count})
	}
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].Name != entities[j].Name {
			return entities[i].Name < entities[j].Name
		}
		return entities[i].Quality < entities[j].Quality
	})

	recipes := make([]RecipeStat, 0, len(recipeCounts))
	for key, count := range recipeCounts {
		recipes = append(recipes, RecipeStat{Name: key[0], Quality: key[1], Count: count})
	}
	sort.Slice(recipes, func(i, j int) bool {
		if recipes[i].Name != recipes[j].Name {
			return recipes[i].Name < recipes[j].Name
		}
		return recipes[i].Quality < recipes[j].Quality
	})

	return entities, recipes
}

// walkBlueprint hydrates a leaf and rebuilds its standalone exchange string by
// re-wrapping the original blueprint object in a fresh single-blueprint
// envelope, so book-specific fields such as the child index are dropped.
func (s *walkState) walkBlueprint(raw json.RawMessage) (*Blueprint, error) {
	s.leaves++
	if s.leaves > s.limits.MaxLeaves {
		return nil, ErrLimitExceeded.Withf("This blueprint book has too many blueprints (at most %d).", s.limits.MaxLeaves)
	}

	var payload blueprintPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, ErrInvalidExchangeString
	}

	exchange, err := Encode(map[string]json.RawMessage{keyBlueprint: raw})
	if err != nil {
		return nil, err
	}

	icons, err := iconsToRichText(payload.Icons)
	if err != nil {
		return nil, err
	}
	entities, recipes := summarizeEntities(payload.Entities)
	version := formatVersion(payload.Version)
	return &Blueprint{
		Label:          payload.Label,
		Description:    payload.Description,
		Icons:          icons,
		ExchangeString: exchange,
		EntityCount:    int32(len(payload.Entities)),
		Entities:       entities,
		Recipes:        recipes,
		SnapToGrid:     payload.snapToGrid(),
		Version:        version,
		Compat21:       leafCompat21(version, payload.Entities),
	}, nil
}

type icon struct {
	Signal struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Quality string `json:"quality"`
	} `json:"signal"`
	Index int `json:"index"`
}

// iconsToRichText converts Factorio's structured blueprint icons into the same
// compact tags the game emits when icons are inserted into rich text.
func iconsToRichText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var icons []icon
	if err := json.Unmarshal(raw, &icons); err != nil {
		return "", ErrInvalidExchangeString
	}
	sort.SliceStable(icons, func(i, j int) bool {
		return icons[i].Index < icons[j].Index
	})

	var richText strings.Builder
	for _, icon := range icons {
		tag := icon.Signal.Type
		if tag == "" {
			tag = "item"
		}
		if tag == "virtual" {
			tag = "virtual-signal"
		}
		if icon.Signal.Name == "" || strings.ContainsAny(icon.Signal.Name, "[]=,") ||
			strings.ContainsAny(icon.Signal.Quality, "[]=,") {
			return "", ErrInvalidExchangeString
		}
		richText.WriteString(icontoken.Format(icontoken.Token{
			Type: tag, Name: icon.Signal.Name, Quality: icon.Signal.Quality,
		}, false))
	}
	return richText.String(), nil
}
