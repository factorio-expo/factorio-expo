package blueprint

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Factorio 2.1 compatibility of a blueprint made before 2.1, read structurally.
// 2.1 separates an entity's input and output wire networks, so a design where
// an entity writes to a wire colour it also reads (a belt or inserter reading
// its hand while a circuit condition enables it, the old self-subtraction
// trick) can behave differently. The rule is the 2.1 Circuit Audit mod's
// (github.com/ipost/factorio-mod-2.1-circuit-audit, control.lua `classify`): an
// entity is flagged when its control behaviour has at least one input flag and
// at least one output flag set to true on a shared wire colour. A 2.0
// blueprint carries no per-side networks, so both sides share every colour and
// any such entity is flagged.
//
// A leaf is Compat21Incompatible when some entity is flagged (it may be
// affected: a configuration, not proof of breakage), Compat21Compatible when
// none is, and "" when it was made in 2.1 or the version is unknown.
const (
	Compat21Compatible   = "compatible"
	Compat21Incompatible = "incompatible"
)

// compatInputFlags and compatOutputFlags are the mod's `input_flags` and
// `output_flags`, by the names the 2.0 blueprint format writes. Two input flags
// are spelled differently in blueprints than in the mod's API names:
// `circuit_enabled` (API `circuit_enable_disable`) and `set_recipe` (API
// `circuit_set_recipe`); both spellings are listed.
var compatInputFlags = setOf(
	"circuit_enable_disable", "circuit_enabled", "circuit_condition_enabled",
	"circuit_set_filters", "circuit_set_recipe", "set_recipe", "circuit_set_stack_size",
	"set_filter", "set_requests", "set_priority", "set_trains_limit", "set_research",
	"send_to_train", "send_to_platform", "enable_harvesting_condition",
	"enable_planting_condition", "close_signal", "open_gate",
)

var compatOutputFlags = setOf(
	"circuit_read_contents", "circuit_read_hand_contents", "circuit_read_transfers",
	"circuit_read_ingredients", "circuit_read_resources", "read_contents",
	"read_empty_slots", "read_content", "read_charge", "read_fuel", "read_research_cost",
	"read_technology_level", "read_temperature", "read_robot_stats", "read_logistics",
	"read_sensor", "read_signal", "read_from_train", "read_stopped_train",
	"read_trains_count", "read_moving_from", "read_moving_to", "read_speed",
	"read_damage_taken",
)

func setOf(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// versionBelow21 reports whether a formatted game version ("2.0.15") is known
// and older than 2.1.
func versionBelow21(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major < 2 || (major == 2 && minor < 1)
}

// flagged reports whether one entity's control behaviour has an input flag and
// an output flag both true.
func flagged(behavior map[string]json.RawMessage) bool {
	var input, output bool
	for key, value := range behavior {
		if string(value) != "true" {
			continue
		}
		input = input || compatInputFlags[key]
		output = output || compatOutputFlags[key]
	}
	return input && output
}

// leafCompat21 judges one leaf from its entities' control behaviours.
func leafCompat21(version string, entities []json.RawMessage) string {
	if !versionBelow21(version) {
		return ""
	}
	for _, raw := range entities {
		var entity struct {
			ControlBehavior map[string]json.RawMessage `json:"control_behavior"`
		}
		if err := json.Unmarshal(raw, &entity); err != nil || len(entity.ControlBehavior) == 0 {
			continue
		}
		if flagged(entity.ControlBehavior) {
			return Compat21Incompatible
		}
	}
	return Compat21Compatible
}

// Compat21 judges a whole submission: incompatible when any leaf is, else
// compatible when some leaf predates 2.1, else "".
func Compat21(root Node) string {
	result := ""
	var visit func(Node)
	visit = func(node Node) {
		switch {
		case node.Kind == KindBook:
			for _, child := range node.Book.Children {
				visit(child)
			}
		case node.Kind == KindBlueprint:
			switch node.Blueprint.Compat21 {
			case Compat21Incompatible:
				result = Compat21Incompatible
			case Compat21Compatible:
				if result == "" {
					result = Compat21Compatible
				}
			}
		}
	}
	visit(root)
	return result
}
