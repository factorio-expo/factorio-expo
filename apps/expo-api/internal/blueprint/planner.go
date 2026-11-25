package blueprint

import (
	"encoding/json"
	"sort"
)

// Planner is an upgrade or deconstruction planner: a leaf of the tree that
// is never rendered or analysed, only listed and copied. ExchangeString is
// self-contained, like a blueprint leaf's.
type Planner struct {
	Label          string
	Description    string
	Icons          string
	ExchangeString string
	// Settings is the planner's configuration in the stored shape:
	// UpgradeSettings or DeconstructionSettings, by the node's Kind.
	Settings any
	Version  string
}

// PlannerFilter is one slot of a planner: an entity, item or tile, with an
// optional quality condition. An empty Quality matches any quality.
type PlannerFilter struct {
	Index int    `json:"index" doc:"The filter's slot."`
	Type  string `json:"type,omitempty" doc:"What the filter names (item, entity, tile and so on)."`
	Name  string `json:"name" doc:"The item or entity filtered on."`
	// Quality and Comparator form the quality condition ("=", "<", ">", "≤",
	// "≥", "≠"); both are empty for any quality.
	Quality    string `json:"quality,omitempty" doc:"The quality of the filter."`
	Comparator string `json:"comparator,omitempty" doc:"The quality comparison operator of the filter."`
	// ModuleLimit caps how many modules an upgrade target replaces; 0 is all.
	ModuleLimit int `json:"module_limit,omitempty" doc:"How many modules an upgrade target replaces."`
}

// UpgradeMapping is one from -> to row of an upgrade planner. Either side may
// be missing in a half-filled slot.
type UpgradeMapping struct {
	Index int            `json:"index" doc:"The mapper's slot."`
	From  *PlannerFilter `json:"from,omitempty" doc:"What is upgraded from."`
	To    *PlannerFilter `json:"to,omitempty" doc:"What it is upgraded to."`
}

// UpgradeSettings is the stored shape of an upgrade planner.
type UpgradeSettings struct {
	Mappers []UpgradeMapping `json:"mappers" doc:"The from/to pairs of the upgrade planner."`
}

// DeconstructionSettings is the stored shape of a deconstruction planner.
// Filter modes are "whitelist" or "blacklist"; TileSelectionMode is
// "normal", "always", "never" or "only".
type DeconstructionSettings struct {
	EntityFilterMode  string          `json:"entity_filter_mode" enum:"whitelist,blacklist" doc:"Whether the entity filters are a whitelist or a blacklist."`
	EntityFilters     []PlannerFilter `json:"entity_filters" doc:"The entities the planner filters on."`
	TreesAndRocksOnly bool            `json:"trees_and_rocks_only" doc:"Whether it only removes trees and rocks."`
	TileFilterMode    string          `json:"tile_filter_mode" enum:"whitelist,blacklist" doc:"Whether the tile filters are a whitelist or a blacklist."`
	TileFilters       []PlannerFilter `json:"tile_filters" doc:"The tiles the planner filters on."`
	TileSelectionMode string          `json:"tile_selection_mode" enum:"normal,always,never,only" doc:"Which tiles the planner selects (0 normal, 1 always, 2 never, 3 only)."`
}

// plannerPayload is the subset of a decoded planner the catalog reads. Label
// sits on the planner; description and icons inside its settings (older
// strings put the description beside the label, so both are read).
type plannerPayload struct {
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Version     uint64          `json:"version"`
	Settings    json.RawMessage `json:"settings"`
}

type plannerSettingsPayload struct {
	Description string          `json:"description"`
	Icons       json.RawMessage `json:"icons"`

	Mappers []struct {
		Index int            `json:"index"`
		From  *filterPayload `json:"from"`
		To    *filterPayload `json:"to"`
	} `json:"mappers"`

	EntityFilterMode  int             `json:"entity_filter_mode"`
	EntityFilters     []filterPayload `json:"entity_filters"`
	TreesAndRocksOnly bool            `json:"trees_and_rocks_only"`
	TileFilterMode    int             `json:"tile_filter_mode"`
	TileFilters       []filterPayload `json:"tile_filters"`
	TileSelectionMode int             `json:"tile_selection_mode"`
}

