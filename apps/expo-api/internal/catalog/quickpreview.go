package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// CreateQuickPreview stores one blueprint as an ephemeral entry: rendered and
// analysed like any upload, but never listed or indexed, read-only, and gone
// after the preview TTL unless published (publish.go). Books are rejected so a preview
// stays one short job. A signed-in caller owns it; an anonymous one gets the
// edit token, as for any anonymous entry.
func (s *Service) CreateQuickPreview(ctx context.Context, exchange, ownerUserID string, ipHash []byte) (Entry, error) {
	if err := s.allowUpload(ctx, ownerUserID, ipHash); err != nil {
		return Entry{}, err
	}
	root, err := blueprint.Parse(exchange, s.limits)
	if err != nil {
		return Entry{}, err
	}
	if root.Kind != blueprint.KindBlueprint {
		return Entry{}, ErrPreviewOfBook
	}
	title, description, err := entryMetadata(Submission{}, root)
	if err != nil {
		return Entry{}, err
	}
	ownerID, err := submissionOwner(ownerUserID)
	if err != nil {
		return Entry{}, err
	}
	return s.insertEntry(ctx, newEntry{
		exchange: exchange, root: root, owner: ownerID, visibility: "public",
		title: title, description: description, tags: []string{},
		expiresAt:    pgtype.Timestamptz{Time: time.Now().Add(s.previewTTL), Valid: true},
		quickPreview: true,
		ticket:       s.queue.QuickPreview(renderqueue.Requester{UserID: ownerID, IPHash: ipHash}),
	})
}

// RejectQuickPreview hard-deletes a quick preview whose preview or analysis
// failed for good: a quick preview is the whole package or nothing. Any
// other entry is left alone.
func (s *Service) RejectQuickPreview(ctx context.Context, entryID string) error {
	return s.inChange(ctx, func(c *change) error {
		deleted, err := c.queries.RejectQuickPreview(ctx, entryID)
		if err != nil {
			return fmt.Errorf("reject failed quick preview: %w", err)
		}
		if deleted > 0 {
			slog.Info("rejected a quick preview whose job failed", "entry_id", entryID)
			c.touch(entryID)
		}
		return nil
	})
}
