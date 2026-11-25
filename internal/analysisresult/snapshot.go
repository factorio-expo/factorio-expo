package analysisresult

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"sort"
)

// snapshotKeys are the analysis sections carried into the durable snapshot
// as-is. state lives in its own column.
var snapshotKeys = []string{
	"ingredients", "products", "intermediates",
	"electricity", "pollution", "completeness", "calculator", "space_platform",
	"footprint", "build_cost",
}

// differenceTolerance bounds |difference - (output - input)| relative to the
// larger operand: the bridge computes the difference at full precision, so the
// recomputed value only differs by encoding rounding of the operands.
const differenceTolerance = 1e-9

// SpacePlatform is the bridge's theoretical platform profile. Metrics are nil
// unless IsSpacePlatform is true.
type SpacePlatform struct {
	IsSpacePlatform bool     `json:"is_space_platform" doc:"Whether the blueprint is a space platform."`
	WeightTons      *float64 `json:"weight_tons,omitempty" doc:"Its weight in tons."`
	ThrustMN        *float64 `json:"thrust_mn,omitempty" doc:"Its thrust in meganewtons."`
	MaxSpeedKMS     *float64 `json:"max_speed_km_s,omitempty" doc:"The platform's top speed in km/s."`
}

// Material is one row of the ingredients, products or intermediates arrays.
// Quality is empty for fluids. A fluid is identified by its name alone: the
// bridge sums its rates over every temperature.
type Material struct {
	Type                string
	Name                string
	Quality             string
	InputPerSecond      float64
	OutputPerSecond     float64
	DifferencePerSecond float64
}

// Snapshot is the decoded durable production snapshot. It is decoded once;
// its accessors do no parsing.
type Snapshot struct {
	Ingredients   []Material
	Products      []Material
	Intermediates []Material
	// ElectricityProductionWatts and ElectricityConsumptionWatts are the
	// electricity made and used (both non-negative; consumption includes
	// drain); ElectricityNetWatts and PollutionNetPerSecond are the aggregate
	// nets.
	ElectricityProductionWatts  float64
	ElectricityConsumptionWatts float64
	ElectricityNetWatts         float64
	PollutionNetPerSecond       float64
	Platform                    SpacePlatform
	// Footprint and BuildCost are nil in snapshots stored before the bridge
	// reported them.
	Footprint *Footprint
	BuildCost *BuildCost

	raw map[string]json.RawMessage
}

// ParseSnapshot decodes a stored blueprint_stats.production snapshot written
// with schemaVersion. Stored snapshots were validated on import and are only
// shape-checked; a snapshot without space_platform (written before it was
// stored) reads as a non-platform.
func ParseSnapshot(raw []byte, schemaVersion int32) (*Snapshot, error) {
	if schemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported production schema version %d", schemaVersion)
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sections); err != nil {
		return nil, fmt.Errorf("decode production snapshot: %w", err)
	}
	return decodeSnapshot(sections, false)
}

type materialRow struct {
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Quality    json.RawMessage `json:"quality"`
	Input      *float64        `json:"input_per_second"`
	Output     *float64        `json:"output_per_second"`
	Difference *float64        `json:"difference_per_second"`
}

type metrics struct {
	Consumption *float64 `json:"consumption_watts"`
	Production  *float64 `json:"production_watts"`
	Drain       *float64 `json:"drain_watts"`
	NetWatts    *float64 `json:"net_watts"`
	PollProd    *float64 `json:"production_per_second"`
	PollAbsorb  *float64 `json:"absorption_per_second"`
	PollNet     *float64 `json:"net_per_second"`
}

