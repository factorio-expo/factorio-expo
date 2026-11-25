package search

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/internal/analysisresult"
)

// projection is everything buildProjection needs to hand back to a Client:
// the entry-level document plus its blueprint documents. Indexable
// is false when the entry should be removed from (or kept out of) the index
// entirely, because it is not listed (entry_listed), so callers can treat
// "index" and "remove" uniformly.
type projection struct {
	Indexable  bool
	Entry      entryFacts
	Blueprints []blueprintDocument
}

// entrySource is the catalog read model the projection is built from:
// *catalog.Service. found is false when the entry is not listed.
type entrySource interface {
	ListedEntry(ctx context.Context, entryID string) (entry catalog.Entry, found bool, err error)
}

// entryRows is everything projectEntry needs: a listed entry as the catalog
// presents it (tree, leaves and their preview states, tags, views) and the
// stored stats of its leaves. It carries no behavior, so projectEntry is a
// pure function of it and needs no database.
type entryRows struct {
	Entry catalog.Entry
	Stats []db.BlueprintStat
}

// rowQueries is the slice of the generated queries the projection reads
// beside the catalog read model, so a Syncer can run against a fake in tests.
type rowQueries interface {
	ListBlueprintStatsByRevision(ctx context.Context, revisionID string) ([]db.BlueprintStat, error)
}

// loadEntryRows reads the rows behind one entry's documents. found is false
// when the entry is not listed: the caller then removes its documents.
func loadEntryRows(
	ctx context.Context, source entrySource, queries rowQueries, entryID string,
) (rows entryRows, found bool, err error) {
	entry, found, err := source.ListedEntry(ctx, entryID)
	if err != nil || !found {
		return entryRows{}, false, err
	}
	if entry.Revision == nil || entry.Revision.Root == nil {
		return entryRows{}, false, fmt.Errorf("listed entry %s has no revision tree", entryID)
	}
	stats, err := queries.ListBlueprintStatsByRevision(ctx, entry.Revision.ID)
	if err != nil {
		return entryRows{}, false, fmt.Errorf("list blueprint stats: %w", err)
	}
	return entryRows{Entry: entry, Stats: stats}, true, nil
}

// buildProjection loads and projects one entry; see loadEntryRows and
// projectEntry.
func buildProjection(ctx context.Context, source entrySource, queries rowQueries, entryID string) (projection, error) {
	rows, found, err := loadEntryRows(ctx, source, queries, entryID)
	if err != nil || !found {
		return projection{}, err
	}
	return projectEntry(rows)
}

// productionFacts are a leaf's stored production facts and whether its rates
// are current. A snapshot that no longer decodes is left out (the leaf
// detail presents it as failed) rather than keeping the entry out of search.
func productionFacts(stat db.BlueprintStat) (facts analysisresult.Facts, current bool) {
	if !stat.ProductionSchemaVersion.Valid || !stat.ProductionState.Valid {
		return analysisresult.Facts{}, false
	}
	snapshot, err := analysisresult.ParseSnapshot(stat.Production, stat.ProductionSchemaVersion.Int32)
	if err != nil {
		slog.Error("decode stored production snapshot", "blueprint_id", stat.BlueprintID, "error", err)
		return analysisresult.Facts{}, false
	}
	return snapshot.Facts(), !stat.ProductionStale
}

