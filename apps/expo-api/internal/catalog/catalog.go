// Package catalog owns the public blueprint catalog: entries, immutable
// revisions, and the recursive book/blueprint node rows that belong to them.
// It is the only place that writes catalog tables; HTTP handlers translate
// between transport concerns and this domain.
package catalog

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/blueprint"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/settings"
	"factorio-expo/internal/analysisresult"
)

var (
	// ErrNotFound is returned when an entry, revision, or blueprint does not
	// exist (or is not visible to the caller).
	ErrNotFound = fault.New(fault.NotFound, "not_found", "not found")
	// ErrForbidden is returned when the caller may not modify an entry.
	ErrForbidden = fault.New(fault.Forbidden, "forbidden", "not allowed to modify this entry")
	// ErrLastRevision is returned when deleting a revision would leave an
	// entry with no live revision.
	ErrLastRevision = fault.New(fault.Conflict, "last_revision", "an entry must keep at least one revision")
	// ErrInvalidEntryMetadata is returned when the user-provided entry title or
	// description exceeds the supported bounds or the title is blank.
	ErrInvalidEntryMetadata = fault.New(fault.Invalid, "invalid_entry_metadata", fmt.Sprintf(
		"title is required and must be at most %d characters; description must be at most %d characters",
		MaxTitleRunes, MaxDescriptionRunes))
	// ErrInvalidTags is returned when a submission includes an unknown tag.
	ErrInvalidTags = fault.New(fault.Invalid, "invalid_tags", "one or more tags are invalid")
	// ErrInvalidVisibility is returned when a submission's visibility is
	// not "public", "unlisted" or "private".
	ErrInvalidVisibility = fault.New(fault.Invalid, "invalid_visibility", "visibility must be public, unlisted or private")
	// ErrPreviewOfBook is returned when a quick preview is asked for a book:
	// it takes one blueprint, so a preview stays a short job.
	ErrPreviewOfBook = fault.New(fault.Unprocessable, "preview_of_book", "a quick preview takes a single blueprint, not a book")
	// ErrQuickPreview is returned when a change other than publishing is asked
	// of a quick preview (metadata, revisions, delete, favorites, collections).
	ErrQuickPreview = fault.New(fault.Conflict, "quick_preview", "a quick preview cannot be changed; publish it first")
	// ErrInvalidExpiry is returned when an upload or publish asks for a
	// lifetime that is not one of ExpiryDays (or 0, never).
	ErrInvalidExpiry = fault.New(fault.Invalid, "invalid_expiry", "Choose never, 1, 7, 14 or 30 days for the expiry.")
	// ErrInvalidRevisionNote is returned when a revision note is over its length or has
	// control characters.
	ErrInvalidRevisionNote = fault.New(fault.Invalid, "invalid_revision_note", fmt.Sprintf(
		"a revision note must be at most %d characters of plain text", MaxRevisionNoteRunes))
	// ErrAlreadyPublished is returned when publishing an entry that already
	// has an owner and is not a quick preview.
	ErrAlreadyPublished = fault.New(fault.Conflict, "already_published", "entry is already published to an account")
	// ErrPreviewNotReady is returned when publishing a quick preview whose
	// render or analysis has not finished.
	ErrPreviewNotReady = fault.New(fault.Conflict, "preview_not_ready", "The preview is still being prepared. Try again in a moment.")
)

// PreviewFailedError refuses a quick preview of a string that already failed
// for good (Code, a reusable final failure): it would fail the same way.
type PreviewFailedError struct {
	Code string
}

func (e *PreviewFailedError) Error() string {
	return "This blueprint already failed to render or analyse."
}

// Fault answers it as preview_failed with the failure's code.
func (e *PreviewFailedError) Fault() *fault.Error {
	return fault.New(fault.Unprocessable, "preview_failed", e.Error()).With("failure_code", e.Code)
}

// The bounds of an entry's title and description, in characters.
const (
	MaxTitleRunes       = 160
	MaxDescriptionRunes = 5000
	// MaxRevisionNoteRunes is the longest revision note (revisions.note).
	MaxRevisionNoteRunes = 280
)

