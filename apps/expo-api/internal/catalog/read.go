package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"factorio-expo/apps/expo-api/internal/blueprint"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/internal/analysisresult"
)

// GetEntry returns an entry hydrated with its live revision: the shape-only
// tree joined with the book/blueprint rows it points at, plus any preview that
// has already been rendered. viewerUserID is empty for anonymous callers;
// private entries are only visible to their owner. IsFavorited is the viewer's.
func (s *Service) GetEntry(ctx context.Context, entryID, viewerUserID string) (Entry, error) {
	return readEntry(ctx, s.queries, entryID, viewerUserID)
}

// readEntry is GetEntry on any query set, so a write returns the entry as
// its own transaction sees it.
func readEntry(ctx context.Context, queries *db.Queries, entryID, viewerUserID string) (Entry, error) {
	row, err := entryAccess{queries: queries}.Viewable(ctx, entryID, viewerUserID)
	if err != nil {
		return Entry{}, err
	}
	return hydrateEntry(ctx, queries, row, viewerUserID)
}

// ListedEntry is a listed entry (entry_listed) as GetEntry presents it to a
// signed-out viewer: the read model the search projection is built from.
// found is false when the entry is not listed.
func (s *Service) ListedEntry(ctx context.Context, entryID string) (Entry, bool, error) {
	id, err := parseID(entryID)
	if err != nil {
		return Entry{}, false, err
	}
	row, err := s.queries.GetListedEntry(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("read listed entry: %w", err)
	}
	entry, err := hydrateEntry(ctx, s.queries, row, "")
	if err != nil {
		return Entry{}, false, err
	}
	return entry, true, nil
}

// hydrateEntry presents an entry row with its owner's name, tags, views, the
// viewer's favorite and pin, and its live revision.
func hydrateEntry(ctx context.Context, queries *db.Queries, row db.Entry, viewerUserID string) (Entry, error) {
	entry := entryFromRow(row)
	if row.OwnerUserID.Valid {
		owner, err := queries.GetUserByID(ctx, row.OwnerUserID.String)
		if err != nil {
			return Entry{}, fmt.Errorf("read entry owner: %w", err)
		}
		entry.OwnerUsername = owner.Username
	}
	tags, err := queries.ListTagsByEntry(ctx, row.ID)
	if err != nil {
		return Entry{}, fmt.Errorf("list entry tags: %w", err)
	}
	entry.Tags = tagSlugs(tags)
	favorited, err := favoritedEntryIDs(ctx, queries, viewerUserID, []string{row.ID})
	if err != nil {
		return Entry{}, err
	}
	entry.IsFavorited = favorited[entry.ID]
	pinned, err := pinnedEntryIDs(ctx, queries, viewerUserID, []string{row.ID})
	if err != nil {
		return Entry{}, err
	}
	entry.IsPinned = pinned[entry.ID]
	if entry.ViewCount, err = queries.GetEntryViewCount(ctx, row.ID); err != nil {
		return Entry{}, fmt.Errorf("read entry views: %w", err)
	}
	if entry.Challenges, err = entryChallenges(ctx, queries, row.ID); err != nil {
		return Entry{}, err
	}
	if entry.Alias, err = entryAlias(ctx, queries, row.ID); err != nil {
		return Entry{}, err
	}
	records, err := readCurrentRecords(ctx, queries)
	if err != nil {
		return Entry{}, err
	}
	entry.Records = records.ofEntry(row.ID)
	if !row.CurrentRevisionID.Valid {
		return entry, nil
	}
	revision, err := hydrateRevision(ctx, queries, row.CurrentRevisionID.String)
	if err != nil {
		return Entry{}, err
	}
	entry.Revision = revision
	return entry, nil
}

