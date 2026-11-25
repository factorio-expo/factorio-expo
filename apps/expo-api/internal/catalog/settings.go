package catalog

import (
	"context"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/settings"
)

// Global settings (docs/admin.md#settings): the emergency switches and the
// site banner. The API reads them through a short cache (Settings); staff
// change them from the console (UpdateSettings), each change one audit row.

// Settings returns the effective global settings, at most settings.DefaultCacheTTL
// old. It never fails: see settings.Cache.
func (s *Service) Settings(ctx context.Context) settings.Values {
	return s.settings.Get(ctx)
}

func loadSettings(queries *db.Queries) settings.Loader {
	return func(ctx context.Context) (map[string]string, error) {
		rows, err := queries.ListGlobalSettings(ctx)
		if err != nil {
			return nil, err
		}
		stored := make(map[string]string, len(rows))
		for _, row := range rows {
			stored[row.Key] = row.Value
		}
		return stored, nil
	}
}
