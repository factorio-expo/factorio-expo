package search

// schemaBlueprints is the Typesense collection schema described in
// docs/search.md. /api/v1/search queries it: one document per result (each
// blueprint, each planner, and each top-level book with its blueprints' facts
// together), with its entry's filter and sort fields. It declares a
// "rate_out_.*"/"rate_in_.*" wildcard field pair so a query like
// "outputs iron-gear-wheel > 5/s" can filter on rate_out_iron_gear_wheel
// without the schema needing to know every Factorio item name in advance.
var schemaBlueprints = collectionSchema{
	Name:        "blueprints",
	SynonymSets: []string{communityNamesSet},
	Fields: []schemaField{
		{Name: "entry_id", Type: "string", Facet: true},
		// kind is the result's: blueprint, book or a planner kind.
		{Name: "kind", Type: "string", Facet: true},
		// node_id is the blueprint's or planner's id; empty on a book.
		{Name: "node_id", Type: "string", Index: notIndexed()},
		// The result card's text: stored, never matched.
		{Name: "title", Type: "string", Index: notIndexed()},
		{Name: "description", Type: "string", Index: notIndexed()},
		{Name: "book_title", Type: "string", Index: notIndexed()},
		{Name: "icon_string", Type: "string", Index: notIndexed()},
		{Name: "search_text", Type: "string"},
		{Name: "icons", Type: "string[]", Facet: true},
		{Name: "tags", Type: "string[]", Facet: true},
		{Name: "mods", Type: "string[]", Facet: true},
		{Name: "mod_badges", Type: "string[]", Index: notIndexed()},
		{Name: "game_version", Type: "string", Facet: true},
		// version_groups are the game version's release families (versions.go).
		{Name: "version_groups", Type: "string[]", Facet: true},
		// top_product is a leaf's highest-rate output: what similar blueprints share.
		{Name: "top_product", Type: "string", Facet: true, Optional: true},
		{Name: "entity_names", Type: "string[]", Facet: true},
		{Name: "recipes", Type: "string[]", Facet: true},
		{Name: "entity_facts", Type: "string[]", Facet: true},
		{Name: "recipe_facts", Type: "string[]", Facet: true},
		{Name: "qualities", Type: "string[]", Facet: true},
		{Name: "product_items", Type: "string[]", Facet: true},
		{Name: "ingredient_items", Type: "string[]", Facet: true},
		{Name: "electricity_net_watts", Type: "float"},
		{Name: "pollution_net_per_second", Type: "float"},
		{Name: "spoiler", Type: "bool"},
		{Name: "entity_count", Type: "int64"},
		{Name: "leaf_count", Type: "int64"},
		{Name: "position", Type: "int64"},
		{Name: "footprint_short", Type: "int64", Optional: true},
		{Name: "footprint_long", Type: "int64", Optional: true},
		{Name: "view_count", Type: "int64"},
		{Name: "has_preview", Type: "bool"},
		{Name: "has_platform", Type: "bool", Facet: true},
		{Name: "weight_tons", Type: "float", Optional: true},
		{Name: "thrust_mn", Type: "float", Optional: true},
		{Name: "max_speed_km_s", Type: "float", Optional: true},
		{Name: "created_at", Type: "int64"},
		{Name: "updated_at", Type: "int64"},
		{Name: "rate_out_.*", Type: "float", Optional: true},
		{Name: "rate_in_.*", Type: "float", Optional: true},
		// A blueprint's own output rates per tile and per MW, and its build
		// cost per raw resource; a book or planner has none.
		{Name: "rate_tile_out_.*", Type: "float", Optional: true},
		{Name: "rate_mw_out_.*", Type: "float", Optional: true},
		{Name: "cost_raw_.*", Type: "float", Optional: true},
	},
}

// collectionSchema and schemaField are the minimal subset of Typesense's
// collection-creation request body this package produces; there's no need
// for a full SDK's worth of options here.
type collectionSchema struct {
	Name   string        `json:"name"`
	Fields []schemaField `json:"fields"`
	// SynonymSets name the synonym sets the collection's searches expand.
	SynonymSets []string `json:"synonym_sets,omitempty"`
}

type schemaField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Facet    bool   `json:"facet,omitempty"`
	Optional bool   `json:"optional,omitempty"`
	// Index, when explicitly false, tells Typesense not to index this field
	// for filtering/query_by/faceting at all - it is stored and returned in
	// results but otherwise inert. nil (the Go zero value for *bool) omits
	// the key entirely, leaving Typesense's own default (indexed) in place.
	Index *bool `json:"index,omitempty"`
}

// notIndexed marks a schemaField as display-only: stored and returned, but
// never matched, filtered, or faceted on.
func notIndexed() *bool {
	no := false
	return &no
}
