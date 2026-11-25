package search

import (
	"encoding/json"
	"strings"
)

// entryFacts are the entry-level values projectEntry aggregates across every
// leaf of the entry's current revision: title/description/icons/tags and the
// entity/recipe/production facts. It is never sent to Typesense itself (there
// is no entry collection): the book result's document and every result's
// shared fields are built from it.
//
// rates holds the per-item "rate_out_<item>" / "rate_in_<item>" values (see
// schema.go's wildcard fields).
type entryFacts struct {
	ID          string
	Title       string
	Description string
	// SearchText is title+description with any embedded rich-text icon
	// tokens stripped out (see icons.go's stripEmbeddedIcons), used only as
	// the query_by target. Factorio blueprint labels often embed icons with
	// no surrounding whitespace (e.g. "[item=x]Gleba science[item=y]+"),
	// which would otherwise glue onto adjacent words as one unsearchable
	// token; Title/Description keep the original text so result cards can
	// still render the author's inline icons.
	SearchText string
	Icons      []string
	Tags       []string
	// Mods are the mod filter values (mods.Vanilla, mods.SpaceAge, modset
	// names) of the leaves' mod detections together.
	Mods []string
	// ModBadges are the mod filter values the result cards badge
	// (mods.Detection.Badges of the leaves together); display only.
	ModBadges   []string
	RootKind    string
	GameVersion string
	// VersionGroups are the release families the game version belongs to
	// (versionGroups): what the game version filter selects and counts.
	VersionGroups []string

	EntityNames     []string
	Recipes         []string
	EntityFacts     []string
	RecipeFacts     []string
	Qualities       []string
	ProductItems    []string
	IngredientItems []string

	ElectricityNetWatts   float64
	PollutionNetPerSecond float64

	// Spoiler is the entry's spoiler/NSFW flag.
	Spoiler bool

	EntityCount int64
	LeafCount   int64
	// FootprintShort and FootprintLong are the largest short and long sides
	// of the leaves' footprints, so "fits in W x H" (either way round) is two
	// comparisons. Omitted unless every leaf has been measured.
	FootprintShort *int64
	FootprintLong  *int64
	PreviewCount   int64
	// ViewCount is the all-time page views; refreshViews keeps it current
	// between reindexes.
	ViewCount  int64
	HasPreview bool
	// HasPlatform is true when any leaf is a space platform.
	HasPlatform bool

	CreatedAt int64
	UpdatedAt int64

	rates map[string]float64
}

// blueprintDocument is the Typesense wire shape of the collection /api/v1/search
// queries: one search result with its facts, plus its entry's filter and sort
// fields (tags, game version, views, timestamps) so a query needs no join.
// Kind says what it is: a blueprint (its own facts), a planner (text only),
// or a top-level book (all its blueprints' facts together, like the entry
// document).
type blueprintDocument struct {
	ID      string `json:"id"`
	EntryID string `json:"entry_id"`
	Kind    string `json:"kind"`
	// NodeID is the blueprint's or planner's id; empty on a book.
	NodeID string `json:"node_id"`
	// Title, Description and IconString are what the result card shows: the
	// node's label, description and rich-text icons; the entry's title and
	// description for a book or an entry's only blueprint or planner.
	// BookTitle is the book entry's title on a node inside a book.
	Title       string `json:"title"`
	Description string `json:"description"`
	BookTitle   string `json:"book_title"`
	IconString  string `json:"icon_string"`
	// SearchText is the node's own label and description; for a book or an
	// entry's only node also the entry title and description.
	SearchText  string   `json:"search_text"`
	Icons       []string `json:"icons"`
	Tags        []string `json:"tags"`
	Mods        []string `json:"mods"`
	ModBadges   []string `json:"mod_badges"`
	GameVersion string   `json:"game_version"`
	// VersionGroups are the release families the revision's game version
	// belongs to (versionGroups).
	VersionGroups []string `json:"version_groups"`
	// TopProduct is the leaf's item or fluid with the highest output rate
	// ("" when it has no current production): what "similar" blueprints share.
	TopProduct string `json:"top_product,omitempty"`

	EntityNames     []string `json:"entity_names"`
	Recipes         []string `json:"recipes"`
	EntityFacts     []string `json:"entity_facts"`
	RecipeFacts     []string `json:"recipe_facts"`
	Qualities       []string `json:"qualities"`
	ProductItems    []string `json:"product_items"`
	IngredientItems []string `json:"ingredient_items"`

	ElectricityNetWatts   float64 `json:"electricity_net_watts"`
	PollutionNetPerSecond float64 `json:"pollution_net_per_second"`

	// Spoiler is the entry's spoiler/NSFW flag.
	Spoiler bool `json:"spoiler"`

	EntityCount int64 `json:"entity_count"`
	// LeafCount is the entry's; Position the node's place in tree order (-1
	// for the book), the last tiebreak so a book lists before its nodes and
	// they list in order.
	LeafCount      int64  `json:"leaf_count"`
	Position       int64  `json:"position"`
	FootprintShort *int64 `json:"footprint_short,omitempty"`
	FootprintLong  *int64 `json:"footprint_long,omitempty"`
	ViewCount      int64  `json:"view_count"`
	HasPreview     bool   `json:"has_preview"`
	// HasPlatform is true for an analysed space platform, which also carries
	// its metrics, so every platform filter matches the same leaf.
	HasPlatform bool     `json:"has_platform"`
	WeightTons  *float64 `json:"weight_tons,omitempty"`
	ThrustMN    *float64 `json:"thrust_mn,omitempty"`
	MaxSpeedKMS *float64 `json:"max_speed_km_s,omitempty"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`

	rates map[string]float64
}

