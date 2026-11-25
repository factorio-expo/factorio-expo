// Package search owns syncing the catalog into the Typesense search
// projection and serving catalog search queries against it. It is never a
// source of truth: every document it writes is rebuildable from entries,
// revisions, books, blueprints, blueprint_stats, tags, and preview metadata
// (see docs/search.md).
//
// Two seams: Client answers queries (httpapi depends on it), and engine
// writes documents (the Syncer depends on it). The projection between the
// catalog rows and the documents is a pure function (projectEntry), so
// neither the dispatcher nor the engine adapter has to know how a document
// is built.
package search

import (
	"context"
	"fmt"
)

// Client serves catalog search queries against the entry-level projection.
type Client interface {
	// Search answers GET /api/v1/search.
	Search(ctx context.Context, query Query) (Result, error)
}

// Indexer syncs one entry's documents into the search engine, or removes
// them. Implementations must be idempotent: the dispatcher may call them more
// than once for the same entry (e.g. after a retry), and a full reindex may
// index an entry that is already current.
type Indexer interface {
	// IndexEntry (re)builds and upserts the documents for one entry, or
	// removes them when the entry is no longer publishable (deleted, private
	// or without a current revision).
	IndexEntry(ctx context.Context, entryID string) error
	// DeleteEntry removes an entry and its joined documents from the index.
	DeleteEntry(ctx context.Context, entryID string) error
}

// engine is the write side of a search backend: store or remove finished
// documents. TypesenseClient is the production adapter.
type engine interface {
	writeProjection(ctx context.Context, proj projection) error
	removeEntry(ctx context.Context, entryID string) error
}

// Syncer loads a listed entry from the catalog read model, projects it and
// writes the result to an engine. It is the Indexer the dispatcher and the
// index rebuild use.
type Syncer struct {
	source  entrySource
	queries syncQueries
	engine  engine
}

// syncQueries adds the rebuild-time queries to rowQueries.
type syncQueries interface {
	rowQueries
	ListSearchableEntryIDs(ctx context.Context) ([]string, error)
	EnqueueSearchIndexJob(ctx context.Context, entryID string) error
}

// NewSyncer builds the Indexer for a Typesense-backed index; source is the
// catalog (*catalog.Service).
func NewSyncer(source entrySource, queries syncQueries, index *TypesenseClient) *Syncer {
	return &Syncer{source: source, queries: queries, engine: index}
}

func (s *Syncer) IndexEntry(ctx context.Context, entryID string) error {
	id, err := parseID(entryID)
	if err != nil {
		return fmt.Errorf("parse entry id: %w", err)
	}
	proj, err := buildProjection(ctx, s.source, s.queries, id)
	if err != nil {
		return fmt.Errorf("build projection: %w", err)
	}
	if !proj.Indexable {
		return s.engine.removeEntry(ctx, entryID)
	}
	return s.engine.writeProjection(ctx, proj)
}

func (s *Syncer) DeleteEntry(ctx context.Context, entryID string) error {
	return s.engine.removeEntry(ctx, entryID)
}

// IndexAll projects every currently searchable catalog entry and returns how
// many were indexed. Entries that fail are queued as index jobs for retry.
func (s *Syncer) IndexAll(ctx context.Context) (int, error) {
	entryIDs, err := s.queries.ListSearchableEntryIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list searchable entries: %w", err)
	}
	return indexEntries(ctx, entryIDs, s.IndexEntry, func(id string) error {
		return s.queries.EnqueueSearchIndexJob(ctx, id)
	})
}
