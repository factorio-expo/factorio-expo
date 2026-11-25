package challenge

import (
	"math"

	"factorio-expo/internal/analysisresult"
)

// TargetKind is what a parameter needs named.
type TargetKind string

const (
	// TargetNone: the parameter is a single number of the blueprint.
	TargetNone TargetKind = "none"
	// TargetMaterial: an item (with an optional quality) or a fluid.
	TargetMaterial TargetKind = "material"
	// TargetEntity: optionally one entity (and quality); none counts all.
	TargetEntity TargetKind = "entity"
	// TargetRecipe: one recipe (and quality): the entities set to it.
	TargetRecipe TargetKind = "recipe"
	// TargetResource: optionally one raw resource; none sums them all.
	TargetResource TargetKind = "resource"
)

// Param is one parameter of the catalogue: what a goal or threshold can be
// written against.
type Param struct {
	Name   string     `json:"name"`
	Label  string     `json:"label"`
	Unit   string     `json:"unit"`
	Target TargetKind `json:"target"`
	// Platform marks parameters only a space platform has.
	Platform bool `json:"platform"`

	value func(values) (float64, bool)
}

// values is what a parameter reads: the analysis facts (nil when the
// blueprint has no usable analysis) and the blueprint's entities.
type values struct {
	facts *analysisresult.Facts
	// hasBuildCost: the analysis measured the build cost (older snapshots
	// did not).
	hasBuildCost bool
	entities     []EntityCount
	recipes      []EntityCount
	target       Target
}

// EntityCount is one entity (or recipe) name and quality and how many the
// blueprint has (how many entities are set to the recipe).
type EntityCount struct {
	Name    string `json:"name"`
	Quality string `json:"quality"`
	Count   int    `json:"count"`
}

// Params is the catalogue, in the order editors list it.
var Params = []Param{
	{Name: "rate_out", Label: "Output rate", Unit: "/s", Target: TargetMaterial, value: rateOut},
	{Name: "rate_in", Label: "Input rate", Unit: "/s", Target: TargetMaterial, value: rateIn},
	{Name: "rate_net", Label: "Net rate (output minus input)", Unit: "/s", Target: TargetMaterial, value: rateNet},
	{Name: "rate_out_per_tile", Label: "Output per tile", Unit: "/s per tile", Target: TargetMaterial, value: ratePerTile},
	{Name: "rate_out_per_mw", Label: "Output per MW", Unit: "/s per MW", Target: TargetMaterial, value: ratePerMW},
	{Name: "footprint_area", Label: "Footprint area", Unit: "tiles", Target: TargetNone, value: footprint(func(w, h float64) float64 { return w * h })},
	{Name: "footprint_long", Label: "Footprint long side", Unit: "tiles", Target: TargetNone, value: footprint(math.Max)},
	{Name: "footprint_short", Label: "Footprint short side", Unit: "tiles", Target: TargetNone, value: footprint(math.Min)},
	{Name: "entity_count", Label: "Entity count", Unit: "", Target: TargetEntity, value: func(v values) (float64, bool) { return countOf(v.entities, v.target), true }},
	{Name: "recipe_count", Label: "Entities set to recipe", Unit: "", Target: TargetRecipe, value: func(v values) (float64, bool) { return countOf(v.recipes, v.target), true }},
	{Name: "power_consumption", Label: "Power used", Unit: "MW", Target: TargetNone, value: analysed(func(f *analysisresult.Facts) float64 { return f.ElectricityConsumptionWatts / 1e6 })},
	{Name: "power_production", Label: "Power produced", Unit: "MW", Target: TargetNone, value: analysed(func(f *analysisresult.Facts) float64 { return f.ElectricityProductionWatts / 1e6 })},
	{Name: "power_net", Label: "Net power", Unit: "MW", Target: TargetNone, value: analysed(func(f *analysisresult.Facts) float64 { return f.ElectricityNetWatts / 1e6 })},
	{Name: "pollution_net", Label: "Net pollution", Unit: "/min", Target: TargetNone, value: analysed(func(f *analysisresult.Facts) float64 { return f.PollutionNetPerSecond * 60 })},
	{Name: "build_cost_raw", Label: "Build cost (raw resources)", Unit: "", Target: TargetResource, value: buildCost},
	{Name: "platform_speed", Label: "Platform max speed", Unit: "km/s", Target: TargetNone, Platform: true, value: platform(func(p *analysisresult.SpacePlatform) float64 { return *p.MaxSpeedKMS })},
	{Name: "platform_thrust", Label: "Platform thrust", Unit: "MN", Target: TargetNone, Platform: true, value: platform(func(p *analysisresult.SpacePlatform) float64 { return *p.ThrustMN })},
	{Name: "platform_weight", Label: "Platform weight", Unit: "t", Target: TargetNone, Platform: true, value: platform(func(p *analysisresult.SpacePlatform) float64 { return *p.WeightTons })},
	{Name: "platform_thrust_to_weight", Label: "Platform thrust to weight", Unit: "MN/t", Target: TargetNone, Platform: true, value: platform(func(p *analysisresult.SpacePlatform) float64 { return *p.ThrustMN / *p.WeightTons })},
}