// Service reads and writes the catalog.
type Service struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	limits     blueprint.Limits
	previewTTL time.Duration
	// ipBanDuration is how long a banned account's address stays banned.
	ipBanDuration time.Duration
	// progress and workQueued hear about committed changes (change.go).
	progress   *progress.Hub
	workQueued func()
	// entryHidden is told, after a commit, which cached URLs must go (change.hide);
	// nil skips the lookup.
	entryHidden func(entryID string, revisionIDs, blueprintIDs []string)
	// tagRules are the tag suggestion rules (LoadTagSuggestionRules);
	// tagMinScore is the score a suggested tag needs.
	tagRules    atomic.Pointer[[]tagSuggestionRule]
	tagMinScore int
	// mods is the mod name table detection runs against.
	mods *mods.Table
	// banHolds hashes a deleted banned account's username and identities.
	banHolds *banhold.Hasher
	// queue places new leaves in the render queue and caps them.
	queue renderqueue.Policy
	// settings caches the global settings (settings.go).
	settings *settings.Cache
	// scores caches each challenge's scored board (challengecache.go).
	scores scoreCache
	// boards caches the ranked ship record boards (leaderboards.go).
	boards boardCache
}

// DefaultPreviewTTL matches the runner's default artifact TTL.
const DefaultPreviewTTL = 24 * time.Hour

// DefaultIPBanDuration is how long an address ban lasts by default: hours,
// since addresses rotate.
const DefaultIPBanDuration = 6 * time.Hour

// Options configures a catalog Service.
type Options struct {
	// Limits bounds traversal of untrusted submissions.
	Limits blueprint.Limits
	// PreviewTTL is how long a quick preview lives; it must not exceed the
	// runner's artifact TTL, since the preview image is proxied from there.
	PreviewTTL time.Duration
	// IPBanDuration is how long a banned account's address stays banned.
	IPBanDuration time.Duration
	// ChallengeScoresTTL is how long a challenge's scored board is reused;
	// zero turns the cache off (DefaultChallengeScoresTTL in production).
	ChallengeScoresTTL time.Duration
	// TagSuggestionMinScore is the score a tag needs before inspect suggests
	// it (DefaultTagSuggestionMinScore when 0).
	TagSuggestionMinScore int
	// Progress is told, after each commit, which entries changed (open entry
	// streams re-read them); nil drops it.
	Progress *progress.Hub
	// WorkQueued is called after a commit that queued preview or analysis
	// intents: the preview dispatcher's Nudge. Nil leaves them to its poll.
	WorkQueued func()
	// EntryHidden is called after a commit that made an entry private or took it
	// down (deleted, expired), with the ids of all its revisions and blueprints:
	// the edge cache purge of its preview images and raw strings. Nil does
	// nothing.
	EntryHidden func(entryID string, revisionIDs, blueprintIDs []string)
	// Mods is the mod name table; nil uses the embedded one (mods.Embedded).
	Mods *mods.Table
	// BanHolds hashes a deleted banned account's username and identities
	// (ban_holds). Required to delete accounts.
	BanHolds *banhold.Hasher
	// RenderQueue places uploads in the render queue and caps them; the zero
	// value means renderqueue.Defaults().
	RenderQueue renderqueue.Policy
}

// New builds a catalog service bound to a Postgres pool, applying defaults for
// any unset option.
func New(pool *pgxpool.Pool, options Options) *Service {
	if options.Limits.MaxDecodedBytes == 0 {
		options.Limits = blueprint.DefaultLimits()
	}
	if options.PreviewTTL <= 0 {
		options.PreviewTTL = DefaultPreviewTTL
	}
	if options.IPBanDuration <= 0 {
		options.IPBanDuration = DefaultIPBanDuration
	}
	if options.Mods == nil {
		options.Mods = mods.Embedded()
	}
	if options.RenderQueue == (renderqueue.Policy{}) {
		options.RenderQueue = renderqueue.Defaults()
	}
	if options.TagSuggestionMinScore <= 0 {
		options.TagSuggestionMinScore = DefaultTagSuggestionMinScore
	}
	queries := db.New(pool)
	return &Service{
		pool:          pool,
		queries:       queries,
		settings:      settings.NewCache(loadSettings(queries), 0),
		limits:        options.Limits,
		previewTTL:    options.PreviewTTL,
		ipBanDuration: options.IPBanDuration,
		progress:      options.Progress,
		workQueued:    options.WorkQueued,
		entryHidden:   options.EntryHidden,
		tagMinScore:   options.TagSuggestionMinScore,
		mods:          options.Mods,
		banHolds:      options.BanHolds,
		queue:         options.RenderQueue,
		scores:        scoreCache{ttl: options.ChallengeScoresTTL},
	}
}

