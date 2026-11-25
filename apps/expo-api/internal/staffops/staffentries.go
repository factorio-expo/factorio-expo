package staffops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Staff console -> Entries (docs/admin.md#entries): every entry whatever
// its state, one entry's page, and what staff do to it besides a delete
// (moderation.go): restore, search sync, edge cache purge.

// staffEntryPageSize is how many entries one Entries page lists.
const staffEntryPageSize = 50

// EntryStates are the Entries page's state tabs.
var EntryStates = []string{"all", "live", "deleted", "quick_preview"}

// ErrEdgePurgeOff is a cache purge asked of an API without CDN
// credentials.
var ErrEdgePurgeOff = audit.ErrInvalidRequest.Withf("the Cloudflare purge is not configured")

// StaffEntry is an entry as the Entries list shows it.
type StaffEntry struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Visibility   string     `json:"visibility"`
	QuickPreview bool       `json:"quick_preview"`
	Spoiler      bool       `json:"spoiler"`
	CreatedAt    time.Time  `json:"created_at"`
	DeletedAt    *time.Time `json:"deleted_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	// Gone is deleted or expired: visitors get a 404.
	Gone bool `json:"gone"`
	// Expired: the expiry sweep deleted it, not a person.
	Expired bool `json:"expired"`
	// Owner is the owner's username, empty for an anonymous entry.
	Owner        string `json:"owner"`
	OwnerDeleted bool   `json:"owner_deleted"`
	// Leaves and FailedLeaves count the current revision's blueprints and
	// those with a preview or analysis failed for good.
	Leaves       int64 `json:"leaves"`
	FailedLeaves int64 `json:"failed_leaves"`
	OpenReports  int64 `json:"open_reports"`
}

// StaffEntryCounts are how many entries each state tab shows.
type StaffEntryCounts struct {
	All           int64 `json:"all"`
	Live          int64 `json:"live"`
	Deleted       int64 `json:"deleted"`
	QuickPreviews int64 `json:"quick_preview"`
}

// StaffEntryPage is one page of the Entries list.
type StaffEntryPage struct {
	Entries []StaffEntry     `json:"entries"`
	Counts  StaffEntryCounts `json:"counts"`
	// NextCursor fetches the next (older) page; empty on the last one.
	NextCursor string `json:"next_cursor"`
}

// StaffEntryQuery filters the Entries list; every field is optional.
type StaffEntryQuery struct {
	// Search matches the id, part of the title or the owner's username.
	Search string
	// State is one of EntryStates.
	State string
	// Visibility is public, unlisted or private.
	Visibility string
	// Author is anonymous or account.
	Author string
	// Failed keeps entries with a leaf failed for good.
	Failed bool
	// Mods is vanilla, space_age (built-in mods only) or modded (another
	// mod, or unknown names).
	Mods string
	// CreatedFrom and CreatedTo are UTC dates (YYYY-MM-DD), both inclusive.
	CreatedFrom string
	CreatedTo   string
	// MinLeaves keeps entries with at least this many blueprints.
	MinLeaves int
	Cursor    string
}

// ListStaffEntries lists entries newest first, whatever their state.
func (s *Service) ListStaffEntries(ctx context.Context, by audit.Staff, query StaffEntryQuery) (StaffEntryPage, error) {
	params, err := staffEntryParams(query)
	if err != nil {
		return StaffEntryPage{}, err
	}
	rows, err := s.queries.ListStaffEntries(ctx, params)
	if err != nil {
		return StaffEntryPage{}, fmt.Errorf("list entries: %w", err)
	}
	counts, err := s.queries.StaffEntryCounts(ctx)
	if err != nil {
		return StaffEntryPage{}, fmt.Errorf("count entries: %w", err)
	}
	page := StaffEntryPage{
		Entries: make([]StaffEntry, 0, min(len(rows), staffEntryPageSize)),
		Counts: StaffEntryCounts{
			All: counts.Total, Live: counts.Live, Deleted: counts.Deleted, QuickPreviews: counts.QuickPreviews,
		},
	}
	for i, row := range rows {
		if i == staffEntryPageSize {
			last := page.Entries[len(page.Entries)-1]
			page.NextCursor = FormatTimeCursor(last.CreatedAt, last.ID)
			break
		}
		page.Entries = append(page.Entries, StaffEntry{
			ID: row.ID, Title: row.Title, Visibility: row.Visibility, QuickPreview: row.QuickPreview,
			Spoiler: row.Spoiler, CreatedAt: row.CreatedAt.Time, DeletedAt: optionalTime(row.DeletedAt),
			ExpiresAt: optionalTime(row.ExpiresAt), Gone: row.Gone, Expired: expiredBySweep(row.DeletedAt, row.ExpiresAt),
			Owner: row.OwnerUsername, OwnerDeleted: row.OwnerDeleted,
			Leaves: row.Leaves, FailedLeaves: row.FailedLeaves, OpenReports: row.OpenReports,
		})
	}
	return page, nil
}

func staffEntryParams(query StaffEntryQuery) (db.ListStaffEntriesParams, error) {
	state := query.State
	if state == "" {
		state = "all"
	}
	if !slices.Contains(EntryStates, state) {
		return db.ListStaffEntriesParams{}, invalidStaffRequest("state must be all, live, deleted or quick_preview")
	}
	if !slices.Contains([]string{"", "public", "unlisted", "private"}, query.Visibility) {
		return db.ListStaffEntriesParams{}, invalidStaffRequest("visibility must be public, unlisted or private")
	}
	if !slices.Contains([]string{"", "anonymous", "account"}, query.Author) {
		return db.ListStaffEntriesParams{}, invalidStaffRequest("author must be anonymous or account")
	}
	if !slices.Contains([]string{"", "vanilla", "space_age", "modded"}, query.Mods) {
		return db.ListStaffEntriesParams{}, invalidStaffRequest("mods must be vanilla, space_age or modded")
	}
	if query.MinLeaves < 0 {
		return db.ListStaffEntriesParams{}, invalidStaffRequest("min_leaves must not be negative")
	}
	params := db.ListStaffEntriesParams{
		Query: strings.TrimSpace(query.Search), State: state, Visibility: query.Visibility,
		Author: query.Author, Failed: query.Failed, Mods: query.Mods, MinLeaves: int32(min(query.MinLeaves, 1<<20)),
		PageSize: staffEntryPageSize + 1,
	}
	var err error
	if params.CreatedFrom, err = staffDate(query.CreatedFrom, 0); err != nil {
		return db.ListStaffEntriesParams{}, err
	}
	// The end date is inclusive: entries before the next day.
	if params.CreatedTo, err = staffDate(query.CreatedTo, 1); err != nil {
		return db.ListStaffEntriesParams{}, err
	}
	if query.Cursor != "" {
		at, id, ok := ParseTimeCursor(query.Cursor)
		if !ok {
			return db.ListStaffEntriesParams{}, invalidStaffRequest("invalid cursor")
		}
		params.BeforeCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.BeforeID = id
	}
	return params, nil
}

// staffDate reads a YYYY-MM-DD date as UTC midnight, plusDays later.
func staffDate(value string, plusDays int) (pgtype.Timestamptz, error) {
	if value == "" {
		return pgtype.Timestamptz{}, nil
	}
	day, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return pgtype.Timestamptz{}, invalidStaffRequest("dates are YYYY-MM-DD")
	}
	return pgtype.Timestamptz{Time: day.AddDate(0, 0, plusDays), Valid: true}, nil
}

// expiredBySweep: a deleted entry whose expiry had passed when it was
// deleted went with the expiry sweep, not by a person.
func expiredBySweep(deletedAt, expiresAt pgtype.Timestamptz) bool {
	return deletedAt.Valid && expiresAt.Valid && !expiresAt.Time.After(deletedAt.Time)
}

// StaffEntryDetail is one entry's console page.
type StaffEntryDetail struct {
	StaffEntry
	Description     string    `json:"description"`
	UpdatedAt       time.Time `json:"updated_at"`
	OwnerID         string    `json:"owner_id"`
	Listed          bool      `json:"listed"`
	CurrentRevision int32     `json:"current_revision"`
	Views           int64     `json:"views"`
	Favorites       int64     `json:"favorites"`
	Tags            []string  `json:"tags"`
	// Search is the entry's pending or failed search sync; empty when the
	// index is up to date.
	Search StaffSearchJob `json:"search"`
	// RestoreBlocker says why a deleted entry cannot be restored; empty
	// when it can (or it is not deleted).
	RestoreBlocker string          `json:"restore_blocker"`
	Revisions      []StaffRevision `json:"revisions"`
	Blueprints     []StaffLeaf     `json:"blueprints"`
	Reports        []StaffReport   `json:"reports"`
	Comments       []StaffComment  `json:"comments"`
}

// StaffSearchJob is an entry's queued search sync.
type StaffSearchJob struct {
	Action      string `json:"action"`
	State       string `json:"state"`
	FailureCode string `json:"failure_code"`
}

// StaffRevision is one revision of an entry.
type StaffRevision struct {
	ID          string    `json:"id"`
	Number      int32     `json:"number"`
	GameVersion string    `json:"game_version"`
	CreatedAt   time.Time `json:"created_at"`
	// StringBytes is the original exchange string's length.
	StringBytes int64 `json:"string_bytes"`
	Leaves      int64 `json:"leaves"`
	Current     bool  `json:"current"`
}

// StaffLeaf is a blueprint of the current revision with its job states.
type StaffLeaf struct {
	ID                    string `json:"id"`
	Label                 string `json:"label"`
	Position              int32  `json:"position"`
	EntityCount           int32  `json:"entity_count"`
	HasPreview            bool   `json:"has_preview"`
	PreviewJobState       string `json:"preview_job_state"`
	PreviewFailureCode    string `json:"preview_failure_code"`
	PreviewFailureDetail  string `json:"preview_failure_detail"`
	HasAnalysis           bool   `json:"has_analysis"`
	AnalysisJobState      string `json:"analysis_job_state"`
	AnalysisFailureCode   string `json:"analysis_failure_code"`
	AnalysisFailureDetail string `json:"analysis_failure_detail"`
}

// StaffReport is one report of an entry, open or resolved.
type StaffReport struct {
	ID             string     `json:"id"`
	Reason         string     `json:"reason"`
	Note           string     `json:"note"`
	RevisionNumber int32      `json:"revision_number"`
	CreatedAt      time.Time  `json:"created_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	ResolvedBy     string     `json:"resolved_by"`
	Reporter       Reporter   `json:"reporter"`
}