var paramByName = func() map[string]Param {
	byName := make(map[string]Param, len(Params))
	for _, param := range Params {
		byName[param.Name] = param
	}
	return byName
}()

// ParamNamed returns the catalogue entry of name.
func ParamNamed(name string) (Param, bool) {
	param, ok := paramByName[name]
	return param, ok
}

func platformOnly(name string) bool { return paramByName[name].Platform }

func analysed(read func(*analysisresult.Facts) float64) func(values) (float64, bool) {
	return func(v values) (float64, bool) {
		if v.facts == nil {
			return 0, false
		}
		return read(v.facts), true
	}
}

// material reads the rate of the target from a rate pair: by quality for an
// item with a quality, else over every quality.
func material(v values, all, byQuality map[string]float64) float64 {
	if v.target.Type == "item" && v.target.Quality != "" {
		return byQuality[analysisresult.QualityKey(v.target.Quality, v.target.Name)]
	}
	return all[v.target.Name]
}

func rateOut(v values) (float64, bool) {
	if v.facts == nil {
		return 0, false
	}
	return material(v, v.facts.OutRates, v.facts.QualityOutRates), true
}

func rateIn(v values) (float64, bool) {
	if v.facts == nil {
		return 0, false
	}
	return material(v, v.facts.InRates, v.facts.QualityInRates), true
}

func rateNet(v values) (float64, bool) {
	out, ok := rateOut(v)
	if !ok {
		return 0, false
	}
	in, _ := rateIn(v)
	return out - in, true
}

func ratePerTile(v values) (float64, bool) {
	out, ok := rateOut(v)
	if !ok || v.facts.FootprintTiles() <= 0 {
		return 0, false
	}
	return out / v.facts.FootprintTiles(), true
}

func ratePerMW(v values) (float64, bool) {
	out, ok := rateOut(v)
	if !ok || v.facts.ConsumptionMW() <= 0 {
		return 0, false
	}
	return out / v.facts.ConsumptionMW(), true
}

func footprint(combine func(w, h float64) float64) func(values) (float64, bool) {
	return func(v values) (float64, bool) {
		if v.facts == nil || v.facts.Footprint == nil {
			return 0, false
		}
		return combine(float64(v.facts.Footprint.Width), float64(v.facts.Footprint.Height)), true
	}
}

// countOf counts the target name (any quality when none is named), or every
// row when no name is given.
func countOf(rows []EntityCount, target Target) float64 {
	total := 0
	for _, row := range rows {
		if target.Name != "" && row.Name != target.Name {
			continue
		}
		if target.Quality != "" && qualityRank(row.Quality) != qualityRank(target.Quality) {
			continue
		}
		total += row.Count
	}
	return float64(total)
}

func buildCost(v values) (float64, bool) {
	if v.facts == nil || !v.hasBuildCost {
		return 0, false
	}
	if v.target.Name != "" {
		return v.facts.RawCost[v.target.Name], true
	}
	total := 0.0
	for _, amount := range v.facts.RawCost {
		total += amount
	}
	return total, true
}

func platform(read func(*analysisresult.SpacePlatform) float64) func(values) (float64, bool) {
	return func(v values) (float64, bool) {
		if v.facts == nil || v.facts.Platform == nil || !v.facts.Platform.IsSpacePlatform {
			return 0, false
		}
		p := v.facts.Platform
		if p.MaxSpeedKMS == nil || p.ThrustMN == nil || p.WeightTons == nil || *p.WeightTons <= 0 {
			return 0, false
		}
		return read(p), true
	}
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