// projectEntry reduces one listed entry to the minimal searchable documents
// described in docs/search.md. It is pure: no I/O, no clock.
func projectEntry(rows entryRows) (projection, error) {
	entry, revision := rows.Entry, rows.Entry.Revision
	entryID := entry.ID
	root := revision.Root
	leaves := root.Leaves()
	statsByBlueprint := make(map[string]db.BlueprintStat, len(rows.Stats))
	for _, stat := range rows.Stats {
		statsByBlueprint[stat.BlueprintID] = stat
	}

	entryOutRates := make(map[string]float64)
	entryInRates := make(map[string]float64)
	entryQualityOutRates := make(map[string]float64)
	entryQualityInRates := make(map[string]float64)
	entityNames := newStringSet()
	recipes := newStringSet()
	entityFacts := newStringSet()
	recipeFacts := newStringSet()
	qualities := newStringSet()
	modValues := newStringSet()
	modBadges := newStringSet()
	// Vanilla only while every leaf has a detection that needs nothing.
	vanilla := len(leaves) > 0
	var electricityNet, pollutionNet float64
	var entryHasPreview, entryHasPlatform bool
	previewCount := int64(0)
	var entityCount int64
	footprints := footprintSides{measured: len(leaves) > 0}

	nodes := searchNodes(root)
	docs := make([]blueprintDocument, 0, len(nodes)+1)
	for position, node := range nodes {
		if node.Kind != blueprint.KindBlueprint {
			doc := projectPlanner(node)
			doc.Position = int64(position)
			docs = append(docs, doc)
			continue
		}
		doc, facts, err := projectLeaf(node, statsByBlueprint)
		if err != nil {
			return projection{}, err
		}
		doc.Position = int64(position)
		entityCount += doc.EntityCount
		if !facts.vanilla {
			vanilla = false
		}
		modValues.addAll(facts.modValues)
		modBadges.addAll(doc.ModBadges)
		footprints.add(facts.footprint, facts.current)
		if doc.HasPreview {
			previewCount++
			entryHasPreview = true
		}
		entryHasPlatform = entryHasPlatform || doc.HasPlatform

		entityNames.addAll(doc.EntityNames)
		recipes.addAll(doc.Recipes)
		entityFacts.addAll(doc.EntityFacts)
		recipeFacts.addAll(doc.RecipeFacts)
		qualities.addAll(doc.Qualities)
		mergeRates(entryOutRates, facts.outRates)
		mergeRates(entryInRates, facts.inRates)
		mergeRates(entryQualityOutRates, facts.qualityOutRates)
		mergeRates(entryQualityInRates, facts.qualityInRates)
		electricityNet += doc.ElectricityNetWatts
		pollutionNet += doc.PollutionNetPerSecond
		docs = append(docs, doc)
	}

	// The root node (a book, or the entry's one blueprint) names the entry
	// beside its own title and description.
	rootTitleText, rootTitleIcons := stripEmbeddedIcons(root.Label)
	rootDescriptionText, rootDescriptionIcons := stripEmbeddedIcons(root.Description)
	rootIcons := mergeIconLists(splitIconTokens(root.Icons), rootTitleIcons, rootDescriptionIcons)
	titleText, titleIcons := stripEmbeddedIcons(entry.Title)
	descriptionText, descriptionIcons := stripEmbeddedIcons(entry.Description)
	entrySearchText := buildSearchText(titleText, descriptionText, buildSearchText(rootTitleText, rootDescriptionText))
	entryIcons := mergeIconLists(rootIcons, titleIcons, descriptionIcons)

	entryDoc := entryFacts{
		ID:                    entryID,
		Title:                 entry.Title,
		Description:           entry.Description,
		SearchText:            entrySearchText,
		Icons:                 entryIcons,
		Tags:                  entry.Tags,
		Mods:                  entryMods(vanilla, modValues.values()),
		ModBadges:             nonNil(modBadges.values()),
		RootKind:              string(root.Kind),
		GameVersion:           revision.GameVersion,
		VersionGroups:         versionGroups(revision.GameVersion, revision.Compat21),
		EntityNames:           entityNames.values(),
		Recipes:               recipes.values(),
		EntityFacts:           sortedSetValues(entityFacts),
		RecipeFacts:           sortedSetValues(recipeFacts),
		Qualities:             sortedSetValues(qualities),
		ProductItems:          mapKeys(entryOutRates),
		IngredientItems:       mapKeys(entryInRates),
		ElectricityNetWatts:   electricityNet,
		PollutionNetPerSecond: pollutionNet,
		Spoiler:               entry.Spoiler,
		EntityCount:           entityCount,
		LeafCount:             int64(len(leaves)),
		FootprintShort:        footprints.short(),
		FootprintLong:         footprints.long(),
		PreviewCount:          previewCount,
		ViewCount:             entry.ViewCount,
		HasPreview:            entryHasPreview,
		HasPlatform:           entryHasPlatform,
		CreatedAt:             entry.CreatedAt.Unix(),
		UpdatedAt:             entry.UpdatedAt.Unix(),
		rates: buildRateFields(
			entryOutRates, entryInRates, entryQualityOutRates, entryQualityInRates, 0, 0, nil,
		),
	}

	entryTitle := cmp.Or(entry.Title, root.Label)
	if root.Kind == blueprint.KindBook {
		for i := range docs {
			docs[i].BookTitle = entryTitle
		}
		docs = append([]blueprintDocument{bookDocument(entryDoc, root, entryTitle)}, docs...)
	} else if len(docs) == 1 {
		// The entry is its one blueprint or planner: the entry's text is the
		// node's own.
		doc := &docs[0]
		doc.Title, doc.Description = entryTitle, entry.Description
		doc.SearchText = buildSearchText(titleText, descriptionText, doc.SearchText)
		doc.Icons = mergeIconLists(doc.Icons, titleIcons, descriptionIcons)
	}
	for i := range docs {
		doc := &docs[i]
		doc.EntryID = entryID
		doc.Tags = entry.Tags
		doc.GameVersion = revision.GameVersion
		doc.VersionGroups = versionGroups(revision.GameVersion, revision.Compat21)
		doc.LeafCount = int64(len(leaves))
		doc.Spoiler = entry.Spoiler
		doc.ViewCount = entry.ViewCount
		doc.CreatedAt = entry.CreatedAt.Unix()
		doc.UpdatedAt = entry.UpdatedAt.Unix()
	}

	return projection{Indexable: true, Entry: entryDoc, Blueprints: docs}, nil
}