// Submission is one upload of an exchange string. It creates either a brand
// new entry or a new revision of one. The render target is not part of a
// submission: the API decides which pinned content set a blueprint is
// interpreted against, because the exchange string only reveals the game
// version that exported it and nothing about active mods.
type Submission struct {
	Exchange    string
	Title       *string
	Description *string
	Tags        []string
	OwnerUserID string
	Visibility  string
	// Spoiler marks the entry spoiler/NSFW.
	Spoiler bool
	// ExpiresInDays is the chosen lifetime (ExpiryDays); 0 never expires.
	// Revisions ignore it.
	ExpiresInDays int
	// IPHash is the keyed hash of the uploader's address, for upload bans
	// (nil when unknown).
	IPHash []byte
	// SourceURL is the canonical link an imported blueprint came from (the
	// importer's, https only); empty for an upload. Entries only.
	SourceURL string
	// Alias is the vanity slug asked for (alias.go): signed-in uploaders only.
	// Entries only.
	Alias string
	// Note is the revision's change note (at most MaxRevisionNoteRunes); revisions only.
	Note string
}

// A leaf's preview status: its progress phase state (progress.LeafOf).
const (
	PreviewPending = progress.StatusPending
	PreviewReady   = progress.StatusReady
	PreviewFailed  = progress.StatusFailed
)

// Entry is the public catalog resource together with its live revision.
type Entry struct {
	ID          string   `json:"id" doc:"The entry's public id."`
	Title       string   `json:"title" doc:"The entry's title."`
	Description string   `json:"description" doc:"The entry's description."`
	Tags        []string `json:"tags" doc:"The tag slugs."`
	OwnerUserID string   `json:"owner_user_id,omitempty" doc:"The uploader's account id; absent for an anonymous entry."`
	// OwnerUsername is the uploader's public name; empty for anonymous entries.
	OwnerUsername string     `json:"owner_username,omitempty" doc:"The uploader's public name; absent for an anonymous entry."`
	Visibility    Visibility `json:"visibility" doc:"Who may see the entry."`
	// Spoiler marks a spoiler/NSFW entry: clients hide its previews until a
	// click.
	Spoiler   bool      `json:"spoiler" doc:"Whether the entry is marked as a spoiler or NSFW: clients hide its preview."`
	CreatedAt time.Time `json:"created_at" doc:"When the entry was created."`
	UpdatedAt time.Time `json:"updated_at" doc:"When the entry was last changed."`
	// IsFavorited is whether the viewer has favorited it; false for anonymous viewers.
	IsFavorited bool `json:"is_favorited" doc:"Whether the viewer favorited it; false when signed out."`
	// IsPinned is whether the viewer pinned it to their profile (only an
	// owner can).
	IsPinned bool `json:"is_pinned" doc:"Whether the viewer pinned it to their profile (owner only)."`
	// ViewCount is the all-time page views (internal/views), up to one flush
	// interval behind.
	ViewCount int64 `json:"view_count" doc:"The all-time page views (counted once per visitor per day)."`
	// SourceURL is where an imported blueprint came from.
	SourceURL string `json:"source_url,omitempty" doc:"The link an imported blueprint came from; absent for an upload."`
	// Alias is the vanity slug of /@{owner_username}/{alias}.
	Alias string `json:"alias,omitempty" doc:"The vanity alias: the entry is also served at /@{owner_username}/{alias}; absent when it has none."`
	// ExpiresAt is when the entry is gone: always set on a quick preview,
	// set on an upload that chose a lifetime.
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When the entry is gone: always set on a quick preview or an entry uploaded with a lifetime."`
	// QuickPreview marks an unpublished quick preview: unlisted, read-only,
	// it can only be published or left to expire.
	QuickPreview bool `json:"quick_preview,omitempty" doc:"Whether it is an unpublished quick preview: unlisted, read-only, gone after a day."`
	// Challenges are the published challenges the entry is entered in.
	Challenges []EntryChallenge `json:"challenges" doc:"The published challenges the entry is entered in."`
	// Records are the leaderboard boards its blueprints lead now
	// (board_leaders), whatever revision is viewed.
	Records []BoardRecord `json:"records" doc:"The leaderboard boards the entry's blueprints lead now, in board order; each names its blueprint."`
	// EditToken is only populated on creation; it is the one-time secret an
	// anonymous uploader needs in order to edit or delete the entry later.
	EditToken string    `json:"edit_token,omitempty" doc:"The one-time edit token of an anonymous entry; only set on creation."`
	Revision  *Revision `json:"revision,omitempty" doc:"The live revision."`
}