// ListRevisions returns an entry's revision history, newest first, without
// hydrating each revision's node tree.
func (s *Service) ListRevisions(ctx context.Context, entryID, viewerUserID string) ([]Revision, error) {
	row, err := s.entries().Viewable(ctx, entryID, viewerUserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListRevisionsByEntry(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	revisions := make([]Revision, 0, len(rows))
	for _, revision := range rows {
		revisions = append(revisions, Revision{
			ID:             revision.ID,
			VersionNumber:  revision.VersionNumber,
			GameVersion:    revision.GameVersion,
			Compat21:       Compat21(revision.Compat21),
			OriginalString: revision.OriginalString,
			Note:           revision.Note,
			CreatedAt:      revision.CreatedAt.Time,
		})
	}
	return revisions, nil
}

// GetRevision returns one hydrated revision of an entry.
func (s *Service) GetRevision(ctx context.Context, entryID, revisionID, viewerUserID string) (Revision, error) {
	entry, err := s.entries().Viewable(ctx, entryID, viewerUserID)
	if err != nil {
		return Revision{}, err
	}

	id, err := parseID(revisionID)
	if err != nil {
		return Revision{}, err
	}
	row, err := s.queries.GetRevision(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Revision{}, ErrNotFound
		}
		return Revision{}, fmt.Errorf("read revision: %w", err)
	}
	if row.EntryID != entry.ID {
		return Revision{}, ErrNotFound
	}
	revision, err := s.hydrateRevision(ctx, row.ID)
	if err != nil {
		return Revision{}, err
	}
	return *revision, nil
}

// GetLeafDetail returns a leaf from any revision of a live entry. Leaves from
// another entry or a soft-deleted entry are indistinguishable from missing ones.
// When ifNoneMatch equals the leaf's current ETag the result has NotModified
// set and skips decoding the exchange string.
func (s *Service) GetLeafDetail(
	ctx context.Context, entryID, blueprintID, viewerUserID, ifNoneMatch string,
) (LeafDetail, error) {
	entry, err := parseID(entryID)
	if err != nil {
		return LeafDetail{}, ErrNotFound
	}
	if _, err := s.entries().Viewable(ctx, entryID, viewerUserID); err != nil {
		return LeafDetail{}, err
	}
	blueprintIDValue, err := parseID(blueprintID)
	if err != nil {
		return LeafDetail{}, ErrNotFound
	}
	row, err := s.queries.GetEntryBlueprint(ctx, db.GetEntryBlueprintParams{
		ID: entry, ID_2: blueprintIDValue,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LeafDetail{}, ErrNotFound
		}
		return LeafDetail{}, fmt.Errorf("read entry blueprint: %w", err)
	}

	detail := LeafDetail{
		ID:    row.ID,
		Label: row.Label, Description: row.Description, Icons: row.Icons,
		ExchangeString: row.ExchangeString, EntityCount: row.EntityCount,
		Compat21: Compat21(row.Compat21),
		Revision: LeafRevisionContext{
			ID: row.RevisionID, VersionNumber: row.VersionNumber,
			GameVersion: row.GameVersion,
		},
	}
	if len(row.SnapToGrid) > 0 {
		if err := json.Unmarshal(row.SnapToGrid, &detail.SnapToGrid); err != nil {
			return LeafDetail{}, fmt.Errorf("decode snap to grid: %w", err)
		}
	}
	facts, err := s.queries.GetLeafProgress(ctx, row.ID)
	if err != nil {
		return LeafDetail{}, fmt.Errorf("read leaf progress: %w", err)
	}
	leaf := leafOf(facts)
	detail.PreviewStatus, detail.PreviewFailureCode = leaf.Preview.Status, leaf.Preview.Code
	detail.Production = Production{Availability: leaf.Analysis.Status, FailureCode: leaf.Analysis.Code}

	// A stored preview is shown whatever the status (a re-render may be
	// pending, or have failed and kept it).
	var previewSource string
	if facts.HasPreview {
		preview, err := s.queries.GetPreviewByBlueprint(ctx, row.ID)
		if err != nil {
			return LeafDetail{}, fmt.Errorf("read preview: %w", err)
		}
		detail.Preview = previewOf(preview.BlueprintID, db.ListPreviewsByRevisionRow(preview))
		previewSource = preview.SourceSha256
		if !detail.Preview.FullAvailable {
			previewSource += ":evicted"
		}
	}

	stat, err := s.queries.GetBlueprintStat(ctx, row.ID)
	hasStat := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LeafDetail{}, fmt.Errorf("read production stats: %w", err)
	}
	if hasStat {
		if detail.Entities, err = decodeEntityStats(stat.Entities); err != nil {
			return LeafDetail{}, fmt.Errorf("decode blueprint entities: %w", err)
		}
		if detail.Recipes, err = decodeRecipeStats(stat.Recipes); err != nil {
			return LeafDetail{}, fmt.Errorf("decode blueprint recipes: %w", err)
		}
		if detail.Mods, err = storedMods(stat); err != nil {
			return LeafDetail{}, fmt.Errorf("decode blueprint mods: %w", err)
		}
	}
	if leaf.Analysis.Status == ProductionReady && hasStat {
		production, productionErr := productionFromStat(stat)
		if productionErr != nil {
			slog.Error("decode stored production snapshot", "blueprint_id", row.ID, "error", productionErr)
		}
		detail.Production = production
	}

	// Parameters and schedules derive from the immutable exchange string, so
	// the blueprint and revision ids in the seed already cover them.
	etagSeed := row.ID + ":" + row.RevisionID
	if detail.Preview != nil {
		etagSeed += ":" + previewSource
	}
	etagSeed += ":" + string(detail.PreviewStatus) + ":" + detail.PreviewFailureCode
	if hasStat {
		etagSeed += ":mods=" + stat.ModsVersion
	}
	if hasStat && stat.ProductionCalculatedAt.Valid {
		etagSeed += ":" + stat.ProductionCalculatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	etagSeed += ":" + string(detail.Production.Availability) + ":" + detail.Production.FailureCode
	if detail.Production.Stale {
		etagSeed += ":stale"
	}
	detail.ETag = fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(etagSeed)))
	if ifNoneMatch == detail.ETag {
		detail.NotModified = true
		return detail, nil
	}

	display, err := blueprint.DecodeLeafDisplay(row.ExchangeString, s.limits.MaxDecodedBytes)
	if err != nil {
		return LeafDetail{}, fmt.Errorf("read blueprint parameters and schedules: %w", err)
	}
	detail.Parameters, detail.Schedules = display.Parameters, display.Schedules
	return detail, nil
}

