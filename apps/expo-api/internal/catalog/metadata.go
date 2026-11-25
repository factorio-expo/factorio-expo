package catalog

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// EntryMetadataUpdate changes only entry-level metadata, never a revision.
// Nil fields are omitted; an empty description clears it. A non-nil Tags
// replaces the whole curated tag set (an empty list removes every tag).
// Visibility follows the upload rules: unlisted and private need an owner, so
// an anonymous entry can only stay public.
type EntryMetadataUpdate struct {
	Title       *string     `json:"title,omitempty" doc:"A new title."`
	Description *string     `json:"description,omitempty" doc:"A new description."`
	Tags        *[]string   `json:"tags,omitempty" doc:"A new list of tag slugs."`
	Visibility  *Visibility `json:"visibility,omitempty" doc:"A new visibility."`
	// Spoiler marks or unmarks the entry spoiler/NSFW.
	Spoiler *bool `json:"spoiler,omitempty" doc:"Marks or unmarks the entry as a spoiler or NSFW."`
	// Alias sets the vanity alias (alias.go); an empty one removes it.
	Alias *string `json:"alias,omitempty" doc:"The vanity alias (lowercase letters, digits and hyphens, 3 to 60 characters): the entry is also served at /@{owner_username}/{alias}. Only a signed-in owner may set one; an empty string removes it."`
}

func (s *Service) CanEditEntry(ctx context.Context, entryID string, auth Authorization) (bool, error) {
	return s.entries().CanEdit(ctx, entryID, auth)
}

func (s *Service) UpdateEntryMetadata(
	ctx context.Context, entryID string, auth Authorization, update EntryMetadataUpdate,
) (Entry, error) {
	id, err := parseID(entryID)
	if err != nil {
		return Entry{}, err
	}
	var tags []string
	if update.Tags != nil {
		if tags, err = s.validateTagSlugs(ctx, *update.Tags); err != nil {
			return Entry{}, err
		}
	}
	var alias string
	if update.Alias != nil && strings.TrimSpace(*update.Alias) != "" {
		if alias, err = normalizeAlias(*update.Alias); err != nil {
			return Entry{}, err
		}
	}
	var entry Entry
	err = s.inChange(ctx, func(c *change) error {
		queries := c.queries
		existing, err := (entryAccess{queries: queries}).Editable(ctx, entryID, auth)
		if err != nil {
			return err
		}
		title, description, err := normalizedMetadataUpdate(update)
		if err != nil {
			return err
		}
		var visibility pgtype.Text
		if update.Visibility != nil {
			// "" would mean the public default on upload; here it is a mistake.
			if *update.Visibility == "" {
				return ErrInvalidVisibility
			}
			value, err := entryVisibility(string(*update.Visibility), existing.OwnerUserID.Valid)
			if err != nil {
				return err
			}
			visibility = pgtype.Text{String: value, Valid: true}
		}
		updated, err := queries.UpdateEntryMetadata(ctx, db.UpdateEntryMetadataParams{
			ID: id, Title: title, Description: description, Visibility: visibility,
			Spoiler: pgBool(update.Spoiler),
		})
		if err != nil {
			return fmt.Errorf("update entry metadata: %w", err)
		}
		if err := changeAlias(ctx, queries, updated, update.Alias, alias); err != nil {
			return err
		}
		if update.Tags != nil {
			if err := queries.ClearEntryTags(ctx, id); err != nil {
				return fmt.Errorf("clear entry tags: %w", err)
			}
			if err := queries.SetEntryTags(ctx, db.SetEntryTagsParams{EntryID: id, Slugs: tags}); err != nil {
				return fmt.Errorf("set entry tags: %w", err)
			}
		}
		// A visibility change is a listing change: thread, award, search
		// and the edge cache follow it there.
		if err := c.entryWritten(ctx, &existing, updated); err != nil {
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

func normalizedMetadataUpdate(update EntryMetadataUpdate) (pgtype.Text, pgtype.Text, error) {
	var title, description pgtype.Text
	if update.Title == nil && update.Description == nil && update.Tags == nil && update.Visibility == nil &&
		update.Spoiler == nil && update.Alias == nil {
		return title, description, ErrInvalidEntryMetadata
	}
	if update.Title != nil {
		value := strings.TrimSpace(*update.Title)
		if value == "" || utf8.RuneCountInString(value) > MaxTitleRunes {
			return title, description, ErrInvalidEntryMetadata
		}
		title = pgtype.Text{String: value, Valid: true}
	}
	if update.Description != nil {
		value := strings.TrimSpace(*update.Description)
		if utf8.RuneCountInString(value) > MaxDescriptionRunes {
			return title, description, ErrInvalidEntryMetadata
		}
		description = pgtype.Text{String: value, Valid: true}
	}
	return title, description, nil
}

// changeAlias applies an update's alias: absent keeps it, empty removes it,
// otherwise the (normalized) slug replaces it.
func changeAlias(ctx context.Context, queries *db.Queries, entry db.Entry, requested *string, slug string) error {
	switch {
	case requested == nil:
		return nil
	case slug == "":
		if _, err := queries.DeleteEntryAlias(ctx, entry.ID); err != nil {
			return fmt.Errorf("delete alias: %w", err)
		}
		return nil
	default:
		return setAlias(ctx, queries, entry, slug)
	}
}