// bookDocumentID is the document id of an entry's book: node and entry ids
// share an alphabet, so the prefix keeps them apart.
func bookDocumentID(entryID string) string {
	return "book:" + entryID
}

// The production filter directions: the rate out or in, and the rate out per
// tile of footprint or per MW of electricity consumed.
const (
	DirectionOut        = "out"
	DirectionIn         = "in"
	DirectionOutPerTile = "out_per_tile"
	DirectionOutPerMW   = "out_per_mw"
)

// rateFieldPrefixes are the dynamic field prefixes of each direction; each has
// its own wildcard in schema.go.
var rateFieldPrefixes = map[string]string{
	DirectionOut:        "rate_out_",
	DirectionIn:         "rate_in_",
	DirectionOutPerTile: "rate_tile_out_",
	DirectionOutPerMW:   "rate_mw_out_",
}

// ValidDirection reports whether direction is a production filter direction.
func ValidDirection(direction string) bool {
	_, ok := rateFieldPrefixes[direction]
	return ok
}

// buildRateFields turns one result's facts into its dynamic fields, using the
// Factorio internal prototype name as-is (see rateFieldName): the per-item
// "rate_out_<item>" / "rate_in_<item>" rates (quality maps hold items only,
// keyed by analysisresult.QualityKey) and, given a leaf's own footprint area
// and consumption (zero leaves them out), its output rates per tile
// ("rate_tile_out_<item>") and per MW ("rate_mw_out_<item>"). rawCost adds
// "cost_raw_<resource>".
func buildRateFields(
	outRates, inRates, qualityOutRates, qualityInRates map[string]float64,
	tiles, megawatts float64, rawCost map[string]float64,
) map[string]float64 {
	fields := make(map[string]float64, 3*len(outRates)+len(inRates)+3*len(qualityOutRates)+len(qualityInRates)+len(rawCost))
	add := func(direction, quality, item string, rate float64) {
		fields[rateFieldName(direction, quality, item)] = rate
		if direction != DirectionOut {
			return
		}
		if tiles > 0 {
			fields[rateFieldName(DirectionOutPerTile, quality, item)] = rate / tiles
		}
		if megawatts > 0 {
			fields[rateFieldName(DirectionOutPerMW, quality, item)] = rate / megawatts
		}
	}
	for item, rate := range outRates {
		add(DirectionOut, "", item, rate)
	}
	for item, rate := range inRates {
		add(DirectionIn, "", item, rate)
	}
	for key, rate := range qualityOutRates {
		quality, item, _ := strings.Cut(key, "\x00")
		add(DirectionOut, quality, item, rate)
	}
	for key, rate := range qualityInRates {
		quality, item, _ := strings.Cut(key, "\x00")
		add(DirectionIn, quality, item, rate)
	}
	for resource, amount := range rawCost {
		fields[costFieldName(resource)] = amount
	}
	return fields
}

// rateFieldName builds the dynamic Typesense field name for one item's
// production rate in one direction. Typesense field names only allow
// letters, digits, and underscores, so hyphens in Factorio's internal names
// (e.g. "iron-gear-wheel") are replaced with underscores; this is a fixed,
// reversible-enough substitution; no parsing of the name itself.
func rateFieldName(direction, quality, item string) string {
	prefix := rateFieldPrefixes[direction]
	if quality != "" {
		return prefix + sanitizeToken(quality) + "_" + sanitizeToken(item)
	}
	return prefix + sanitizeToken(item)
}

// costFieldName is the dynamic field of one raw resource's build cost.
func costFieldName(resource string) string {
	return "cost_raw_" + sanitizeToken(resource)
}

func sanitizeToken(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// toMap flattens the document to a plain JSON object and merges in the
// dynamic rate_*/cost_raw_* fields, producing the exact body Typesense's
// document upsert endpoint expects.
func (d blueprintDocument) toMap() (map[string]any, error) {
	return flattenDocument(d, d.rates)
}

func flattenDocument(doc any, rates map[string]float64) (map[string]any, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any)
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for field, rate := range rates {
		m[field] = rate
	}
	return m, nil
}