// decodeEntityStats parses blueprint_stats.entities: an array of
// {name, count} rows summarizing the leaf's placed entities.
func decodeEntityStats(raw []byte) ([]blueprint.EntityStat, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entities []blueprint.EntityStat
	if err := json.Unmarshal(raw, &entities); err != nil {
		return nil, err
	}
	return entities, nil
}

func decodeRecipeStats(raw []byte) ([]blueprint.RecipeStat, error) {
	if len(raw) == 0 {
		return []blueprint.RecipeStat{}, nil
	}
	var recipes []blueprint.RecipeStat
	if err := json.Unmarshal(raw, &recipes); err != nil {
		return nil, err
	}
	return recipes, nil
}

// productionFromStat presents a stored production snapshot. A snapshot that
// no longer decodes is reported as a failed production, not a partial one.
func productionFromStat(stat db.BlueprintStat) (Production, error) {
	snapshot, err := analysisresult.ParseSnapshot(stat.Production, stat.ProductionSchemaVersion.Int32)
	if err != nil {
		return Production{Availability: ProductionFailed}, err
	}
	view, err := snapshot.View()
	if err != nil {
		return Production{Availability: ProductionFailed}, err
	}
	return Production{
		Availability:  ProductionReady,
		Stale:         stat.ProductionStale,
		SchemaVersion: stat.ProductionSchemaVersion.Int32,
		State:         stat.ProductionState.String,
		SpacePlatform: view.SpacePlatform,
		Ingredients:   view.Ingredients, Products: view.Products, Intermediates: view.Intermediates,
		Electricity: view.Electricity, Pollution: view.Pollution,
		Completeness: view.Completeness, Calculator: view.Calculator,
		Footprint: view.Footprint, BuildCost: view.BuildCost,
	}, nil
}

// Leaves returns every leaf blueprint at or below n, in tree order.
func (n *Node) Leaves() []Node {
	if n == nil {
		return nil
	}
	return collectLeaves(*n, nil)
}

func collectLeaves(node Node, into []Node) []Node {
	if node.Kind == blueprint.KindBlueprint {
		return append(into, node)
	}
	for _, child := range node.Children {
		into = collectLeaves(child, into)
	}
	return into
}

// hydrateRevision reads the shape-only tree, batch-fetches every book and
// blueprint row of the revision plus their previews, then joins shape to data.
func (s *Service) hydrateRevision(ctx context.Context, revisionID string) (*Revision, error) {
	return hydrateRevision(ctx, s.queries, revisionID)
}

// hydrateRevision is the one tree builder: reads and the create/revise
// responses (inside their transaction) both go through it, so a POST returns
// exactly what a later GET returns.
func hydrateRevision(ctx context.Context, queries *db.Queries, revisionID string) (*Revision, error) {
	if revisionID == "" {
		return nil, nil
	}
	row, err := queries.GetRevision(ctx, revisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read revision: %w", err)
	}

	var shape treeNode
	if err := json.Unmarshal(row.Tree, &shape); err != nil {
		return nil, fmt.Errorf("decode revision tree: %w", err)
	}

	books, err := queries.ListBooksByRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	blueprints, err := queries.ListBlueprintsByRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list blueprints: %w", err)
	}
	planners, err := queries.ListPlannersByRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list planners: %w", err)
	}
	previews, err := queries.ListPreviewsByRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list previews: %w", err)
	}
	leaves, err := queries.ListRevisionLeafProgress(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list leaf progress: %w", err)
	}

	index := newNodeIndex(books, blueprints, planners, previews, leaves)
	root, err := index.hydrate(shape)
	if err != nil {
		return nil, err
	}

	return &Revision{
		ID:             row.ID,
		VersionNumber:  row.VersionNumber,
		GameVersion:    row.GameVersion,
		Compat21:       Compat21(row.Compat21),
		OriginalString: row.OriginalString,
		Note:           row.Note,
		CreatedAt:      row.CreatedAt.Time,
		Root:           root,
	}, nil
}

