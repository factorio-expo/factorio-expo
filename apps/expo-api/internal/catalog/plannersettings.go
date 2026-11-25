package catalog

import (
	"encoding/json"
	"fmt"

	"factorio-expo/apps/expo-api/internal/blueprint"
)

// PlannerSettings is a planner node's configuration, by the node's kind: an
// upgrade planner's mappers or a deconstruction planner's filters. It
// answers as the one that is set (httpapi documents it as a oneOf).
type PlannerSettings struct {
	Upgrade        *blueprint.UpgradeSettings
	Deconstruction *blueprint.DeconstructionSettings
}

func (p PlannerSettings) MarshalJSON() ([]byte, error) {
	if p.Upgrade != nil {
		return json.Marshal(p.Upgrade)
	}
	return json.Marshal(p.Deconstruction)
}

// plannerSettings decodes a planner row's stored settings by its kind.
func plannerSettings(kind string, raw []byte) (*PlannerSettings, error) {
	var settings PlannerSettings
	var target any
	switch blueprint.Kind(kind) {
	case blueprint.KindUpgradePlanner:
		settings.Upgrade = &blueprint.UpgradeSettings{}
		target = settings.Upgrade
	case blueprint.KindDeconstructionPlanner:
		settings.Deconstruction = &blueprint.DeconstructionSettings{}
		target = settings.Deconstruction
	default:
		return nil, fmt.Errorf("planner of kind %q", kind)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, fmt.Errorf("decode %s settings: %w", kind, err)
	}
	return &settings, nil
}
