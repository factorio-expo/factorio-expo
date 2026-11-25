package catalog

import (
	"context"
	"fmt"
	"time"
)

// MaxHomePicks is how many hand-picked leaves the home page holds.
const MaxHomePicks = 6

// HomePick is a leaf staff picked for the home page, with why.
type HomePick struct {
	Leaf     LeafRef
	Caption  string
	PickedAt time.Time
}

// ListHomePicks returns the picks that show, newest first: those whose leaf
// is in the current revision of a listed entry (the others drop out until
// staff remove them). Staff manage them in internal/staffops.
func (s *Service) ListHomePicks(ctx context.Context) ([]HomePick, error) {
	rows, err := s.queries.ListShownHomePicks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list home picks: %w", err)
	}
	picks := make([]HomePick, 0, len(rows))
	for _, row := range rows {
		picks = append(picks, HomePick{
			Leaf: leafRef(row.EntryID, row.EntryTitle, row.RootKind, row.BlueprintID,
				row.Label, row.Description, row.Icons),
			Caption: row.Caption, PickedAt: row.PickedAt.Time,
		})
	}
	return picks, nil
}
