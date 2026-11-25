package blueprint

import (
	"encoding/json"
	"fmt"
)

// LeafDisplay is the display-only metadata of a stored leaf: its parameters
// in source order and its train and space platform schedules, decoded into
// the fields the site shows. Parametrization is not validated; an entry
// that does not fit its type (null, a field of another JSON type) is
// skipped, and fields the types do not name are dropped.
type LeafDisplay struct {
	Parameters []Parameter
	Schedules  []Schedule
}

// Parameter is one blueprint parameter: a signal ("id": an item, fluid or
// other signal, maybe tied to another parameter's recipe) or a number
// ("number": a constant or a formula over variables).
type Parameter struct {
	Type string `json:"type" enum:"id,number" doc:"The parameter's type (id or number)."`
	Name string `json:"name,omitempty" doc:"The parameter's name."`
	// ID is a signal parameter's placeholder signal.
	ID               string                     `json:"id,omitempty" doc:"A signal parameter's placeholder signal."`
	QualityCondition *ParameterQualityCondition `json:"quality-condition,omitempty" doc:"The quality condition of the parameter."`
	// The *-of fields tie a signal to another parameter's recipe.
	IngredientOf      string `json:"ingredient-of,omitempty" doc:"Ties a signal to the recipe of another parameter (as its ingredient)."`
	ItemIngredientOf  string `json:"item-ingredient-of,omitempty" doc:"Ties an item signal to the recipe of another parameter (as its ingredient)."`
	FluidIngredientOf string `json:"fluid-ingredient-of,omitempty" doc:"Ties a fluid signal to the recipe of another parameter (as its ingredient)."`
	ProductOf         string `json:"product-of,omitempty" doc:"Ties a signal to the recipe of another parameter (as its product)."`
	ItemProductOf     string `json:"item-product-of,omitempty" doc:"Ties an item signal to the recipe of another parameter (as its product)."`
	FluidProductOf    string `json:"fluid-product-of,omitempty" doc:"Ties a fluid signal to the recipe of another parameter (as its product)."`
	// Parameter is false for a signal kept as it is (not parametrised).
	Parameter *bool `json:"parameter,omitempty" doc:"False for a signal kept as it is (not parametrised)."`
	// Number is a number parameter's value, as Factorio writes it (a string).
	Number          string `json:"number,omitempty" doc:"A number parameter's value, as Factorio writes it (a string)."`
	Variable        string `json:"variable,omitempty" doc:"The variable the number parameter is named by."`
	Formula         string `json:"formula,omitempty" doc:"The formula of a number parameter."`
	Dependent       bool   `json:"dependent,omitempty" doc:"Whether the parameter depends on another one."`
	NotParametrised bool   `json:"not-parametrised,omitempty" doc:"True for a signal kept as it is, not made a parameter."`
}

// ParameterQualityCondition is a signal parameter's quality condition.
type ParameterQualityCondition struct {
	Quality    string `json:"quality,omitempty" doc:"The quality compared against."`
	Comparator string `json:"comparator,omitempty" doc:"The comparison operator."`
}

// Schedule is one train or space platform schedule.
type Schedule struct {
	// Locomotives are the entity numbers of the train's locomotives.
	Locomotives []int64       `json:"locomotives,omitempty" doc:"The entity numbers of the train's locomotives (a train schedule); empty for a platform schedule."`
	Schedule    *ScheduleBody `json:"schedule,omitempty" doc:"The schedule itself."`
}

// ScheduleBody is a schedule's stops and interrupts.
type ScheduleBody struct {
	Records    []ScheduleStop      `json:"records,omitempty" doc:"The stops of the schedule, in order."`
	Interrupts []ScheduleInterrupt `json:"interrupts,omitempty" doc:"The schedule's interrupts."`
}

// ScheduleStop is one stop: a station or (on a platform) a planet.
type ScheduleStop struct {
	Station   string `json:"station,omitempty" doc:"The station or planet name."`
	Temporary bool   `json:"temporary,omitempty" doc:"Whether the stop is temporary."`
	// AllowsUnloading is absent for the default (unloading allowed).
	AllowsUnloading *bool                   `json:"allows_unloading,omitempty" doc:"Whether the stop allows unloading; absent for the default (it does)."`
	WaitConditions  []ScheduleWaitCondition `json:"wait_conditions,omitempty" doc:"The conditions the train waits for at the stop."`
}