type filterPayload struct {
	Index       int    `json:"index"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Quality     string `json:"quality"`
	Comparator  string `json:"comparator"`
	ModuleLimit int    `json:"module_limit"`
}

func (f *filterPayload) filter(index int) *PlannerFilter {
	if f == nil || f.Name == "" {
		return nil
	}
	filter := &PlannerFilter{
		Index: index, Type: f.Type, Name: f.Name, Quality: f.Quality,
		Comparator: f.Comparator, ModuleLimit: f.ModuleLimit,
	}
	// A quality without a comparator is an exact match.
	if filter.Quality != "" && filter.Comparator == "" {
		filter.Comparator = "="
	}
	return filter
}

func filterList(raw []filterPayload) []PlannerFilter {
	filters := make([]PlannerFilter, 0, len(raw))
	for _, entry := range raw {
		if filter := entry.filter(entry.Index); filter != nil {
			filters = append(filters, *filter)
		}
	}
	sort.SliceStable(filters, func(i, j int) bool { return filters[i].Index < filters[j].Index })
	return filters
}

func filterMode(mode int) string {
	if mode == 1 {
		return "blacklist"
	}
	return "whitelist"
}

func tileSelectionMode(mode int) string {
	switch mode {
	case 1:
		return "always"
	case 2:
		return "never"
	case 3:
		return "only"
	default:
		return "normal"
	}
}

// walkPlanner hydrates an upgrade or deconstruction planner and rebuilds its
// standalone exchange string, like walkBlueprint does for a blueprint.
func (s *walkState) walkPlanner(kind Kind, raw json.RawMessage) (*Planner, error) {
	var payload plannerPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, ErrInvalidExchangeString
	}
	var settings plannerSettingsPayload
	if len(payload.Settings) > 0 && string(payload.Settings) != "null" {
		if err := json.Unmarshal(payload.Settings, &settings); err != nil {
			return nil, ErrInvalidExchangeString
		}
	}
	icons, err := iconsToRichText(settings.Icons)
	if err != nil {
		return nil, err
	}
	exchange, err := Encode(map[string]json.RawMessage{string(kind): raw})
	if err != nil {
		return nil, err
	}

	planner := &Planner{
		Label: payload.Label, Description: settings.Description, Icons: icons,
		ExchangeString: exchange, Version: formatVersion(payload.Version),
	}
	if planner.Description == "" {
		planner.Description = payload.Description
	}
	if kind == KindUpgradePlanner {
		mappers := make([]UpgradeMapping, 0, len(settings.Mappers))
		for _, mapper := range settings.Mappers {
			mapping := UpgradeMapping{
				Index: mapper.Index, From: mapper.From.filter(mapper.Index), To: mapper.To.filter(mapper.Index),
			}
			// A target has no condition: its quality is what it becomes
			// (empty is normal).
			if mapping.To != nil {
				mapping.To.Comparator = ""
			}
			if mapping.From != nil || mapping.To != nil {
				mappers = append(mappers, mapping)
			}
		}
		sort.SliceStable(mappers, func(i, j int) bool { return mappers[i].Index < mappers[j].Index })
		planner.Settings = UpgradeSettings{Mappers: mappers}
		return planner, nil
	}
	planner.Settings = DeconstructionSettings{
		EntityFilterMode:  filterMode(settings.EntityFilterMode),
		EntityFilters:     filterList(settings.EntityFilters),
		TreesAndRocksOnly: settings.TreesAndRocksOnly,
		TileFilterMode:    filterMode(settings.TileFilterMode),
		TileFilters:       filterList(settings.TileFilters),
		TileSelectionMode: tileSelectionMode(settings.TileSelectionMode),
	}
	return planner, nil
}
