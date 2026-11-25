package catalog

import (
	"context"
	"encoding/json"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/mods"
)

// The blueprint string tools: decode, encode and inspect. They store nothing
// and dispatch nothing, and share the submission limits, so what inspect
// accepts is exactly what an upload accepts.

// Inspection is the upload form's summary of a pasted exchange string.
type Inspection struct {
	Kind           blueprint.Kind         `json:"kind" doc:"The root's kind."`
	Label          string                 `json:"label" doc:"The root's label."`
	Description    string                 `json:"description" doc:"The root's description."`
	Icons          string                 `json:"icons" doc:"The root's icons."`
	GameVersion    string                 `json:"game_version" doc:"The Factorio version that exported the string."`
	BlueprintCount int                    `json:"blueprint_count" doc:"How many blueprints the string holds."`
	PlannerCount   int                    `json:"planner_count" doc:"How many planners the string holds."`
	EntityCount    int                    `json:"entity_count" doc:"The total number of entities."`
	Entities       []blueprint.EntityStat `json:"entities" doc:"Entity totals per name and quality."`
	Recipes        []blueprint.RecipeStat `json:"recipes" doc:"Recipe totals: entities set to each recipe and quality."`
	// SuggestedTags are curated tag slugs the upload form pre-selects.
	SuggestedTags []string `json:"suggested_tags" doc:"Curated tag slugs the upload form pre-selects."`
	// Mods are the mods the blueprint needs (a guess beyond the built-in ones).
	Mods mods.Detection `json:"mods" doc:"The mods the whole submission needs (a guess beyond the built-in ones)."`
	// Compat21 is "compatible" or "incompatible" for a submission made before
	// Factorio 2.1, empty for one made in 2.1.
	Compat21 Compat21 `json:"compat_21" doc:"The Factorio 2.1 compatibility of a submission made before 2.1; empty for one made in 2.1."`
}

// DecodeBlueprint returns an exchange string's JSON as Factorio wrote it.
// Any decodable string is accepted, planners included.
func (s *Service) DecodeBlueprint(exchange string) (json.RawMessage, error) {
	return blueprint.Decode(exchange, s.limits.MaxDecodedBytes)
}

// EncodeBlueprint compresses JSON into an exchange string, then parses the
// result like an upload, so it never returns a string the catalog (or
// Factorio) would reject.
func (s *Service) EncodeBlueprint(payload json.RawMessage) (string, error) {
	exchange, err := blueprint.Encode(payload)
	if err != nil {
		return "", err
	}
	if _, err := blueprint.Parse(exchange, s.limits); err != nil {
		return "", err
	}
	return exchange, nil
}

// InspectBlueprint parses an exchange string like an upload and summarizes it.
func (s *Service) InspectBlueprint(_ context.Context, exchange string) (Inspection, error) {
	root, err := blueprint.Parse(exchange, s.limits)
	if err != nil {
		return Inspection{}, err
	}
	summary := blueprint.Summarize(root)
	return Inspection{
		Kind:           summary.Kind,
		Label:          summary.Label,
		Description:    summary.Description,
		Icons:          summary.Icons,
		GameVersion:    summary.GameVersion,
		BlueprintCount: summary.BlueprintCount,
		PlannerCount:   summary.PlannerCount,
		EntityCount:    summary.EntityCount,
		Entities:       summary.Entities,
		Recipes:        summary.Recipes,
		SuggestedTags:  suggestTags(s.currentTagRules(), summary, s.tagMinScore),
		Mods:           detectMods(s.mods, summary.Entities, summary.Recipes),
		Compat21:       Compat21(summary.Compat21),
	}, nil
}
