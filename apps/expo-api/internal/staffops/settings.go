package staffops

import (
	"context"
	"fmt"
	"slices"
	"time"

	"factorio-expo/apps/expo-api/internal/audit"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/settings"
)

// StaffSetting is one setting as the console shows it.
type StaffSetting struct {
	settings.Definition
	Value string `json:"value"`
	// UpdatedAt and UpdatedBy are the last change; absent while the setting
	// has never been changed.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	UpdatedBy string     `json:"updated_by,omitempty"`
}

// ListSettings lists every setting with its current value, read from the
// table (not the cache).
func (s *Service) ListSettings(ctx context.Context) ([]StaffSetting, error) {
	rows, err := s.queries.ListGlobalSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list global settings: %w", err)
	}
	stored := make(map[string]db.GlobalSetting, len(rows))
	raw := make(map[string]string, len(rows))
	for _, row := range rows {
		stored[row.Key] = row
		raw[row.Key] = row.Value
	}
	values, _ := settings.Merge(raw)
	definitions := settings.Definitions()
	list := make([]StaffSetting, 0, len(definitions))
	for _, definition := range definitions {
		setting := StaffSetting{Definition: definition, Value: values[definition.Key]}
		if row, ok := stored[definition.Key]; ok {
			setting.UpdatedAt = optionalTime(row.UpdatedAt)
			setting.UpdatedBy = row.UpdatedBy
		}
		list = append(list, setting)
	}
	return list, nil
}

// SettingsUpdate is a staff member's change of settings: the keys to set and
// the reason for the audit log.
type SettingsUpdate struct {
	Values map[string]string `json:"values"`
	Reason string            `json:"reason"`
}

// UpdateSettings validates every value, then stores those that differ from
// the current value, with one audit row each, in one transaction. It returns
// the keys that changed (sorted); the cache is dropped, so this instance
// applies them at once.
func (s *Service) UpdateSettings(ctx context.Context, by audit.Staff, update SettingsUpdate) ([]string, error) {
	reason, err := audit.CleanReason(update.Reason)
	if err != nil {
		return nil, err
	}
	normalized := make(map[string]string, len(update.Values))
	for key, raw := range update.Values {
		definition, ok := settings.Lookup(key)
		if !ok {
			return nil, invalidStaffRequest("unknown setting %q", key)
		}
		value, err := definition.Normalize(raw)
		if err != nil {
			return nil, invalidStaffRequest("%s", err.Error())
		}
		normalized[key] = value
	}

	var changed []string
	err = s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		changed = changed[:0]
		rows, err := queries.ListGlobalSettings(ctx)
		if err != nil {
			return fmt.Errorf("list global settings: %w", err)
		}
		raw := make(map[string]string, len(rows))
		for _, row := range rows {
			raw[row.Key] = row.Value
		}
		current, _ := settings.Merge(raw)
		for key, value := range normalized {
			if current[key] == value {
				continue
			}
			if err := queries.UpsertGlobalSetting(ctx, db.UpsertGlobalSettingParams{
				Key: key, Value: value, UpdatedBy: by.Username,
			}); err != nil {
				return fmt.Errorf("store global setting %s: %w", key, err)
			}
			if err := audit.Write(ctx, queries, by, audit.Record{
				Action: "setting.update", TargetType: "setting", TargetID: key, Reason: reason,
				Before: map[string]string{"value": current[key]},
				After:  map[string]string{"value": value},
			}); err != nil {
				return err
			}
			changed = append(changed, key)
		}
		return nil
	})
	s.catalog.InvalidateSettings()
	if err != nil {
		return nil, err
	}
	slices.Sort(changed)
	return changed, nil
}