// nodeIndex holds one revision's rows keyed by id so tree hydration is a
// lookup rather than a query per node.
type nodeIndex struct {
	books      map[string]db.Book
	blueprints map[string]db.Blueprint
	planners   map[string]db.Planner
	previews   map[string]db.ListPreviewsByRevisionRow
	leaves     map[string]progress.Leaf
}

func newNodeIndex(
	books []db.Book, blueprints []db.Blueprint, planners []db.Planner,
	previews []db.ListPreviewsByRevisionRow, leaves []db.LeafProgress,
) nodeIndex {
	index := nodeIndex{
		books:      make(map[string]db.Book, len(books)),
		blueprints: make(map[string]db.Blueprint, len(blueprints)),
		planners:   make(map[string]db.Planner, len(planners)),
		previews:   make(map[string]db.ListPreviewsByRevisionRow, len(previews)),
		leaves:     make(map[string]progress.Leaf, len(leaves)),
	}
	for _, book := range books {
		index.books[book.ID] = book
	}
	for _, leaf := range blueprints {
		index.blueprints[leaf.ID] = leaf
	}
	for _, planner := range planners {
		index.planners[planner.ID] = planner
	}
	for _, preview := range previews {
		index.previews[preview.BlueprintID] = preview
	}
	for _, leaf := range leaves {
		index.leaves[leaf.BlueprintID] = leafOf(leaf)
	}
	return index
}

// hydrate joins one shape node (and its children) with the stored row data.
func (i nodeIndex) hydrate(shape treeNode) (*Node, error) {
	if blueprint.Kind(shape.Kind).IsPlanner() {
		row, ok := i.planners[shape.ID]
		if !ok || row.Kind != shape.Kind {
			return nil, fmt.Errorf("revision tree references unknown planner %s", shape.ID)
		}
		settings, err := plannerSettings(row.Kind, row.Settings)
		if err != nil {
			return nil, fmt.Errorf("planner %s: %w", shape.ID, err)
		}
		return &Node{
			Kind: blueprint.Kind(shape.Kind), ID: shape.ID, Label: row.Label, Description: row.Description,
			Icons: row.Icons, ExchangeString: row.ExchangeString, Planner: settings,
		}, nil
	}
	if shape.Kind == string(blueprint.KindBlueprint) {
		row, ok := i.blueprints[shape.ID]
		if !ok {
			return nil, fmt.Errorf("revision tree references unknown blueprint %s", shape.ID)
		}
		node := &Node{
			Kind: blueprint.Kind(shape.Kind), ID: shape.ID, Label: row.Label, Description: row.Description,
			Icons: row.Icons, ExchangeString: row.ExchangeString, EntityCount: row.EntityCount,
			PreviewStatus: PreviewPending,
		}
		if leaf, ok := i.leaves[shape.ID]; ok {
			node.PreviewStatus, node.PreviewFailureCode = leaf.Preview.Status, leaf.Preview.Code
		}
		if preview, ok := i.previews[shape.ID]; ok {
			node.Preview = previewOf(shape.ID, preview)
		}
		return node, nil
	}

	row, ok := i.books[shape.ID]
	if !ok {
		return nil, fmt.Errorf("revision tree references unknown book %s", shape.ID)
	}
	node := &Node{
		Kind: blueprint.Kind(shape.Kind), ID: shape.ID, Label: row.Label,
		Description: row.Description, Icons: row.Icons,
		Children: make([]Node, 0, len(shape.Children)),
	}
	if row.ActiveIndex.Valid {
		activeIndex := row.ActiveIndex.Int32
		node.ActiveIndex = &activeIndex
	}
	for _, child := range shape.Children {
		hydrated, err := i.hydrate(child)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, *hydrated)
	}
	return node, nil
}

// PreviewURL and ThumbnailURL are the public, API-served locations of a
// blueprint's full preview and its thumbnail. Keyed by blueprint, never by
// storage key; the extension lets CDNs cache them by default.
func PreviewURL(blueprintID string) string {
	return "/api/v1/blueprints/" + blueprintID + "/preview.webp"
}

