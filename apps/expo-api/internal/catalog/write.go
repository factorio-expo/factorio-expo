package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/blueprint"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/internal/shortid"
)

// Authorization is the proof a caller presents when mutating an entry: either
// the authenticated owner's user id, or the anonymous edit token.
type Authorization struct {
	UserID    string
	EditToken string
}

// planNode is a decoded node with its future database id already assigned, so
// the shape-only tree can be written to the revision before any node row
// exists.
type planNode struct {
	id       string
	source   blueprint.Node
	children []planNode
}

// CreateEntry decodes a submission, then persists the entry, its first
// immutable revision, and every book/blueprint node in one transaction. An
// anonymous submission (no owner) receives an edit token, which is returned
// once and never again.
func (s *Service) CreateEntry(ctx context.Context, submission Submission) (Entry, error) {
	if err := s.allowUpload(ctx, submission.OwnerUserID, submission.IPHash); err != nil {
		return Entry{}, err
	}
	root, err := blueprint.Parse(submission.Exchange, s.limits)
	if err != nil {
		return Entry{}, err
	}
	title, description, err := entryMetadata(submission, root)
	if err != nil {
		return Entry{}, err
	}
	tags, err := s.validateTagSlugs(ctx, submission.Tags)
	if err != nil {
		return Entry{}, err
	}
	ownerID, err := submissionOwner(submission.OwnerUserID)
	if err != nil {
		return Entry{}, err
	}
	visibility, err := entryVisibility(submission.Visibility, ownerID != "")
	if err != nil {
		return Entry{}, err
	}
	expiresAt, err := expiryAfter(submission.ExpiresInDays, time.Now())
	if err != nil {
		return Entry{}, err
	}
	alias, err := newEntryAlias(submission.Alias, ownerID)
	if err != nil {
		return Entry{}, err
	}
	return s.insertEntry(ctx, newEntry{
		exchange: submission.Exchange, root: root, owner: ownerID, visibility: visibility,
		title: title, description: description, tags: tags, expiresAt: expiresAt, spoiler: submission.Spoiler,
		sourceURL: submission.SourceURL, alias: alias,
		ticket: s.queue.Upload(renderqueue.Requester{UserID: ownerID, IPHash: submission.IPHash}),
	})
}

// newEntry is a validated submission, ready to be written.
type newEntry struct {
	exchange           string
	root               blueprint.Node
	owner              string
	visibility         string
	title, description string
	tags               []string
	// sourceURL is the imported link (empty for an upload); alias the vanity slug
	// (normalized, empty for none).
	sourceURL, alias string
	spoiler          bool
	// expiresAt is when the entry is removed; null lives until deleted.
	expiresAt pgtype.Timestamptz
	// quickPreview makes it a quick preview: never indexed or listed.
	quickPreview bool
	// ticket places its leaves in the render queue.
	ticket renderqueue.Ticket
}

func submissionOwner(userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	ownerID, err := parseID(userID)
	if err != nil {
		return "", ErrForbidden
	}
	return ownerID, nil
}

