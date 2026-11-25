package staffops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The home page's hand-picked leaves (docs/admin.md#home-picks): at most
// catalog.MaxHomePicks, each a blueprint in the current revision of a listed
// entry when picked, with an optional one-line caption. The public read is
// catalog.ListHomePicks.

// maxPickCaptionRunes bounds a pick's caption (home_picks.caption CHECK).
const maxPickCaptionRunes = 140

// StaffHomePick is one pick as the console lists it.
type StaffHomePick struct {
	BlueprintID string    `json:"blueprint_id" doc:"The picked blueprint."`
	EntryID     string    `json:"entry_id" doc:"Its entry."`
	EntryTitle  string    `json:"entry_title" doc:"The entry's title."`
	Label       string    `json:"label" doc:"The blueprint's label."`
	Caption     string    `json:"caption" doc:"Why it was picked; empty for none."`
	PickedAt    time.Time `json:"picked_at" doc:"When it was picked."`
	PickedBy    string    `json:"picked_by" doc:"The staff member who picked it."`
	// Shown is false once the leaf left the current revision of a listed
	// entry: the home page leaves it out.
	Shown bool `json:"shown" doc:"Whether the home page shows it (the leaf is in the current revision of a listed entry)."`
}

// ListHomePicks lists every pick, newest first.
func (s *Service) ListHomePicks(ctx context.Context) ([]StaffHomePick, error) {
	rows, err := s.queries.ListStaffHomePicks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list home picks: %w", err)
	}
	picks := make([]StaffHomePick, 0, len(rows))
	for _, row := range rows {
		picks = append(picks, StaffHomePick{
			BlueprintID: row.BlueprintID, EntryID: row.EntryID, EntryTitle: row.EntryTitle, Label: row.Label,
			Caption: row.Caption, PickedAt: row.PickedAt.Time, PickedBy: row.PickedBy, Shown: row.Shown,
		})
	}
	return picks, nil
}

// cleanCaption trims a caption and checks it is one short line.
func cleanCaption(caption string) (string, error) {
	caption = strings.TrimSpace(caption)
	if !printable(caption) || utf8.RuneCountInString(caption) > maxPickCaptionRunes {
		return "", invalidStaffRequest("the caption is one line of at most %d characters", maxPickCaptionRunes)
	}
	return caption, nil
}

// AddHomePick picks a blueprint for the home page. It must be in the current
// revision of a listed entry and not picked yet, and there is room for at
// most catalog.MaxHomePicks.
func (s *Service) AddHomePick(ctx context.Context, by audit.Staff, blueprintID, caption string) error {
	id, err := parseID(blueprintID)
	if err != nil {
		return invalidStaffRequest("that is not a blueprint id")
	}
	if caption, err = cleanCaption(caption); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		if err := queries.LockHomePicks(ctx); err != nil {
			return fmt.Errorf("lock home picks: %w", err)
		}
		leaf, err := queries.GetPickableLeaf(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidStaffRequest("only a blueprint of the current revision of a public entry can be picked")
		}
		if err != nil {
			return fmt.Errorf("read picked blueprint: %w", err)
		}
		count, err := queries.CountHomePicks(ctx)
		if err != nil {
			return fmt.Errorf("count home picks: %w", err)
		}
		if count >= catalog.MaxHomePicks {
			return invalidStaffRequest("the home page holds at most %d picks: remove one first", catalog.MaxHomePicks)
		}
		inserted, err := queries.InsertHomePick(ctx, db.InsertHomePickParams{
			BlueprintID: leaf.ID, Caption: caption, PickedBy: by.Username,
		})
		if err != nil {
			return fmt.Errorf("add home pick: %w", err)
		}
		if inserted == 0 {
			return invalidStaffRequest("that blueprint is already picked")
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "home_pick.add", TargetType: "blueprint", TargetID: leaf.ID,
			After: map[string]any{"entry_id": leaf.EntryID, "caption": caption},
		})
	})
}

// SetHomePickCaption changes a pick's caption.
func (s *Service) SetHomePickCaption(ctx context.Context, by audit.Staff, blueprintID, caption string) error {
	id, err := parseID(blueprintID)
	if err != nil {
		return err
	}
	if caption, err = cleanCaption(caption); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		before, err := queries.UpdateHomePickCaption(ctx, db.UpdateHomePickCaptionParams{BlueprintID: id, Caption: caption})
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("update home pick: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "home_pick.update", TargetType: "blueprint", TargetID: id,
			Before: map[string]any{"caption": before}, After: map[string]any{"caption": caption},
		})
	})
}

// RemoveHomePick takes a pick off the home page.
func (s *Service) RemoveHomePick(ctx context.Context, by audit.Staff, blueprintID string) error {
	id, err := parseID(blueprintID)
	if err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		caption, err := queries.DeleteHomePick(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("remove home pick: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "home_pick.remove", TargetType: "blueprint", TargetID: id,
			Before: map[string]any{"caption": caption},
		})
	})
}