// EntrySummary is the flat listing projection of an entry: what a catalog card
// needs, with no recursive tree and no per-leaf data. Built only by entryCards
// (cards.go).
type EntrySummary struct {
	ID            string     `json:"id" doc:"The entry's public id."`
	Title         string     `json:"title" doc:"The entry's title."`
	OwnerUserID   string     `json:"owner_user_id,omitempty" doc:"The uploader's account id; absent for an anonymous entry."`
	OwnerUsername string     `json:"owner_username,omitempty" doc:"The uploader's username; absent for an anonymous entry."`
	Visibility    Visibility `json:"visibility" doc:"Who may see the entry."`
	// Spoiler marks a spoiler/NSFW entry.
	Spoiler   bool      `json:"spoiler" doc:"Whether the entry is marked as a spoiler or NSFW: clients hide its preview."`
	CreatedAt time.Time `json:"created_at" doc:"When the entry was created."`
	UpdatedAt time.Time `json:"updated_at" doc:"When the entry was last changed."`
	// ExpiresAt is set when the entry was uploaded with a lifetime.
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When the entry expires, if it was uploaded with a lifetime."`

	RevisionID    string `json:"revision_id" doc:"The live revision's id."`
	VersionNumber int32  `json:"version_number" doc:"The live revision's number."`
	GameVersion   string `json:"game_version" doc:"The Factorio version that exported it."`
	// Compat21 is the Factorio 2.1 compatibility of a submission made before
	// 2.1 ("compatible" or "incompatible"); empty when made in 2.1.
	Compat21 Compat21 `json:"compat_21" doc:"The Factorio 2.1 compatibility of a submission made before 2.1; empty for one made in 2.1."`

	// Kind, Label, Description, and Icons describe the root node only.
	Kind        blueprint.Kind `json:"kind" doc:"The root node's kind."`
	Label       string         `json:"label" doc:"The root node's label."`
	Description string         `json:"description" doc:"The root node's description."`
	Icons       string         `json:"icons" doc:"The root node's icons."`

	// LeafCount and PreviewCount let a client show render progress and decide
	// whether it is worth polling the entry again.
	LeafCount    int64 `json:"leaf_count" doc:"How many blueprints the entry has, so a client can show render progress."`
	PreviewCount int64 `json:"preview_count" doc:"How many of them have rendered."`
	// PreviewURL is the cover image (the first rendered leaf's thumbnail),
	// empty until at least one leaf rendered.
	PreviewURL string `json:"preview_url,omitempty" doc:"The cover image URL (the first rendered blueprint's thumbnail); empty until one has rendered."`
	// CoverURLs is a book's collage: the thumbnails of its first (up to four)
	// rendered leaves; set only for books with two or more.
	CoverURLs []string `json:"cover_urls,omitempty" doc:"A book's collage: the thumbnail URLs of its first rendered blueprints (at most four); only for a book with two or more."`
	// PreviewPending is set while there is no cover yet and a leaf's preview
	// is still rendering, so a card can show a spinner instead of a placeholder.
	PreviewPending bool  `json:"preview_pending,omitempty" doc:"True while there is no cover yet and a preview is still rendering."`
	FavoritesCount int64 `json:"favorites_count" doc:"How many users favorited it."`
	// IsFavorited is whether the viewer has favorited it; false for anonymous viewers.
	IsFavorited bool `json:"is_favorited" doc:"Whether the viewer favorited it; false when signed out."`
	// IsPinned is whether the viewer pinned it to their profile.
	IsPinned bool `json:"is_pinned" doc:"Whether the viewer pinned it to their profile."`
	// ViewCount is the all-time page views.
	ViewCount int64 `json:"view_count" doc:"The all-time page views."`
	// Records are the leaderboard boards its blueprints lead now.
	Records []BoardRecord `json:"records" doc:"The leaderboard boards the entry's blueprints lead now, in board order; each names its blueprint."`
}