// StaffComment is one comment of an entry, or its tombstone.
type StaffComment struct {
	ID          string     `json:"id"`
	Number      int32      `json:"number"`
	Author      string     `json:"author"`
	Body        string     `json:"body"`
	CreatedAt   time.Time  `json:"created_at"`
	EditedAt    *time.Time `json:"edited_at"`
	RemovedBy   string     `json:"removed_by"`
	OpenReports int64      `json:"open_reports"`
}

// GetStaffEntry reads one entry, whatever its state, with its revisions,
// the current revision's leaves, reports and latest comments.
func (s *Service) GetStaffEntry(ctx context.Context, by audit.Staff, entryID string) (StaffEntryDetail, error) {
	entryID, err := parseID(entryID)
	if err != nil {
		return StaffEntryDetail{}, err
	}
	row, err := s.queries.GetStaffEntry(ctx, entryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffEntryDetail{}, catalog.ErrNotFound
	}
	if err != nil {
		return StaffEntryDetail{}, fmt.Errorf("read entry: %w", err)
	}
	detail := StaffEntryDetail{
		ID: row.ID, Title: row.Title, Visibility: row.Visibility, QuickPreview: row.QuickPreview,
		Spoiler: row.Spoiler, CreatedAt: row.CreatedAt.Time, DeletedAt: optionalTime(row.DeletedAt),
		ExpiresAt: optionalTime(row.ExpiresAt), Gone: row.Gone, Expired: expiredBySweep(row.DeletedAt, row.ExpiresAt),
		Owner: row.OwnerUsername, OwnerDeleted: row.OwnerDeleted,
		Description: row.Description, UpdatedAt: row.UpdatedAt.Time, OwnerID: row.OwnerUserID.String,
		Listed: row.Listed, CurrentRevision: row.CurrentRevisionNumber, Views: row.Views, Favorites: row.Favorites,
		Tags: row.Tags,
		Search: StaffSearchJob{
			Action: row.SearchAction, State: row.SearchState, FailureCode: row.SearchFailureCode,
		},
		RestoreBlocker: restoreBlocker(row.DeletedAt.Valid, row.QuickPreview, row.OwnerDeleted),
	}
	if err := s.staffEntryParts(ctx, &detail, row.CurrentRevisionID.String); err != nil {
		return StaffEntryDetail{}, err
	}
	return detail, nil
}

