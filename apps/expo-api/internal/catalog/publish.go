package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// EntryPublish is what publishing sets: the entry's own metadata, as on an
// upload.
type EntryPublish struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Tags replaces the tag set; nil keeps the entry's tags (a form whose tag
	// list failed to load sends none).
	Tags       *[]string `json:"tags,omitempty"`
	Visibility string    `json:"visibility"`
	// ExpiresInDays is the lifetime from now (ExpiryDays, 0 never). Absent, a
	// quick preview never expires and an anonymous entry keeps its expiry.
	ExpiresInDays *int `json:"expires_in_days,omitempty"`
	// Spoiler marks the entry spoiler/NSFW; absent keeps its flag.
	Spoiler *bool `json:"spoiler,omitempty"`
}

// PublishEntry gives an entry to the signed-in caller and sets its metadata.
// It takes a quick preview (which becomes a lasting entry, reusing its render
// and analysis; its preview and analysis must both be ready) or an anonymous
// entry (claimed as it is, whatever its leaves' state). The caller must own a
// preview or present the edit token of an anonymous entry; an owned entry
// that is not a quick preview has nothing to publish. An upload ban covers it.
func (s *Service) PublishEntry(
	ctx context.Context, entryID string, auth Authorization, ipHash []byte, request EntryPublish,
) (Entry, error) {
	if err := s.allowUpload(ctx, auth.UserID, ipHash); err != nil {
		return Entry{}, err
	}
	owner, err := parseID(auth.UserID)
	if auth.UserID == "" || err != nil {
		return Entry{}, ErrForbidden
	}
	title, description, err := normalizedMetadataUpdate(EntryMetadataUpdate{
		Title: &request.Title, Description: &request.Description,
	})
	if err != nil {
		return Entry{}, err
	}
	var tags []string
	if request.Tags != nil {
		if tags, err = s.validateTagSlugs(ctx, *request.Tags); err != nil {
			return Entry{}, err
		}
	}
	visibility, err := entryVisibility(request.Visibility, true)
	if err != nil {
		return Entry{}, err
	}
	var expiresAt pgtype.Timestamptz
	if request.ExpiresInDays != nil {
		if expiresAt, err = expiryAfter(*request.ExpiresInDays, time.Now()); err != nil {
			return Entry{}, err
		}
	}

	var entry Entry
	err = s.inChange(ctx, func(c *change) error {
		queries := c.queries
		target, err := (entryAccess{queries: queries}).Publishable(ctx, entryID, auth)
		if err != nil {
			return err
		}
		if target.QuickPreview {
			if err := previewFinished(ctx, queries, target.CurrentRevisionID.String); err != nil {
				return err
			}
		} else if request.ExpiresInDays == nil {
			expiresAt = target.ExpiresAt
		}
		published, err := queries.PublishEntry(ctx, db.PublishEntryParams{
			ID: target.ID, OwnerUserID: pgText(owner), Visibility: visibility,
			Title: title.String, Description: description.String, ExpiresAt: expiresAt,
			Spoiler: pgBool(request.Spoiler),
		})
		if err != nil {
			// No row: a preview expired between the lock and the update.
			return notFoundOr(err, "publish entry")
		}
		// Replace the tag set: an anonymous entry may already have tags.
		if request.Tags != nil {
			if err := queries.ClearEntryTags(ctx, target.ID); err != nil {
				return fmt.Errorf("clear entry tags: %w", err)
			}
			if err := queries.SetEntryTags(ctx, db.SetEntryTagsParams{EntryID: target.ID, Slugs: tags}); err != nil {
				return fmt.Errorf("set entry tags: %w", err)
			}
		}
		// A published or claimed entry earns from now on; one published as
		// unlisted or private loses its thread.
		if err := c.entryWritten(ctx, &target, published); err != nil {
			return err
		}
		entry, err = readEntry(ctx, queries, entryID, auth.UserID)
		return err
	})
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// previewFinished requires every leaf of the revision to have its preview and
// its analysis ready (a failed preview is deleted, so only pending ones block).
func previewFinished(ctx context.Context, queries *db.Queries, revisionID string) error {
	rows, err := queries.ListRevisionLeafProgress(ctx, revisionID)
	if err != nil {
		return fmt.Errorf("read leaf progress: %w", err)
	}
	for _, row := range rows {
		if !leafOf(row).Ready() {
			return ErrPreviewNotReady
		}
	}
	return nil
}