// Revision is an immutable snapshot of one submission, hydrated into the
// recursive node tree it describes.
type Revision struct {
	ID            string `json:"id" doc:"The revision's id."`
	VersionNumber int32  `json:"version_number" doc:"The revision's number, counting from 1."`
	// GameVersion is the game version that exported the submission, read out of
	// the exchange string (e.g. "2.0.15"); empty when the string carries none.
	GameVersion string `json:"game_version" doc:"The Factorio version that exported it; empty when the string carries none."`
	// Compat21 is the submission's Factorio 2.1 compatibility (revisions.compat_21).
	Compat21       Compat21 `json:"compat_21" doc:"The submission's Factorio 2.1 compatibility."`
	OriginalString string   `json:"original_string" doc:"The exact exchange string that was submitted."`
	// Note is the uploader's change note, empty for none.
	Note      string    `json:"note" doc:"The uploader's note on this revision (at most 280 characters); empty for none."`
	CreatedAt time.Time `json:"created_at" doc:"When the revision was made."`
	Root      *Node     `json:"root,omitempty" doc:"The submission's tree."`
}

// Node is one hydrated tree node: a book with children, a renderable leaf
// blueprint with its own exchange string and preview state, or a planner with
// its exchange string and settings (never rendered).
type Node struct {
	Kind        blueprint.Kind `json:"kind" doc:"The node's kind."`
	ID          string         `json:"id" doc:"The node's id."`
	Label       string         `json:"label" doc:"The node's label."`
	Description string         `json:"description" doc:"The node's description."`
	Icons       string         `json:"icons" doc:"The node's icons."`

	ActiveIndex *int32 `json:"active_index,omitempty" doc:"A book's active blueprint index."`
	Children    []Node `json:"children,omitempty" doc:"A book's children, in order."`

	ExchangeString     string          `json:"exchange_string,omitempty" doc:"The node's exchange string."`
	EntityCount        int32           `json:"entity_count,omitempty" doc:"How many entities the node has."`
	Preview            *Preview        `json:"preview,omitempty" doc:"A blueprint's render; absent until stored."`
	PreviewStatus      progress.Status `json:"preview_status,omitempty" doc:"A blueprint's preview state."`
	PreviewFailureCode string          `json:"preview_failure_code,omitempty" doc:"Why a failed preview failed."`
	// Planner is a planner node's settings, by Kind.
	Planner *PlannerSettings `json:"planner,omitempty" doc:"The planner's settings, for an upgrade or deconstruction planner node."`
}

// Preview is the rendered image for one leaf.
type Preview struct {
	// URL is the full preview; ThumbnailURL a smaller one that fits
	// within previewimage.ThumbnailMax px. Both are MediaType.
	URL             string `json:"url" doc:"The full preview's URL."`
	ThumbnailURL    string `json:"thumbnail_url" doc:"The thumbnail's URL."`
	MediaType       string `json:"media_type" doc:"The image's media type."`
	Width           int32  `json:"width" doc:"The full preview's width in pixels."`
	Height          int32  `json:"height" doc:"The full preview's height in pixels."`
	ThumbnailWidth  int32  `json:"thumbnail_width" doc:"The thumbnail's width in pixels."`
	ThumbnailHeight int32  `json:"thumbnail_height" doc:"The thumbnail's height in pixels."`
	// FullAvailable is false once the storage budget evicted the full file:
	// URL then answers 404 until a re-render (POST .../preview/render)
	// stores it again. The thumbnail is never evicted.
	FullAvailable bool      `json:"full_available" doc:"False once the storage budget evicted the full file; the thumbnail stays."`
	CreatedAt     time.Time `json:"created_at" doc:"When the render was stored."`
}