// staffEntryParts reads an entry page's lists.
func (s *Service) staffEntryParts(ctx context.Context, detail *StaffEntryDetail, currentRevisionID string) error {
	revisions, err := s.queries.ListStaffEntryRevisions(ctx, detail.ID)
	if err != nil {
		return fmt.Errorf("list revisions: %w", err)
	}
	detail.Revisions = make([]StaffRevision, 0, len(revisions))
	for _, revision := range revisions {
		detail.Revisions = append(detail.Revisions, StaffRevision{
			ID: revision.ID, Number: revision.VersionNumber, GameVersion: revision.GameVersion,
			CreatedAt: revision.CreatedAt.Time, StringBytes: revision.StringBytes, Leaves: revision.Leaves,
			Current: revision.ID == currentRevisionID,
		})
	}
	detail.Blueprints = []StaffLeaf{}
	if currentRevisionID != "" {
		leaves, err := s.queries.ListStaffRevisionLeaves(ctx, currentRevisionID)
		if err != nil {
			return fmt.Errorf("list leaves: %w", err)
		}
		for _, leaf := range leaves {
			detail.Blueprints = append(detail.Blueprints, StaffLeaf{
				ID: leaf.ID, Label: leaf.Label, Position: leaf.Position, EntityCount: leaf.EntityCount,
				HasPreview: leaf.HasPreview, PreviewJobState: leaf.PreviewJobState,
				PreviewFailureCode: leaf.PreviewFailureCode, PreviewFailureDetail: leaf.PreviewFailureDetail,
				HasAnalysis: leaf.HasProduction, AnalysisJobState: leaf.AnalysisJobState,
				AnalysisFailureCode: leaf.AnalysisFailureCode, AnalysisFailureDetail: leaf.AnalysisFailureDetail,
			})
		}
	}
	detail.Leaves = int64(len(detail.Blueprints))
	for _, leaf := range detail.Blueprints {
		if leaf.PreviewJobState == "failed" || leaf.AnalysisJobState == "failed" {
			detail.FailedLeaves++
		}
	}

	reports, err := s.queries.ListStaffEntryReports(ctx, detail.ID)
	if err != nil {
		return fmt.Errorf("list reports: %w", err)
	}
	detail.Reports = make([]StaffReport, 0, len(reports))
	for _, report := range reports {
		if !report.ResolvedAt.Valid {
			detail.OpenReports++
		}
		detail.Reports = append(detail.Reports, StaffReport{
			ID: report.ID, Reason: report.Reason, Note: report.Note, RevisionNumber: report.RevisionNumber,
			CreatedAt: report.CreatedAt.Time, ResolvedAt: optionalTime(report.ResolvedAt),
			ResolvedBy: report.ResolvedBy.String,
			Reporter:   Reporter{Anonymous: report.ReporterUsername == "", Username: report.ReporterUsername},
		})
	}

	comments, err := s.queries.ListStaffEntryComments(ctx, detail.ID)
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}
	detail.Comments = make([]StaffComment, 0, len(comments))
	for _, comment := range comments {
		detail.Comments = append(detail.Comments, StaffComment{
			ID: comment.ID, Number: comment.Number, Author: comment.AuthorUsername, Body: comment.Body,
			CreatedAt: comment.CreatedAt.Time, EditedAt: optionalTime(comment.EditedAt),
			RemovedBy: comment.DeletedBy.String, OpenReports: comment.OpenReports,
		})
	}
	return nil
}

