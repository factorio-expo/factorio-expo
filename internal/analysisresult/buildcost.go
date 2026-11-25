package analysisresult

import (
	"encoding/json"
	"errors"
	"math"
)

// Footprint is the tile-aligned width and height of everything a blueprint
// places, entities by their collision boxes and tiles by their tile.
type Footprint struct {
	Width  int `json:"width" doc:"The width in tiles."`
	Height int `json:"height" doc:"The height in tiles."`
}

// BuildItem is one item a blueprint needs to be built, per quality.
type BuildItem struct {
	Type    string `json:"type" enum:"item" doc:"Always item."`
	Name    string `json:"name" doc:"The item's name."`
	Quality string `json:"quality" doc:"The item's quality."`
	Count   int64  `json:"count" doc:"How many are needed."`
}

// RawMaterial is one raw resource behind the build items, at normal quality.
type RawMaterial struct {
	Type   string  `json:"type" enum:"item,fluid" doc:"item or fluid."`
	Name   string  `json:"name" doc:"The resource's name."`
	Amount float64 `json:"amount" doc:"How much is needed."`
}

// BuildCost is what a blueprint costs to build: its items and the raw
// resources those break down to through the pinned game's recipes.
type BuildCost struct {
	Items []BuildItem   `json:"items" doc:"The items needed to build it, by item and quality."`
	Raw   []RawMaterial `json:"raw" doc:"The raw resources behind those items."`
}

func decodeFootprint(raw json.RawMessage) (*Footprint, error) {
	var footprint Footprint
	var fields map[string]json.RawMessage
	if !isObject(raw) || json.Unmarshal(raw, &fields) != nil || fields["width"] == nil || fields["height"] == nil ||
		json.Unmarshal(raw, &footprint) != nil || footprint.Width < 1 || footprint.Height < 1 {
		return nil, errors.New("analysis result has invalid footprint")
	}
	return &footprint, nil
}

func decodeBuildCost(raw json.RawMessage) (*BuildCost, error) {
	invalid := errors.New("analysis result has invalid build_cost")
	var cost BuildCost
	if !isObject(raw) || json.Unmarshal(raw, &cost) != nil || cost.Items == nil || cost.Raw == nil {
		return nil, invalid
	}
	for _, item := range cost.Items {
		if item.Type != "item" || item.Name == "" || item.Quality == "" || item.Count < 1 {
			return nil, invalid
		}
	}
	for _, material := range cost.Raw {
		if (material.Type != "item" && material.Type != "fluid") || material.Name == "" ||
			math.IsNaN(material.Amount) || math.IsInf(material.Amount, 0) || material.Amount <= 0 {
			return nil, invalid
		}
	}
	return &cost, nil
}
