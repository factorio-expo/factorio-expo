package analysisresult

import (
	"encoding/json"
	"fmt"
)

// The public API's view of a stored snapshot: each section decoded into the
// types below (analysis.lua is their exact schema; docs/production-rate-calculation.md).
// Older snapshots may lack a field: numbers then read as zero and optional
// parts as absent.

// MaterialRate is one row of the ingredients, products or intermediates: a
// material's per-second rate as the rate calculator reports it. Ingredients
// carry the input side, products the output side, intermediates both and
// their difference. A fluid is its name alone (rates at every temperature
// summed) and has no quality.
type MaterialRate struct {
	Type string `json:"type" enum:"item,fluid" doc:"item or fluid."`
	Name string `json:"name" doc:"The item's or fluid's name."`
	// Quality is null for a fluid.
	Quality             *string  `json:"quality" doc:"The item's quality; null for a fluid."`
	InputPerSecond      *float64 `json:"input_per_second,omitempty" doc:"How much the blueprint uses per second."`
	OutputPerSecond     *float64 `json:"output_per_second,omitempty" doc:"How much the blueprint makes per second."`
	DifferencePerSecond *float64 `json:"difference_per_second,omitempty" doc:"Output minus input per second."`
	// ProducerCount and ConsumerCount are the machines making and using it.
	ProducerCount *float64 `json:"producer_count,omitempty" doc:"How many machines make it."`
	ConsumerCount *float64 `json:"consumer_count,omitempty" doc:"How many machines use it."`
}

// Completeness says whether a metric accounts for every entity:
// "not_applicable" for pollution on a surface without one.
type Completeness struct {
	Status          string `json:"status" enum:"complete,partial,not_applicable" doc:"complete, partial (some machines could not be calculated) or not_applicable."`
	UnresolvedCount int64  `json:"unresolved_count" doc:"Entities the metric could not account for."`
}

// Electricity is what the entities make and use at full load, in watts.
type Electricity struct {
	ConsumptionWatts float64       `json:"consumption_watts" doc:"Used, drain included."`
	ProductionWatts  float64       `json:"production_watts" doc:"The power the blueprint produces in watts."`
	NetWatts         float64       `json:"net_watts" doc:"Production minus consumption."`
	DrainWatts       *float64      `json:"drain_watts,omitempty" doc:"The standing power drain of the buildings in watts."`
	Completeness     *Completeness `json:"completeness,omitempty" doc:"Whether the electricity figures are complete."`
}

// Pollution is the surface's tracked pollutant per second, every emitted type
// summed; ByPollutant breaks the types out.
type Pollution struct {
	// Pollutant is the surface's tracked type; null where it tracks none.
	Pollutant           *string         `json:"pollutant" doc:"The surface's tracked pollutant; null where it tracks none."`
	ProductionPerSecond float64         `json:"production_per_second" doc:"Pollution produced per second."`
	AbsorptionPerSecond float64         `json:"absorption_per_second" doc:"Pollution absorbed per second."`
	NetPerSecond        float64         `json:"net_per_second" doc:"Production minus absorption per second."`
	NotApplicable       bool            `json:"not_applicable,omitempty" doc:"True where the surface tracks no pollution."`
	Completeness        *Completeness   `json:"completeness,omitempty" doc:"Whether the pollution figures are complete."`
	ByPollutant         []PollutantRate `json:"by_pollutant,omitempty" doc:"The same per pollutant."`
}

// PollutantRate is one pollutant type's rates.
type PollutantRate struct {
	Pollutant           string  `json:"pollutant" doc:"The pollutant's name."`
	ProductionPerSecond float64 `json:"production_per_second" doc:"Pollution produced per second."`
	AbsorptionPerSecond float64 `json:"absorption_per_second" doc:"Pollution absorbed per second."`
	NetPerSecond        float64 `json:"net_per_second" doc:"Production minus absorption per second."`
}

// Unresolved is why some entities are not accounted for: a technical failure
// of the analysis, never something a blueprint merely lacks (a furnace's
// recipe, a burner's fuel, ore under a drill, the fluid an offshore pump
// stands on).
type Unresolved struct {
	Cause  string `json:"cause" enum:"unavailable_prototype,entity_not_materialized,missing_modules,calculation_error,unknown_material,invalid_value,fluid_unresolved" doc:"Why: unavailable_prototype (a mod entity missing from the analysis), entity_not_materialized (a ghost that never became an entity), missing_modules (module requests that could not be filled), calculation_error (the calculator failed on the entity), unknown_material (a rate for an item or fluid that does not exist), invalid_value (a computed rate that is not a finite positive number) or fluid_unresolved (a boiler, generator or fluid energy source whose fluid is ambiguous, usually a mod)."`
	Entity string `json:"entity" doc:"The entity's prototype name; empty when unknown."`
	Count  int64  `json:"count" doc:"How many entities."`
}

// SectionCompleteness is each metric's completeness.
type SectionCompleteness struct {
	Materials   *Completeness `json:"materials,omitempty" doc:"Completeness of the material rates."`
	Electricity *Completeness `json:"electricity,omitempty" doc:"Completeness of the electricity figures."`
	Pollution   *Completeness `json:"pollution,omitempty" doc:"Completeness of the pollution figures."`
	Unresolved  []Unresolved  `json:"unresolved,omitempty" doc:"The causes behind the metrics that are partial, by cause and entity; absent when every metric is complete."`
}

// Calculator names the rate calculator and the game it ran on.
type Calculator struct {
	Implementation  string `json:"implementation,omitempty" doc:"What calculated the rates."`
	UpstreamVersion string `json:"upstream_version,omitempty" doc:"The calculator's release version."`
	UpstreamCommit  string `json:"upstream_commit,omitempty" doc:"The calculator's source revision."`
	FactorioVersion string `json:"factorio_version,omitempty" doc:"The Factorio version of that content."`
	ContentSet      string `json:"content_set,omitempty" doc:"The game content the analysis ran against."`
}

// View is the snapshot as the public API presents it.
type View struct {
	Ingredients   []MaterialRate
	Products      []MaterialRate
	Intermediates []MaterialRate
	Electricity   *Electricity
	Pollution     *Pollution
	Completeness  *SectionCompleteness
	Calculator    *Calculator
	SpacePlatform *SpacePlatform
	// Footprint and BuildCost are nil for a snapshot stored without them.
	Footprint *Footprint
	BuildCost *BuildCost
}

// View returns the API presentation of the snapshot.
func (s *Snapshot) View() (View, error) {
	platform := s.Platform
	view := View{SpacePlatform: &platform, Footprint: s.Footprint, BuildCost: s.BuildCost}
	for key, target := range map[string]any{
		"ingredients": &view.Ingredients, "products": &view.Products,
		"intermediates": &view.Intermediates, "electricity": &view.Electricity,
		"pollution": &view.Pollution, "completeness": &view.Completeness,
		"calculator": &view.Calculator,
	} {
		raw, ok := s.raw[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return View{}, fmt.Errorf("decode production %s: %w", key, err)
		}
	}
	return view, nil
}