// decodeSnapshot decodes every section once. strict applies the full import
// validation; otherwise only the structure the derivations need is checked.
func decodeSnapshot(sections map[string]json.RawMessage, strict bool) (*Snapshot, error) {
	snapshot := &Snapshot{raw: sections}
	for _, group := range []string{"ingredients", "products", "intermediates"} {
		raw, ok := sections[group]
		if !ok {
			return nil, fmt.Errorf("analysis result is missing %s", group)
		}
		var rows []materialRow
		if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
			return nil, fmt.Errorf("analysis result has invalid %s", group)
		}
		materials := make([]Material, 0, len(rows))
		for _, row := range rows {
			material, err := row.material(group, strict)
			if err != nil {
				return nil, err
			}
			materials = append(materials, material)
		}
		switch group {
		case "ingredients":
			snapshot.Ingredients = materials
		case "products":
			snapshot.Products = materials
		default:
			snapshot.Intermediates = materials
		}
	}

	var electricity, pollution metrics
	if err := decodeObject(sections["electricity"], &electricity); err != nil {
		return nil, errors.New("analysis result has invalid electricity")
	}
	if err := decodeObject(sections["pollution"], &pollution); err != nil {
		return nil, errors.New("analysis result has invalid pollution")
	}
	if strict {
		if !validMetrics([]*float64{electricity.Consumption, electricity.Production, electricity.Drain}, electricity.NetWatts) {
			return nil, errors.New("analysis result has invalid electricity")
		}
		if !validMetrics([]*float64{pollution.PollProd, pollution.PollAbsorb}, pollution.PollNet) {
			return nil, errors.New("analysis result has invalid pollution")
		}
	}
	snapshot.ElectricityProductionWatts = deref(electricity.Production)
	snapshot.ElectricityConsumptionWatts = deref(electricity.Consumption)
	snapshot.ElectricityNetWatts = deref(electricity.NetWatts)
	snapshot.PollutionNetPerSecond = deref(pollution.PollNet)

	rawPlatform, ok := sections["space_platform"]
	switch {
	case ok:
		platform, err := decodePlatform(rawPlatform)
		if err != nil {
			return nil, err
		}
		snapshot.Platform = platform
	case strict:
		return nil, errors.New("analysis result is missing space_platform")
	default:
		copied := make(map[string]json.RawMessage, len(sections)+1)
		maps.Copy(copied, sections)
		copied["space_platform"] = json.RawMessage(`{"is_space_platform":false}`)
		snapshot.raw = copied
	}

	if raw, ok := sections["footprint"]; ok || strict {
		footprint, err := decodeFootprint(raw)
		if err != nil {
			return nil, err
		}
		snapshot.Footprint = footprint
	}
	if raw, ok := sections["build_cost"]; ok || strict {
		cost, err := decodeBuildCost(raw)
		if err != nil {
			return nil, err
		}
		snapshot.BuildCost = cost
	}
	return snapshot, nil
}

func decodeObject(raw json.RawMessage, target any) error {
	if !isObject(raw) {
		return errors.New("not an object")
	}
	return json.Unmarshal(raw, target)
}

func (row materialRow) material(group string, strict bool) (Material, error) {
	material := Material{
		Type: row.Type, Name: row.Name,
		InputPerSecond: deref(row.Input), OutputPerSecond: deref(row.Output),
		DifferencePerSecond: deref(row.Difference),
	}
	if len(row.Quality) > 0 && !isNull(row.Quality) {
		if err := json.Unmarshal(row.Quality, &material.Quality); err != nil && strict {
			return Material{}, errors.New("analysis result has invalid item identity")
		}
	}
	if !strict {
		return material, nil
	}
	if row.Type != "item" && row.Type != "fluid" {
		return Material{}, errors.New("analysis result has invalid material type")
	}
	if row.Name == "" {
		return Material{}, errors.New("analysis result has invalid material name")
	}
	if row.Type == "item" {
		if material.Quality == "" {
			return Material{}, errors.New("analysis result has invalid item identity")
		}
	} else if !isNull(row.Quality) {
		return Material{}, errors.New("analysis result has invalid fluid identity")
	}
	rate := func(value *float64) bool { return value != nil && finite(*value) && *value >= 0 }
	switch group {
	case "ingredients":
		if !rate(row.Input) {
			return Material{}, errors.New("analysis result has invalid material rate")
		}
	case "products":
		if !rate(row.Output) {
			return Material{}, errors.New("analysis result has invalid material rate")
		}
	default:
		if !rate(row.Input) || !rate(row.Output) {
			return Material{}, errors.New("analysis result has invalid material rate")
		}
		if row.Difference == nil || !finite(*row.Difference) ||
			math.Abs(*row.Difference-(*row.Output-*row.Input)) >
				differenceTolerance*math.Max(1, math.Max(*row.Input, *row.Output)) {
			return Material{}, errors.New("analysis result has invalid material difference")
		}
	}
	return material, nil
}

func validMetrics(nonnegative []*float64, signed *float64) bool {
	for _, value := range nonnegative {
		if value == nil || !finite(*value) || *value < 0 {
			return false
		}
	}
	return signed != nil && finite(*signed)
}

func decodePlatform(raw json.RawMessage) (SpacePlatform, error) {
	var fields map[string]json.RawMessage
	if !isObject(raw) || json.Unmarshal(raw, &fields) != nil {
		return SpacePlatform{}, errors.New("analysis result has invalid space_platform")
	}
	if _, ok := fields["is_space_platform"]; !ok {
		return SpacePlatform{}, errors.New("space_platform is missing is_space_platform")
	}
	var platform SpacePlatform
	if err := json.Unmarshal(raw, &platform); err != nil {
		return SpacePlatform{}, fmt.Errorf("analysis result has invalid space_platform: %w", err)
	}
	if !platform.IsSpacePlatform {
		if platform.WeightTons != nil || platform.ThrustMN != nil || platform.MaxSpeedKMS != nil {
			return SpacePlatform{}, errors.New("non-platform analysis result must not contain platform metrics")
		}
		return platform, nil
	}
	if platform.WeightTons == nil || platform.ThrustMN == nil || platform.MaxSpeedKMS == nil ||
		!finite(*platform.WeightTons) || !finite(*platform.ThrustMN) || !finite(*platform.MaxSpeedKMS) ||
		*platform.WeightTons <= 0 || *platform.ThrustMN < 0 || *platform.MaxSpeedKMS < 0 {
		return SpacePlatform{}, errors.New("analysis result has invalid platform metrics")
	}
	return platform, nil
}