// insertEntry writes the entry, its tags, its first revision with every node
// and the dispatch intents in one change, and queues a search index unless it
// is a quick preview.
func (s *Service) insertEntry(ctx context.Context, input newEntry) (Entry, error) {
	// Only anonymous entries get an edit token; for owned entries, ownership
	// governs edit rights instead.
	editToken := ""
	if input.owner == "" {
		var err error
		if editToken, err = newEditToken(); err != nil {
			return Entry{}, fmt.Errorf("generate edit token: %w", err)
		}
	}

	var entry Entry
	err := s.inChange(ctx, func(c *change) error {
		queries := c.queries
		row, err := queries.CreateEntry(ctx, db.CreateEntryParams{
			ID:            shortid.New(),
			OwnerUserID:   pgText(input.owner),
			EditTokenHash: pgText(hashEditToken(editToken)),
			Visibility:    input.visibility,
			Title:         input.title,
			Description:   input.description,
			ExpiresAt:     input.expiresAt,
			QuickPreview:  input.quickPreview,
			Spoiler:       input.spoiler,
			SourceUrl:     pgText(input.sourceURL),
		})
		if err != nil {
			return fmt.Errorf("create entry: %w", err)
		}
		if input.alias != "" {
			if err := setAlias(ctx, queries, row, input.alias); err != nil {
				return err
			}
		}
		if err := queries.SetEntryTags(ctx, db.SetEntryTagsParams{EntryID: row.ID, Slugs: input.tags}); err != nil {
			return fmt.Errorf("set entry tags: %w", err)
		}
		revision, reused, err := s.writeRevision(ctx, c, row.ID, input.exchange, "", input.root, input.ticket)
		if err != nil {
			return err
		}
		if input.quickPreview {
			// A quick preview is the whole package or nothing: a string that
			// already failed for good is refused now, and nothing is stored.
			for _, leaf := range reused {
				if leaf.FailureCode != "" {
					return &PreviewFailedError{Code: leaf.FailureCode}
				}
			}
		}
		updated, err := queries.SetEntryCurrentRevision(ctx, db.SetEntryCurrentRevisionParams{
			ID:                row.ID,
			CurrentRevisionID: pgText(revision.ID),
		})
		if err != nil {
			return fmt.Errorf("set current revision: %w", err)
		}
		entry = entryFromRow(updated)
		entry.EditToken = editToken
		entry.Alias = input.alias
		entry.Revision = revision
		tagRows, err := queries.ListTagsByEntry(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("list entry tags: %w", err)
		}
		entry.Tags = tagSlugs(tagRows)
		return c.entryWritten(ctx, nil, updated)
	})
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func entryMetadata(submission Submission, root blueprint.Node) (string, string, error) {
	summary := blueprint.Summarize(root)
	title, description := summary.Label, summary.Description
	if submission.Title != nil {
		title = strings.TrimSpace(*submission.Title)
		description = ""
		if title == "" || utf8.RuneCountInString(title) > MaxTitleRunes {
			return "", "", ErrInvalidEntryMetadata
		}
		if submission.Description != nil {
			description = strings.TrimSpace(*submission.Description)
		}
	}
	if utf8.RuneCountInString(description) > MaxDescriptionRunes {
		return "", "", ErrInvalidEntryMetadata
	}
	return title, description, nil
}

// entryVisibility validates a submission's visibility against the entries
// CHECKs: "public" (the default), "unlisted" or "private"; the last two need
// an owner, so an anonymous entry is always public.
func entryVisibility(visibility string, owned bool) (string, error) {
	switch visibility {
	case "", "public":
		return "public", nil
	case "unlisted", "private":
		if !owned {
			return "", ErrForbidden
		}
		return visibility, nil
	default:
		return "", ErrInvalidVisibility
	}
}

func (s *Service) validateTagSlugs(ctx context.Context, slugs []string) ([]string, error) {
	unique := make([]string, 0, len(slugs))
	seen := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; !ok {
			seen[slug] = struct{}{}
			unique = append(unique, slug)
		}
	}
	if len(unique) == 0 {
		return unique, nil
	}

	known, err := s.queries.ListTagsBySlugs(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("validate entry tags: %w", err)
	}
	if len(known) != len(unique) {
		return nil, ErrInvalidTags
	}
	return unique, nil
}

func tagSlugs(tags []db.Tag) []string {
	slugs := make([]string, 0, len(tags))
	for _, tag := range tags {
		slugs = append(slugs, tag.Slug)
	}
	return slugs
}

