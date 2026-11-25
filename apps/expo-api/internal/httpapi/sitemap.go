package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Sitemap pages are large: the web app's sitemap reads every listed entry
// through them, and each row is only an id and a date.
const (
	defaultSitemapPage = 5000
	maxSitemapPage     = 5000
)

type sitemapEntry struct {
	ID        string    `json:"id" doc:"The entry's public id."`
	UpdatedAt time.Time `json:"updated_at" doc:"When the entry last changed."`
}

type sitemapPage struct {
	Entries []sitemapEntry `json:"entries" doc:"The listed entries of this page, in id order."`
	// The `after` of the next page; absent on the last one.
	Next string `json:"next,omitempty" doc:"The after of the next page; absent on the last page."`
}

type sitemapInput struct {
	Limit int32  `query:"limit" default:"5000" doc:"How many entries to return, 1 to 5000. Any other value means 5000."`
	After string `query:"after" doc:"The next page's after: the id the previous page ended with."`
}

type sitemapOutput struct {
	Body sitemapPage
}

// listSitemapEntries pages through listed (public, searchable) entries by id,
// straight from PostgreSQL rather than search: the sitemap needs every entry,
// and deep search offsets are slow and rate limited for search traffic.
func (s *server) listSitemapEntries(ctx context.Context, in *sitemapInput) (*sitemapOutput, error) {
	limit := in.Limit
	if limit < 1 || limit > maxSitemapPage {
		limit = defaultSitemapPage
	}
	rows, err := s.queries.ListSitemapEntries(ctx, db.ListSitemapEntriesParams{
		AfterID:   in.After,
		PageLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list sitemap entries: %w", err)
	}
	page := sitemapPage{Entries: make([]sitemapEntry, len(rows))}
	for i, row := range rows {
		page.Entries[i] = sitemapEntry{ID: row.ID, UpdatedAt: row.UpdatedAt.Time.UTC()}
	}
	if len(rows) == int(limit) {
		page.Next = rows[len(rows)-1].ID
	}
	return &sitemapOutput{Body: page}, nil
}

func (s *server) registerSitemap(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listSitemapEntries", Method: http.MethodGet, Path: "/sitemap/entries",
		Tags:    []string{"Sitemap"},
		Summary: "Listed entries by id, for the sitemap",
		Description: "Public, searchable entries in id order, read from PostgreSQL rather than search. " +
			"Follow next until it is absent.",
	}, accessPublic, limitSitemap), s.listSitemapEntries)
}