// ScheduleWaitCondition is one wait or interrupt condition; which fields it
// carries depends on Type (blueprint schema 2.1.0).
type ScheduleWaitCondition struct {
	Type        string             `json:"type" doc:"The kind of wait condition."`
	CompareType string             `json:"compare_type,omitempty" enum:"and,or" doc:"How the condition combines with the previous one (and or or)."`
	Ticks       *float64           `json:"ticks,omitempty" doc:"How long to wait, in ticks, for a timed wait."`
	Damage      *float64           `json:"damage,omitempty" doc:"The damage threshold, for a damage wait."`
	Station     string             `json:"station,omitempty" doc:"The station to wait for."`
	Planet      *SchedulePlanet    `json:"planet,omitempty" doc:"The planet to wait for, for a platform wait."`
	Condition   *ScheduleCondition `json:"condition,omitempty" doc:"The circuit condition, for a circuit wait."`
}

// SchedulePlanet names a space location.
type SchedulePlanet struct {
	Name string `json:"name,omitempty" doc:"The planet's name."`
}

// ScheduleCondition compares a signal with another or with a constant.
type ScheduleCondition struct {
	FirstSignal  *ScheduleSignal `json:"first_signal,omitempty" doc:"The signal on the left of the comparison."`
	SecondSignal *ScheduleSignal `json:"second_signal,omitempty" doc:"The signal on the right of the comparison."`
	Comparator   string          `json:"comparator,omitempty" doc:"The comparison operator."`
	Constant     *float64        `json:"constant,omitempty" doc:"The constant the first signal is compared with."`
}

// ScheduleSignal is a signal of a schedule condition.
type ScheduleSignal struct {
	Type    string `json:"type,omitempty" doc:"The signal's type (item, fluid, virtual and so on)."`
	Name    string `json:"name,omitempty" doc:"The signal's name."`
	Quality string `json:"quality,omitempty" doc:"The signal's quality."`
}

// ScheduleInterrupt is a named interrupt: when its conditions hold, the
// train or platform goes to its targets.
type ScheduleInterrupt struct {
	Name            string                  `json:"name,omitempty" doc:"The interrupt's name."`
	Conditions      []ScheduleWaitCondition `json:"conditions,omitempty" doc:"The conditions that trigger the interrupt."`
	Targets         []ScheduleStop          `json:"targets,omitempty" doc:"The stops the interrupt sends the train to."`
	InsideInterrupt bool                    `json:"inside_interrupt,omitempty" doc:"Whether the interrupt can fire during another interrupt."`
}

// DecodeLeafDisplay decompresses a stored leaf exchange string once and reads
// both its parameters and its schedules.
func DecodeLeafDisplay(exchange string, maxDecodedBytes int64) (LeafDisplay, error) {
	raw, err := Decode(exchange, maxDecodedBytes)
	if err != nil {
		return LeafDisplay{}, err
	}
	var payload struct {
		Blueprint struct {
			Parameters json.RawMessage `json:"parameters"`
			Schedules  json.RawMessage `json:"schedules"`
		} `json:"blueprint"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return LeafDisplay{}, fmt.Errorf("decode blueprint parameters and schedules: %w", err)
	}
	return LeafDisplay{
		Parameters: decodeEach[Parameter](payload.Blueprint.Parameters),
		Schedules:  decodeEach[Schedule](payload.Blueprint.Schedules),
	}, nil
}

// decodeEach decodes every element of a JSON array that fits T, skipping
// null and the rest; anything but an array is no list.
func decodeEach[T any](list json.RawMessage) []T {
	var elements []json.RawMessage
	if json.Unmarshal(list, &elements) != nil || elements == nil {
		return nil
	}
	decoded := make([]T, 0, len(elements))
	for _, element := range elements {
		var value *T
		if json.Unmarshal(element, &value) == nil && value != nil {
			decoded = append(decoded, *value)
		}
	}
	return decoded
}