// CreateRevision adds a new immutable revision to an existing entry and makes
// it the live one. Past revisions and their nodes are left untouched.
func (s *Service) CreateRevision(
	ctx context.Context, entryID string, auth Authorization, submission Submission,
) (Entry, error) {
	if err := s.allowUpload(ctx, auth.UserID, submission.IPHash); err != nil {
		return Entry{}, err
	}
	root, err := blueprint.Parse(submission.Exchange, s.limits)
	if err != nil {
		return Entry{}, err
	}
	note, err := normalizeNote(submission.Note)
	if err != nil {
		return Entry{}, err
	}

	var entry Entry
	err = s.inChange(ctx, func(c *change) error {
		queries := c.queries
		existing, err := entryAccess{queries: queries}.Editable(ctx, entryID, auth)
		if err != nil {
			return err
		}
		ticket := s.queue.Upload(renderqueue.Requester{UserID: auth.UserID, IPHash: submission.IPHash})
		revision, _, err := s.writeRevision(ctx, c, existing.ID, submission.Exchange, note, root, ticket)
		if err != nil {
			return err
		}
		updated, err := queries.SetEntryCurrentRevision(ctx, db.SetEntryCurrentRevisionParams{
			ID:                existing.ID,
			CurrentRevisionID: pgText(revision.ID),
		})
		if err != nil {
			return fmt.Errorf("set current revision: %w", err)
		}
		entry = entryFromRow(updated)
		entry.Revision = revision
		tagRows, err := queries.ListTagsByEntry(ctx, existing.ID)
		if err != nil {
			return fmt.Errorf("list entry tags: %w", err)
		}
		entry.Tags = tagSlugs(tagRows)
		return c.entryWritten(ctx, &existing, updated)
	})
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// writeRevision stores one immutable revision: it assigns node ids up front,
// writes the shape-only tree together with the revision row, and only then
// inserts the book/blueprint rows the tree points at, with a preview and an
// analysis intent per leaf under ticket. Leaves whose string another leaf
// already has a result for take it (reuse.go); what still needs work must fit
// the ticket's cap (renderqueue.ErrFull), else the whole change rolls back.
func (s *Service) writeRevision(
	ctx context.Context, c *change, entryID string,
	exchange, note string, root blueprint.Node, ticket renderqueue.Ticket,
) (*Revision, []renderqueue.ReusedLeaf, error) {
	queries := c.queries
	plan := planTree(root)
	tree, err := json.Marshal(shapeOf(plan))
	if err != nil {
		return nil, nil, fmt.Errorf("encode revision tree: %w", err)
	}

	row, err := queries.CreateRevision(ctx, db.CreateRevisionParams{
		ID:             shortid.New(),
		EntryID:        entryID,
		OriginalString: exchange,
		Tree:           tree,
		GameVersion:    blueprint.GameVersion(root),
		Compat21:       blueprint.Compat21(root),
		Note:           note,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create revision: %w", err)
	}

	if err := insertNodes(ctx, queries, s.mods, row.ID, plan, new(int32)); err != nil {
		return nil, nil, err
	}
	leaves := leafIDs(plan, nil)
	places, err := renderqueue.Place(ctx, queries, ticket, time.Now(), len(leaves))
	if err != nil {
		return nil, nil, err
	}
	for i, blueprintID := range leaves {
		for _, phase := range []string{progress.PhasePreview, progress.PhaseAnalysis} {
			if err := queries.EnqueuePhaseJob(ctx, db.EnqueuePhaseJobParams{
				BlueprintID: blueprintID, Phase: phase, QueueKey: ticket.Key, OrderAt: places[i],
			}); err != nil {
				return nil, nil, fmt.Errorf("enqueue %s job: %w", phase, err)
			}
		}
		c.queuedWork()
	}
	reused, err := reuseResults(ctx, queries, pgText(row.ID))
	if err != nil {
		return nil, nil, err
	}
	// The intents written above count; Place locked the key.
	if err := renderqueue.Check(ctx, queries, ticket, 0); err != nil {
		return nil, nil, err
	}

	revision, err := hydrateRevision(ctx, queries, row.ID)
	return revision, reused, err
}

// leafIDs collects the blueprint ids of a planned tree in depth-first order.
func leafIDs(plan planNode, into []string) []string {
	if plan.source.Kind == blueprint.KindBlueprint {
		return append(into, plan.id)
	}
	for _, child := range plan.children {
		into = leafIDs(child, into)
	}
	return into
}

// planTree assigns a database id to every decoded node, depth-first in source
// order, so child order in the stored tree is the submitted order.
func planTree(node blueprint.Node) planNode {
	plan := planNode{id: shortid.New(), source: node}
	if node.Kind == blueprint.KindBook {
		plan.children = make([]planNode, 0, len(node.Book.Children))
		for _, child := range node.Book.Children {
			plan.children = append(plan.children, planTree(child))
		}
	}
	return plan
}

// shapeOf reduces a planned tree to the shape-only jsonb form: kind, id, and
// children, with no duplicated label, icon, or exchange-string data.
func shapeOf(plan planNode) treeNode {
	shape := treeNode{Kind: string(plan.source.Kind), ID: plan.id}
	for _, child := range plan.children {
		shape.Children = append(shape.Children, shapeOf(child))
	}
	return shape
}

// storeBlueprintStructure persists the structural entity/recipe breakdown
// computed at decode time and the mods it needs, independent of (and before)
// any production analysis for the leaf.
func storeBlueprintStructure(
	ctx context.Context, queries *db.Queries, modTable *mods.Table, blueprintID string, leaf *blueprint.Blueprint,
) error {
	entities := leaf.Entities
	if entities == nil {
		entities = []blueprint.EntityStat{}
	}
	recipes := leaf.Recipes
	if recipes == nil {
		recipes = []blueprint.RecipeStat{}
	}
	entitiesJSON, err := json.Marshal(entities)
	if err != nil {
		return fmt.Errorf("encode blueprint entities: %w", err)
	}
	recipesJSON, err := json.Marshal(recipes)
	if err != nil {
		return fmt.Errorf("encode blueprint recipes: %w", err)
	}
	modsJSON, err := json.Marshal(detectMods(modTable, entities, recipes))
	if err != nil {
		return fmt.Errorf("encode blueprint mods: %w", err)
	}
	if err := queries.UpsertBlueprintStructure(ctx, db.UpsertBlueprintStructureParams{
		BlueprintID: blueprintID, Entities: entitiesJSON, Recipes: recipesJSON,
		Mods: modsJSON, ModsVersion: modTable.Version(),
	}); err != nil {
		return fmt.Errorf("store blueprint structure: %w", err)
	}
	return nil
}

// insertNodes writes every book, blueprint and planner row of a planned tree
// (blueprint leaves get consecutive depth-first positions from nextLeaf).
func insertNodes(
	ctx context.Context, queries *db.Queries, modTable *mods.Table, revisionID string, plan planNode, nextLeaf *int32,
) error {
	if plan.source.Kind.IsPlanner() {
		return insertPlanner(ctx, queries, revisionID, plan)
	}
	if plan.source.Kind == blueprint.KindBlueprint {
		leaf := plan.source.Blueprint
		var snap []byte
		if leaf.SnapToGrid != nil {
			var err error
			if snap, err = json.Marshal(leaf.SnapToGrid); err != nil {
				return fmt.Errorf("encode snap to grid: %w", err)
			}
		}
		row, err := queries.CreateBlueprint(ctx, db.CreateBlueprintParams{
			ID:             plan.id,
			RevisionID:     revisionID,
			Label:          leaf.Label,
			Description:    leaf.Description,
			Icons:          leaf.Icons,
			ExchangeString: leaf.ExchangeString,
			ExchangeSha256: exchangeHash(leaf.ExchangeString),
			EntityCount:    leaf.EntityCount,
			Position:       *nextLeaf,
			SnapToGrid:     snap,
			Compat21:       leaf.Compat21,
		})
		*nextLeaf++
		if err != nil {
			return fmt.Errorf("create blueprint: %w", err)
		}
		return storeBlueprintStructure(ctx, queries, modTable, row.ID, leaf)
	}

	book := plan.source.Book
	activeIndex := pgtype.Int4{}
	if book.ActiveIndex != nil {
		activeIndex = pgtype.Int4{Int32: *book.ActiveIndex, Valid: true}
	}
	if _, err := queries.CreateBook(ctx, db.CreateBookParams{
		ID:          plan.id,
		RevisionID:  revisionID,
		Label:       book.Label,
		Description: book.Description,
		Icons:       book.Icons,
		ActiveIndex: activeIndex,
	}); err != nil {
		return fmt.Errorf("create book: %w", err)
	}
	for _, child := range plan.children {
		if err := insertNodes(ctx, queries, modTable, revisionID, child, nextLeaf); err != nil {
			return err
		}
	}
	return nil
}

// insertPlanner writes one planner row with its settings.
func insertPlanner(ctx context.Context, queries *db.Queries, revisionID string, plan planNode) error {
	planner := plan.source.Planner
	settings, err := json.Marshal(planner.Settings)
	if err != nil {
		return fmt.Errorf("encode planner settings: %w", err)
	}
	if err := queries.CreatePlanner(ctx, db.CreatePlannerParams{
		ID:             plan.id,
		RevisionID:     revisionID,
		Kind:           string(plan.source.Kind),
		Label:          planner.Label,
		Description:    planner.Description,
		Icons:          planner.Icons,
		ExchangeString: planner.ExchangeString,
		Settings:       settings,
	}); err != nil {
		return fmt.Errorf("create planner: %w", err)
	}
	return nil
}

// inTransaction runs fn against a transaction-scoped query set, committing on
// success and rolling back on any error or panic.
func (s *Service) inTransaction(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func entryFromRow(row db.Entry) Entry {
	entry := Entry{
		SourceURL:   row.SourceUrl.String,
		ID:          row.ID,
		Title:       row.Title,
		Description: row.Description,
		Tags:        []string{},
		Records:     []BoardRecord{},
		OwnerUserID: row.OwnerUserID.String,
		Visibility:  Visibility(row.Visibility),
		Spoiler:     row.Spoiler,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		entry.ExpiresAt = &expiresAt
	}
	entry.QuickPreview = row.QuickPreview
	return entry
}

// exchangeHash is blueprints.exchange_sha256: how reuse finds leaves with
// the same string.
func exchangeHash(exchange string) []byte {
	sum := sha256.Sum256([]byte(exchange))
	return sum[:]
}