// searchNodes returns the blueprints and planners at or below root, in tree
// order: each is a search result of its own.
func searchNodes(root *catalog.Node) []catalog.Node {
	var nodes []catalog.Node
	var walk func(node catalog.Node)
	walk = func(node catalog.Node) {
		switch {
		case node.Kind == blueprint.KindBlueprint, blueprint.Kind(node.Kind).IsPlanner():
			nodes = append(nodes, node)
		default:
			for _, child := range node.Children {
				walk(child)
			}
		}
	}
	walk(*root)
	return nodes
}

// bookDocument is a top-level book's result: the entry's aggregated text and
// its blueprints' facts together.
func bookDocument(entryDoc entryFacts, root *catalog.Node, title string) blueprintDocument {
	return blueprintDocument{
		ID:                    bookDocumentID(entryDoc.ID),
		Kind:                  string(root.Kind),
		Title:                 title,
		Description:           entryDoc.Description,
		IconString:            root.Icons,
		SearchText:            entryDoc.SearchText,
		Icons:                 entryDoc.Icons,
		Mods:                  entryDoc.Mods,
		ModBadges:             entryDoc.ModBadges,
		EntityNames:           entryDoc.EntityNames,
		Recipes:               entryDoc.Recipes,
		EntityFacts:           entryDoc.EntityFacts,
		RecipeFacts:           entryDoc.RecipeFacts,
		Qualities:             entryDoc.Qualities,
		ProductItems:          entryDoc.ProductItems,
		IngredientItems:       entryDoc.IngredientItems,
		ElectricityNetWatts:   entryDoc.ElectricityNetWatts,
		PollutionNetPerSecond: entryDoc.PollutionNetPerSecond,
		EntityCount:           entryDoc.EntityCount,
		Position:              -1,
		FootprintShort:        entryDoc.FootprintShort,
		FootprintLong:         entryDoc.FootprintLong,
		HasPreview:            entryDoc.HasPreview,
		HasPlatform:           entryDoc.HasPlatform,
		rates:                 entryDoc.rates,
	}
}