// LeafDetail is the entry-scoped representation of a revision's leaf. Production
// data is deliberately kept out of recursive tree and listing projections.
type LeafDetail struct {
	// NotModified is set when the caller's If-None-Match matched the ETag;
	// only ETag is then filled.
	NotModified    bool   `json:"-"`
	ID             string `json:"id" doc:"The blueprint's id."`
	Label          string `json:"label" doc:"The blueprint's label."`
	Description    string `json:"description" doc:"The blueprint's description."`
	Icons          string `json:"icons" doc:"The blueprint's icons."`
	ExchangeString string `json:"exchange_string" doc:"The blueprint's exchange string."`
	EntityCount    int32  `json:"entity_count" doc:"How many entities the blueprint has."`
	// Compat21 is this leaf's Factorio 2.1 compatibility ("compatible" or
	// "incompatible" for one made before 2.1; empty otherwise).
	Compat21 Compat21 `json:"compat_21" doc:"This blueprint's Factorio 2.1 compatibility; empty for one made in 2.1."`
	// SnapToGrid is the blueprint's grid snapping; absent when it has none.
	SnapToGrid         *blueprint.SnapToGrid `json:"snap_to_grid,omitempty" doc:"The blueprint's grid snapping; absent when it has none."`
	Revision           LeafRevisionContext   `json:"revision" doc:"The revision this blueprint belongs to."`
	Preview            *Preview              `json:"preview,omitempty" doc:"The render; absent until one is stored."`
	PreviewStatus      progress.Status       `json:"preview_status" doc:"The preview's state."`
	PreviewFailureCode string                `json:"preview_failure_code,omitempty" doc:"Why a failed preview failed."`
	// Entities and Recipes are the structural breakdown computed at decode
	// time (independent of, and available before, production analysis).
	Entities []blueprint.EntityStat `json:"entities,omitempty" doc:"The blueprint's entities per name and quality."`
	Recipes  []blueprint.RecipeStat `json:"recipes,omitempty" doc:"The entities set to each recipe and quality."`
	// Mods is what the entities and recipes need beyond the base game;
	// absent for a leaf stored before detection ran.
	Mods       *mods.Detection       `json:"mods,omitempty" doc:"What the entities and recipes need beyond the base game; absent when not detected yet."`
	Parameters []blueprint.Parameter `json:"parameters,omitempty" doc:"The blueprint's display-only parameters."`
	// Schedules is the blueprint's train / space platform schedules, verbatim.
	Schedules  []blueprint.Schedule `json:"schedules,omitempty" doc:"The train or space platform schedules."`
	Production Production           `json:"production" doc:"The analysis of what it makes and uses."`
	ETag       string               `json:"-"`
}

type LeafRevisionContext struct {
	ID            string `json:"id" doc:"The revision's id."`
	VersionNumber int32  `json:"version_number" doc:"The revision's number."`
	GameVersion   string `json:"game_version" doc:"The revision's Factorio version."`
}

// Production exposes the lifecycle state for analysis. Snapshot fields are
// populated only when Availability is ready.
type Production struct {
	Availability progress.Status `json:"availability" doc:"The analysis's state: pending, ready or failed."`
	// FailureCode is why a failed analysis failed (its job's code).
	FailureCode   string         `json:"failure_code,omitempty" doc:"Why a failed analysis failed."`
	Stale         bool           `json:"stale" doc:"Whether the analysis is out of date and being redone."`
	SpacePlatform *SpacePlatform `json:"space_platform,omitempty" doc:"Platform speed, thrust and weight; present for a space platform."`

	SchemaVersion int32 `json:"schema_version,omitempty" doc:"The analysis format's version."`
	// State is "complete" or "partial", reflecting unresolved production
	// materials only. Named to avoid confusion with Factorio's quality game
	// mechanic.
	State         string                              `json:"state,omitempty" enum:"complete,partial" doc:"complete, or partial when some production could not be resolved."`
	Ingredients   []analysisresult.MaterialRate       `json:"ingredients,omitempty" doc:"The materials it needs from outside, per second."`
	Products      []analysisresult.MaterialRate       `json:"products,omitempty" doc:"The materials it makes for outside, per second."`
	Intermediates []analysisresult.MaterialRate       `json:"intermediates,omitempty" doc:"The materials made and used inside it, per second."`
	Electricity   *analysisresult.Electricity         `json:"electricity,omitempty" doc:"Power produced and used."`
	Pollution     *analysisresult.Pollution           `json:"pollution,omitempty" doc:"Pollution produced and absorbed."`
	Completeness  *analysisresult.SectionCompleteness `json:"completeness,omitempty" doc:"Which sections are complete."`
	Calculator    *analysisresult.Calculator          `json:"calculator,omitempty" doc:"What calculated the rates."`
	// Footprint is the tile-aligned size of what the blueprint places, and
	// BuildCost its items and their raw resources; absent in snapshots stored
	// before the analysis reported them.
	Footprint *analysisresult.Footprint `json:"footprint,omitempty" doc:"The tile-aligned size of what the blueprint places."`
	BuildCost *analysisresult.BuildCost `json:"build_cost,omitempty" doc:"What building it costs."`
}

// treeNode mirrors the shape-only jsonb stored in revisions.tree: typed
// pointers into the books/blueprints tables and nothing else.
type treeNode struct {
	Kind     string     `json:"kind"`
	ID       string     `json:"id"`
	Children []treeNode `json:"children,omitempty"`
}