func ThumbnailURL(blueprintID string) string {
	return "/api/v1/blueprints/" + blueprintID + "/thumbnail.webp"
}

func previewOf(blueprintID string, row db.ListPreviewsByRevisionRow) *Preview {
	width, height := previewimage.ThumbnailSize(int(row.Width), int(row.Height))
	// A small preview is stored as its thumbnail alone: point the full URL at
	// the same file so browsers and CDNs keep one copy.
	url := PreviewURL(blueprintID)
	if !previewimage.HasFull(int(row.Width), int(row.Height)) {
		url = ThumbnailURL(blueprintID)
	}
	return &Preview{
		URL: url, ThumbnailURL: ThumbnailURL(blueprintID),
		MediaType: previewimage.MediaType, Width: row.Width, Height: row.Height,
		ThumbnailWidth: int32(width), ThumbnailHeight: int32(height),
		FullAvailable: row.FullStored || !previewimage.HasFull(int(row.Width), int(row.Height)),
		CreatedAt:     row.CreatedAt.Time,
	}
}

// PreviewFile names a viewable blueprint's stored preview files.
type PreviewFile struct {
	// SourceSHA256 names the files (preview.Files) and is their ETag; the
	// dimensions say which files exist (previewimage.HasFull).
	SourceSHA256  string
	Width, Height int
	// Shareable reports whether shared caches (CDNs) may store the
	// image: anyone with the link may see public and unlisted entries.
	Shareable bool
}

// PreviewSource returns a blueprint's preview files if the viewer may see
// them. Previews follow their entry's visibility.
func (s *Service) PreviewSource(ctx context.Context, blueprintID, viewerUserID string) (PreviewFile, error) {
	id, err := parseID(blueprintID)
	if err != nil {
		return PreviewFile{}, err
	}
	preview, err := s.queries.GetPreviewWithEntryAccess(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PreviewFile{}, ErrNotFound
		}
		return PreviewFile{}, fmt.Errorf("read preview: %w", err)
	}
	if !visibleTo(preview.Visibility, preview.OwnerUserID, viewerUserID) {
		return PreviewFile{}, ErrNotFound
	}
	return PreviewFile{
		SourceSHA256: preview.SourceSha256, Shareable: preview.Visibility != "private",
		Width: int(preview.Width), Height: int(preview.Height),
	}, nil
}

// ExportRevisionString rebuilds the exchange string of a revision's root node.
// Book strings are not stored, so they are re-encoded from the stored leaves.
func (s *Service) ExportRevisionString(ctx context.Context, entryID, revisionID, viewerUserID string) (string, error) {
	revision, err := s.GetRevision(ctx, entryID, revisionID, viewerUserID)
	if err != nil {
		return "", err
	}
	if revision.Root == nil {
		return "", ErrNotFound
	}
	exported, err := blueprint.EncodeTree(exportNodeOf(*revision.Root), s.limits)
	if err != nil {
		return "", fmt.Errorf("encode revision: %w", err)
	}
	return exported, nil
}

// ExportNodeString returns the exchange string of one book, blueprint or
// planner of any revision of an entry the viewer may see; a book is
// re-encoded from its stored leaves and planners.
func (s *Service) ExportNodeString(ctx context.Context, nodeID, viewerUserID string) (string, error) {
	id, err := parseID(nodeID)
	if err != nil {
		return "", err
	}
	row, err := s.queries.GetNodeRevision(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read node revision: %w", err)
	}
	revision, err := s.GetRevision(ctx, row.EntryID, row.RevisionID, viewerUserID)
	if err != nil {
		return "", err
	}
	if revision.Root == nil {
		return "", ErrNotFound
	}
	node := findNode(*revision.Root, id)
	if node == nil {
		return "", ErrNotFound
	}
	exported, err := blueprint.EncodeTree(exportNodeOf(*node), s.limits)
	if err != nil {
		return "", fmt.Errorf("encode node: %w", err)
	}
	return exported, nil
}

func findNode(node Node, id string) *Node {
	if node.ID == id {
		return &node
	}
	for _, child := range node.Children {
		if found := findNode(child, id); found != nil {
			return found
		}
	}
	return nil
}

func exportNodeOf(node Node) blueprint.ExportNode {
	out := blueprint.ExportNode{
		Kind: blueprint.Kind(node.Kind), Label: node.Label, Description: node.Description,
		Icons: node.Icons, ActiveIndex: node.ActiveIndex, ExchangeString: node.ExchangeString,
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, exportNodeOf(child))
	}
	return out
}
