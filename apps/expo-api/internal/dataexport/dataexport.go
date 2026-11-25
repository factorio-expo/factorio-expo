// Package dataexport writes everything the catalog holds about one account as
// a single JSON document (GET /api/v1/me/export, docs/api.md#account-data-export):
// the account with its profile status and links, its sign-in methods and
// sessions (ids and dates, never keys), its entries with every revision's
// original exchange string, comments, favorites, pins, collections and the
// reports it filed. The document is streamed: long lists are read a page at
// a time and exchange strings one revision at a time.
package dataexport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// ErrNotFound: no live account with that id.
var ErrNotFound = errors.New("account not found")

// pageSize is how many entries or comments one read returns.
const pageSize = 200

// Service reads the export from the catalog database.
type Service struct {
	queries *db.Queries
}

// New builds the service on the catalog queries.
func New(queries *db.Queries) *Service {
	return &Service{queries: queries}
}

// Find reads the live account to export (ErrNotFound for a deleted or
// unknown one), before anything is written.
func (s *Service) Find(ctx context.Context, userID string) (Account, error) {
	row, err := s.queries.ExportAccount(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("read account: %w", err)
	}
	links, err := s.queries.ExportSocialLinks(ctx, userID)
	if err != nil {
		return Account{}, fmt.Errorf("read social links: %w", err)
	}
	account := Account{
		ID: row.ID, Username: row.Username, CreatedAt: row.CreatedAt.Time, Status: row.Status,
		SocialLinks: make([]socialLink, 0, len(links)),
	}
	for _, link := range links {
		account.SocialLinks = append(account.SocialLinks, socialLink{Platform: link.Platform, Handle: link.Handle})
	}
	return account, nil
}

// Write streams the account's export to w. An error part way leaves the
// document unfinished, so the caller must not present it as complete.
func (s *Service) Write(ctx context.Context, w io.Writer, account Account, now time.Time) error {
	out := newStream(w)
	out.raw("{")
	out.field("format", true)
	out.value(Format)
	out.field("version", false)
	out.value(Version)
	out.field("exported_at", false)
	out.value(now.UTC())
	out.field("account", false)
	out.value(account)
	for _, section := range []struct {
		name  string
		write func(context.Context, *stream, string)
	}{
		{"identities", s.writeIdentities},
		{"passkeys", s.writePasskeys},
		{"sessions", s.writeSessions},
		{"entries", s.writeEntries},
		{"comments", s.writeComments},
		{"favorites", s.writeFavorites},
		{"pinned_entries", s.writePinnedEntries},
		{"collections", s.writeCollections},
		{"entry_reports", s.writeEntryReports},
		{"comment_reports", s.writeCommentReports},
		{"xp_events", s.writeXPEvents},
	} {
		out.field(section.name, false)
		section.write(ctx, out, account.ID)
		if out.err != nil {
			return fmt.Errorf("export %s: %w", section.name, out.err)
		}
	}
	out.raw("}\n")
	return out.flush()
}
