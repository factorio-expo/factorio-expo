package dataexport

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// One writer per member of the export. Each writes a JSON array, or records
// the read error on the stream.

func (s *Service) writeIdentities(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportIdentities(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]identity, 0, len(rows))
	for _, row := range rows {
		items = append(items, identity{
			Provider: row.Provider, ProviderUserID: row.ProviderUserID,
			LinkedAt: row.CreatedAt.Time, LastUsedAt: row.LastUsedAt.Time,
		})
	}
	list(out, items)
}

func (s *Service) writePasskeys(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportPasskeys(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]passkey, 0, len(rows))
	for _, row := range rows {
		items = append(items, passkey{ID: row.ID, AddedAt: row.CreatedAt.Time, LastUsedAt: optionalTime(row.LastUsedAt)})
	}
	list(out, items)
}

func (s *Service) writeSessions(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportSessions(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]session, 0, len(rows))
	for _, row := range rows {
		items = append(items, session{
			CreatedAt: row.CreatedAt.Time, LastUsedAt: row.LastUsedAt.Time,
			ExpiresAt: row.ExpiresAt.Time, LastSignInAt: row.AuthTime.Time,
		})
	}
	list(out, items)
}

// writeEntries pages through the entries; each carries its revisions, whose
// exchange strings are read and written one at a time.
func (s *Service) writeEntries(ctx context.Context, out *stream, userID string) {
	out.raw("[")
	params := db.ExportEntriesParams{
		UserID: userID, AfterCreatedAt: pgtype.Timestamptz{Valid: true}, RowLimit: pageSize,
	}
	first := true
	for out.err == nil {
		rows, err := s.queries.ExportEntries(ctx, params)
		if err != nil {
			out.fail(err)
			return
		}
		for _, row := range rows {
			if !first {
				out.raw(",")
			}
			first = false
			s.writeEntry(ctx, out, row)
		}
		if len(rows) < pageSize {
			break
		}
		last := rows[len(rows)-1]
		params.AfterCreatedAt, params.AfterID = last.CreatedAt, last.ID
	}
	out.raw("]")
}

// writeEntry writes one entry object: its fields, then "revisions".
func (s *Service) writeEntry(ctx context.Context, out *stream, row db.ExportEntriesRow) {
	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}
	head, err := json.Marshal(entry{
		ID: row.ID, Title: row.Title, Description: row.Description, Visibility: row.Visibility,
		QuickPreview: row.QuickPreview, Spoiler: row.Spoiler, Tags: tags, CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time, ExpiresAt: optionalTime(row.ExpiresAt), DeletedAt: optionalTime(row.DeletedAt),
		SourceURL: optionalText(row.SourceUrl), Alias: optionalText(row.Alias),
	})
	if err != nil {
		out.fail(err)
		return
	}
	// The object stays open ("}" dropped) for the revisions member.
	out.raw(string(head[:len(head)-1]))
	out.field("revisions", false)
	revisions, err := s.queries.ExportRevisions(ctx, row.ID)
	if err != nil {
		out.fail(err)
		return
	}
	out.raw("[")
	written := 0
	for _, rev := range revisions {
		exchange, err := s.queries.ExportRevisionString(ctx, rev.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // deleted since the list was read
		}
		if err != nil {
			out.fail(err)
			return
		}
		if written > 0 {
			out.raw(",")
		}
		written++
		out.value(revision{
			ID: rev.ID, Version: rev.VersionNumber, GameVersion: rev.GameVersion, Note: rev.Note,
			CreatedAt: rev.CreatedAt.Time, ExchangeString: exchange,
		})
	}
	out.raw("]}")
}

func (s *Service) writeComments(ctx context.Context, out *stream, userID string) {
	out.raw("[")
	params := db.ExportCommentsParams{
		UserID: userID, AfterCreatedAt: pgtype.Timestamptz{Valid: true}, RowLimit: pageSize,
	}
	first := true
	for out.err == nil {
		rows, err := s.queries.ExportComments(ctx, params)
		if err != nil {
			out.fail(err)
			return
		}
		for _, row := range rows {
			if !first {
				out.raw(",")
			}
			first = false
			out.value(comment{
				EntryID: row.EntryID, Number: row.Number, RevisionNumber: row.RevisionNumber, Body: row.Body,
				CreatedAt: row.CreatedAt.Time, EditedAt: optionalTime(row.EditedAt),
				RemovedAt: optionalTime(row.DeletedAt), RemovedBy: row.DeletedBy.String,
			})
		}
		if len(rows) < pageSize {
			break
		}
		last := rows[len(rows)-1]
		params.AfterCreatedAt, params.AfterID = last.CreatedAt, last.ID
	}
	out.raw("]")
}

func (s *Service) writeFavorites(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportFavorites(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]favorite, 0, len(rows))
	for _, row := range rows {
		items = append(items, favorite{EntryID: row.EntryID, CreatedAt: row.CreatedAt.Time})
	}
	list(out, items)
}

func (s *Service) writePinnedEntries(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportPinnedEntries(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]pinnedEntry, 0, len(rows))
	for _, row := range rows {
		items = append(items, pinnedEntry{EntryID: row.EntryID, Position: row.Position, PinnedAt: row.PinnedAt.Time})
	}
	list(out, items)
}

func (s *Service) writeCollections(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportCollections(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	out.raw("[")
	for i, row := range rows {
		members, err := s.queries.ExportCollectionEntries(ctx, row.ID)
		if err != nil {
			out.fail(err)
			return
		}
		item := collection{
			ID: row.ID, Name: row.Name, Description: row.Description, Visibility: row.Visibility,
			CreatedAt: row.CreatedAt.Time, Entries: make([]collectionEntry, 0, len(members)),
		}
		for _, member := range members {
			item.Entries = append(item.Entries, collectionEntry{
				EntryID: member.EntryID, Position: member.Position, AddedAt: member.AddedAt.Time,
			})
		}
		if i > 0 {
			out.raw(",")
		}
		out.value(item)
	}
	out.raw("]")
}

func (s *Service) writeEntryReports(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportEntryReports(ctx, pgtype.Text{String: userID, Valid: true})
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]entryReport, 0, len(rows))
	for _, row := range rows {
		items = append(items, entryReport{
			ID: row.ID, EntryID: row.EntryID, Reason: row.Reason, Note: row.Note,
			RevisionNumber: row.RevisionNumber, CreatedAt: row.CreatedAt.Time, ResolvedAt: optionalTime(row.ResolvedAt),
		})
	}
	list(out, items)
}

func (s *Service) writeCommentReports(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportCommentReports(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]commentReport, 0, len(rows))
	for _, row := range rows {
		items = append(items, commentReport{
			EntryID: row.EntryID, CommentNumber: row.CommentNumber, Reason: row.Reason, Note: row.Note,
			CreatedAt: row.CreatedAt.Time, ResolvedAt: optionalTime(row.ResolvedAt),
		})
	}
	list(out, items)
}

func (s *Service) writeXPEvents(ctx context.Context, out *stream, userID string) {
	rows, err := s.queries.ExportXPEvents(ctx, userID)
	if err != nil {
		out.fail(err)
		return
	}
	items := make([]xpEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, xpEvent{
			Reason: row.Reason, Points: row.Points, EntryID: optionalText(row.EntryID),
			CommentID: optionalText(row.CommentID), ChallengeID: optionalText(row.ChallengeID),
			Board: optionalText(row.Board), CreatedAt: row.CreatedAt.Time,
		})
	}
	list(out, items)
}