func deref(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// Facts are the normalised searchable numbers of one snapshot.
type Facts struct {
	// OutRates and InRates are per-second rates keyed by item name; output sums
	// products and intermediates, input sums ingredients and intermediates.
	OutRates map[string]float64
	InRates  map[string]float64
	// QualityOutRates and QualityInRates are keyed by QualityKey and hold items
	// only: fluids have no quality.
	QualityOutRates map[string]float64
	QualityInRates  map[string]float64
	// Imports are the materials the blueprint needs from outside: every
	// ingredient, and the shortfall of an intermediate that is consumed faster
	// than it is made. Unlike InRates it leaves out what the blueprint makes
	// itself.
	Imports []Import
	// Qualities are the distinct non-empty material qualities, sorted.
	Qualities                   []string
	ElectricityProductionWatts  float64
	ElectricityConsumptionWatts float64
	ElectricityNetWatts         float64
	PollutionNetPerSecond       float64
	// Platform is set only for space platforms.
	Platform *SpacePlatform
	// Footprint is the blueprint's size; nil for a snapshot stored before it
	// was measured.
	Footprint *Footprint
	// RawCost is the build cost's raw resources by name (items and fluids);
	// empty for a snapshot stored without a build cost.
	RawCost map[string]float64
}

// Import is one material (an item has a quality, a fluid has none) the
// blueprint takes from outside, in units per second.
type Import struct {
	Type      string
	Name      string
	Quality   string
	PerSecond float64
}

// FootprintTiles is the footprint's area in tiles; 0 when unmeasured.
func (f Facts) FootprintTiles() float64 {
	if f.Footprint == nil {
		return 0
	}
	return float64(f.Footprint.Width) * float64(f.Footprint.Height)
}

// ConsumptionMW is the electricity consumed (drain included) in megawatts.
func (f Facts) ConsumptionMW() float64 { return f.ElectricityConsumptionWatts / 1e6 }

// QualityKey is the key of Facts.QualityOutRates and QualityInRates.
func QualityKey(quality, name string) string { return quality + "\x00" + name }

// hasQuality reports whether a material row carries a quality: items do,
// fluids never do.
func (row Material) hasQuality() bool { return row.Type != "fluid" && row.Quality != "" }

// Facts derives the search facts. Item names are used exactly as stored.
func (s *Snapshot) Facts() Facts {
	facts := Facts{
		OutRates: map[string]float64{}, InRates: map[string]float64{},
		QualityOutRates: map[string]float64{}, QualityInRates: map[string]float64{},
		ElectricityProductionWatts:  s.ElectricityProductionWatts,
		ElectricityConsumptionWatts: s.ElectricityConsumptionWatts,
		ElectricityNetWatts:         s.ElectricityNetWatts,
		PollutionNetPerSecond:       s.PollutionNetPerSecond,
	}
	qualities := map[string]struct{}{}
	out := func(row Material) {
		facts.OutRates[row.Name] += row.OutputPerSecond
		if row.hasQuality() {
			facts.QualityOutRates[QualityKey(row.Quality, row.Name)] += row.OutputPerSecond
			qualities[row.Quality] = struct{}{}
		}
	}
	in := func(row Material) {
		facts.InRates[row.Name] += row.InputPerSecond
		if row.hasQuality() {
			facts.QualityInRates[QualityKey(row.Quality, row.Name)] += row.InputPerSecond
			qualities[row.Quality] = struct{}{}
		}
	}
	for _, row := range s.Products {
		out(row)
	}
	for _, row := range s.Intermediates {
		out(row)
		in(row)
		if row.DifferencePerSecond < 0 {
			facts.Imports = append(facts.Imports, Import{Type: row.Type, Name: row.Name, Quality: row.Quality, PerSecond: -row.DifferencePerSecond})
		}
	}
	for _, row := range s.Ingredients {
		in(row)
		facts.Imports = append(facts.Imports, Import{Type: row.Type, Name: row.Name, Quality: row.Quality, PerSecond: row.InputPerSecond})
	}
	facts.Qualities = make([]string, 0, len(qualities))
	for quality := range qualities {
		facts.Qualities = append(facts.Qualities, quality)
	}
	sort.Strings(facts.Qualities)
	if s.Platform.IsSpacePlatform {
		platform := s.Platform
		facts.Platform = &platform
	}
	facts.Footprint = s.Footprint
	facts.RawCost = map[string]float64{}
	if s.BuildCost != nil {
		for _, material := range s.BuildCost.Raw {
			facts.RawCost[material.Name] += material.Amount
		}
	}
	return facts
}
