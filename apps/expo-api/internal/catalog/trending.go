package catalog

import (
	"context"
	"fmt"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
)

// TrendingPeriod is the window trending counts views in. Windows are calendar
// periods in UTC (today, since Monday, since the 1st), not rolling ones.
type TrendingPeriod string

const (
	TrendingDay   TrendingPeriod = "day"
	TrendingWeek  TrendingPeriod = "week"
	TrendingMonth TrendingPeriod = "month"
)

// MaxTrending is the most entries one trending read returns.
const MaxTrending = 24

// ErrInvalidTrendingPeriod answers an unknown period.
var ErrInvalidTrendingPeriod = fault.New(fault.Invalid, "invalid_period", "The period must be day, week or month.")

// ListTrending returns the listed entries with the most page views in the
// period, most viewed first, as cards for the viewer. Fewer than limit (or none)
// early in a period.
func (s *Service) ListTrending(ctx context.Context, period TrendingPeriod, limit int32, viewerUserID string) ([]EntrySummary, error) {
	switch period {
	case TrendingDay, TrendingWeek, TrendingMonth:
	default:
		return nil, ErrInvalidTrendingPeriod
	}
	if limit < 1 || limit > MaxTrending {
		limit = MaxTrending
	}
	ids, err := s.queries.ListTrendingEntryIDs(ctx, db.ListTrendingEntryIDsParams{
		Period: string(period), RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list trending entries: %w", err)
	}
	return s.entryCards(ctx, ids, viewerUserID)
}

// PruneViewDays deletes the daily view rows of earlier months.
func (s *Service) PruneViewDays(ctx context.Context) (int64, error) {
	deleted, err := s.queries.DeleteOldEntryViewDays(ctx)
	if err != nil {
		return 0, fmt.Errorf("prune view days: %w", err)
	}
	return deleted, nil
}