// projectPlanner builds one planner's document: its text only, since
// planners are never analysed.
func projectPlanner(node catalog.Node) blueprintDocument {
	titleText, titleIcons := stripEmbeddedIcons(node.Label)
	descriptionText, descriptionIcons := stripEmbeddedIcons(node.Description)
	return blueprintDocument{
		ID:          node.ID,
		Kind:        string(node.Kind),
		NodeID:      node.ID,
		Title:       node.Label,
		Description: node.Description,
		IconString:  node.Icons,
		SearchText:  buildSearchText(titleText, descriptionText),
		Icons:       mergeIconLists(splitIconTokens(node.Icons), titleIcons, descriptionIcons),
		Mods:        []string{},
		ModBadges:   []string{},
		rates:       map[string]float64{},
	}
}

// leafFacts are what the entry's facts aggregate from one leaf beside its
// blueprint document.
type leafFacts struct {
	vanilla   bool
	modValues []string
	footprint *analysisresult.Footprint
	current   bool

	outRates, inRates, qualityOutRates, qualityInRates map[string]float64
}

// projectLeaf builds one leaf's blueprint document from its stored stats;
// projectEntry adds the entry's fields.
func projectLeaf(leaf catalog.Node, statsByBlueprint map[string]db.BlueprintStat) (blueprintDocument, leafFacts, error) {
	var facts leafFacts
	titleText, titleIcons := stripEmbeddedIcons(leaf.Label)
	descriptionText, descriptionIcons := stripEmbeddedIcons(leaf.Description)
	doc := blueprintDocument{
		ID:          leaf.ID,
		Kind:        string(leaf.Kind),
		NodeID:      leaf.ID,
		Title:       leaf.Label,
		Description: leaf.Description,
		IconString:  leaf.Icons,
		SearchText:  buildSearchText(titleText, descriptionText),
		Icons:       mergeIconLists(splitIconTokens(leaf.Icons), titleIcons, descriptionIcons),
		EntityCount: int64(leaf.EntityCount),
		// A stored preview counts whatever the status (a re-render may run).
		HasPreview: leaf.Preview != nil,
		rates:      map[string]float64{},
	}

	stat, hasStat := statsByBlueprint[leaf.ID]
	detection, err := decodeMods(stat, hasStat)
	if err != nil {
		return doc, facts, fmt.Errorf("decode mods for blueprint %s: %w", leaf.ID, err)
	}
	if detection != nil {
		facts.vanilla = detection.IsVanilla()
		facts.modValues = detection.SearchValues()
		doc.ModBadges = detection.Badges()
	}
	if doc.ModBadges == nil {
		doc.ModBadges = []string{}
	}
	doc.Mods = entryMods(facts.vanilla, slices.Clone(facts.modValues))
	if !hasStat {
		return doc, facts, nil
	}

	if doc.EntityNames, err = decodeEntityNames(stat.Entities); err != nil {
		return doc, facts, fmt.Errorf("decode entities for blueprint %s: %w", leaf.ID, err)
	}
	if doc.Recipes, err = decodeRecipes(stat.Recipes); err != nil {
		return doc, facts, fmt.Errorf("decode recipes for blueprint %s: %w", leaf.ID, err)
	}
	if doc.EntityFacts, err = decodeEntityFacts(stat.Entities); err != nil {
		return doc, facts, fmt.Errorf("decode entity quality facts for blueprint %s: %w", leaf.ID, err)
	}
	if doc.RecipeFacts, err = decodeRecipeFacts(stat.Recipes); err != nil {
		return doc, facts, fmt.Errorf("decode recipe quality facts for blueprint %s: %w", leaf.ID, err)
	}
	production, current := productionFacts(stat)
	facts.footprint, facts.current = production.Footprint, current
	if current {
		facts.outRates, facts.inRates = production.OutRates, production.InRates
		facts.qualityOutRates, facts.qualityInRates = production.QualityOutRates, production.QualityInRates
		doc.ElectricityNetWatts, doc.PollutionNetPerSecond = production.ElectricityNetWatts, production.PollutionNetPerSecond
		if footprint := production.Footprint; footprint != nil {
			short := int64(min(footprint.Width, footprint.Height))
			long := int64(max(footprint.Width, footprint.Height))
			doc.FootprintShort, doc.FootprintLong = &short, &long
		}
		if platform := production.Platform; platform != nil {
			doc.HasPlatform = true
			doc.WeightTons, doc.ThrustMN, doc.MaxSpeedKMS = platform.WeightTons, platform.ThrustMN, platform.MaxSpeedKMS
		}
	}
	if doc.Qualities, err = decodeQualities(stat.Entities, stat.Recipes, production.Qualities); err != nil {
		return doc, facts, fmt.Errorf("decode qualities for blueprint %s: %w", leaf.ID, err)
	}
	doc.ProductItems = mapKeys(facts.outRates)
	doc.TopProduct = topProduct(facts.outRates)
	doc.IngredientItems = mapKeys(facts.inRates)
	var tiles, megawatts float64
	var cost map[string]float64
	if current {
		tiles, megawatts, cost = production.FootprintTiles(), production.ConsumptionMW(), production.RawCost
	}
	doc.rates = buildRateFields(
		facts.outRates, facts.inRates, facts.qualityOutRates, facts.qualityInRates, tiles, megawatts, cost,
	)
	return doc, facts, nil
}