// restoreBlocker says why a deleted entry cannot be restored, or "".
func restoreBlocker(deleted, quickPreview, ownerDeleted bool) string {
	switch {
	case !deleted:
		return ""
	case quickPreview:
		return "a quick preview cannot be restored"
	case ownerDeleted:
		return "its owner's account is deleted"
	default:
		return ""
	}
}

// StaffRevisionString is a revision's original exchange string, whatever
// the entry's state.
func (s *Service) StaffRevisionString(ctx context.Context, by audit.Staff, entryID, revisionID string) (string, error) {
	entryID, err := parseID(entryID)
	if err != nil {
		return "", err
	}
	if revisionID, err = parseID(revisionID); err != nil {
		return "", err
	}
	raw, err := s.queries.GetStaffRevisionString(ctx, db.GetStaffRevisionStringParams{
		RevisionID: revisionID, EntryID: entryID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", catalog.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read revision string: %w", err)
	}
	return raw, nil
}

// StaffRestoreEntry undoes a soft delete (by its owner, staff or the expiry
// sweep) until the hard-delete cleanup has run. A passed expiry is cleared.
// Quick previews and entries of deleted accounts stay deleted.
func (s *Service) StaffRestoreEntry(ctx context.Context, by audit.Staff, entryID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if entryID, err = parseID(entryID); err != nil {
		return err
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		entry, err := c.LockEntry(ctx, entryID)
		if err != nil {
			return err
		}
		if !entry.DeletedAt.Valid {
			return invalidStaffRequest("the entry is not deleted")
		}
		ownerDeleted := false
		if entry.OwnerUserID.Valid {
			owner, err := c.Queries().GetUserForStaffUpdate(ctx, entry.OwnerUserID.String)
			if err != nil {
				return fmt.Errorf("lock owner: %w", err)
			}
			ownerDeleted = owner.DeletedAt.Valid
		}
		if blocker := restoreBlocker(true, entry.QuickPreview, ownerDeleted); blocker != "" {
			return invalidStaffRequest("%s", blocker)
		}
		restored, err := c.RestoreEntry(ctx, entry.ID)
		if err != nil {
			return err
		}
		return audit.Write(ctx, c.Queries(), by, audit.Record{
			Action: "entry.restore", TargetType: "entry", TargetID: entry.ID, Reason: reason,
			Before: map[string]any{"deleted_at": entry.DeletedAt.Time, "expires_at": optionalTime(entry.ExpiresAt)},
			After:  map[string]any{"deleted": false, "expires_at": optionalTime(restored.ExpiresAt)},
		})
	})
}