// footprintSides collects the leaves' largest short and long footprint sides;
// one unmeasured leaf leaves the entry without them.
type footprintSides struct {
	measured          bool
	shortest, longest int64
}

func (f *footprintSides) add(footprint *analysisresult.Footprint, current bool) {
	if !current || footprint == nil {
		f.measured = false
		return
	}
	short, long := int64(min(footprint.Width, footprint.Height)), int64(max(footprint.Width, footprint.Height))
	f.shortest, f.longest = max(f.shortest, short), max(f.longest, long)
}

func (f footprintSides) short() *int64 {
	if !f.measured {
		return nil
	}
	return &f.shortest
}

func (f footprintSides) long() *int64 {
	if !f.measured {
		return nil
	}
	return &f.longest
}

// decodeMods reads a leaf's stored mod detection; nil when the leaf has
// none yet (stored before detection ran, and not re-detected).
func decodeMods(stat db.BlueprintStat, hasStat bool) (*mods.Detection, error) {
	if !hasStat || stat.ModsVersion == "" {
		return nil, nil
	}
	var detection mods.Detection
	if err := json.Unmarshal(stat.Mods, &detection); err != nil {
		return nil, err
	}
	return &detection, nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// entryMods is the entry's mods field: mods.Vanilla when every leaf needs
// nothing beyond base, else the leaves' filter values together.
func entryMods(vanilla bool, values []string) []string {
	if vanilla {
		return []string{mods.Vanilla}
	}
	if values == nil {
		return []string{}
	}
	return values
}

// buildSearchText joins the icon-stripped title and description into the one
// blob Typesense actually tokenizes and matches free-text queries against.
func buildSearchText(parts ...string) string {
	return strings.TrimSpace(strings.Join(parts, " "))
}

func mergeRates(dst, src map[string]float64) {
	for item, rate := range src {
		dst[item] += rate
	}
}

func mapKeys(m map[string]float64) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

type stringSet struct {
	seen  map[string]struct{}
	order []string
}

func newStringSet() *stringSet {
	return &stringSet{seen: make(map[string]struct{})}
}

func (s *stringSet) add(value string) {
	if value == "" {
		return
	}
	if _, ok := s.seen[value]; ok {
		return
	}
	s.seen[value] = struct{}{}
	s.order = append(s.order, value)
}

func (s *stringSet) addAll(values []string) {
	for _, value := range values {
		s.add(value)
	}
}

func (s *stringSet) values() []string {
	return s.order
}