// StaffReindexEntry queues a search sync of the entry: indexed when it is
// listed, removed from the index otherwise.
func (s *Service) StaffReindexEntry(ctx context.Context, by audit.Staff, entryID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if entryID, err = parseID(entryID); err != nil {
		return err
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		entry, err := c.LockEntry(ctx, entryID)
		if err != nil {
			return err
		}
		if err := c.Resync(ctx, entry.ID); err != nil {
			return err
		}
		return audit.Write(ctx, c.Queries(), by, audit.Record{
			Action: "entry.reindex", TargetType: "entry", TargetID: entry.ID, Reason: reason,
		})
	})
}

// StaffPurgeEntryCache purges the entry's preview images and raw strings
// from the CDN cache (every revision), whatever its state.
// ErrEdgePurgeOff without CDN credentials.
func (s *Service) StaffPurgeEntryCache(ctx context.Context, by audit.Staff, entryID, reason string) error {
	if !s.catalog.EdgePurgeEnabled() {
		return ErrEdgePurgeOff
	}
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if entryID, err = parseID(entryID); err != nil {
		return err
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		entry, err := c.LockEntry(ctx, entryID)
		if err != nil {
			return err
		}
		if err := c.Hide(ctx, entry.ID); err != nil {
			return err
		}
		return audit.Write(ctx, c.Queries(), by, audit.Record{
			Action: "entry.purge_cache", TargetType: "entry", TargetID: entry.ID, Reason: reason,
		})
	})
}
