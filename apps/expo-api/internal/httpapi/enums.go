package httpapi

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/progress"
)

// The API's enums are named Go string types (catalog.Visibility,
// blueprint.Kind, ...). Huma documents a named string as an inline string, so
// each enum is registered here once as a component schema and every field of
// the type refers to it: one definition in the document, one named type in the
// generated TypeScript.

// enumSchema registers the enum component name once and returns a reference to
// it.
func enumSchema(r huma.Registry, name, description string, values ...string) *huma.Schema {
	if _, ok := r.Map()[name]; !ok {
		enum := make([]any, len(values))
		for i, value := range values {
			enum[i] = value
		}
		r.Map()[name] = &huma.Schema{Type: huma.TypeString, Description: description, Enum: enum}
	}
	return &huma.Schema{Ref: "#/components/schemas/" + name}
}

type visibilitySchema string

func (visibilitySchema) Schema(r huma.Registry) *huma.Schema {
	return enumSchema(r, "Visibility",
		"Who may see an entry: public (listed and searchable), unlisted (anyone with the link) or private (the owner only).",
		string(catalog.VisibilityPublic), string(catalog.VisibilityUnlisted), string(catalog.VisibilityPrivate))
}

type compat21Schema string

func (compat21Schema) Schema(r huma.Registry) *huma.Schema {
	return enumSchema(r, "Compat21",
		"The Factorio 2.1 compatibility of a submission made before 2.1; empty for one made in 2.1.",
		"", string(catalog.Compat21Compatible), string(catalog.Compat21Incompatible))
}

type nodeKindSchema string

func (nodeKindSchema) Schema(r huma.Registry) *huma.Schema {
	return enumSchema(r, "NodeKind",
		"What a tree node is: a book, a blueprint, or an upgrade or deconstruction planner.",
		string(blueprint.KindBook), string(blueprint.KindBlueprint),
		string(blueprint.KindUpgradePlanner), string(blueprint.KindDeconstructionPlanner))
}

type phaseStatusSchema string

func (phaseStatusSchema) Schema(r huma.Registry) *huma.Schema {
	return enumSchema(r, "PhaseStatus",
		"The state of a preview or analysis: pending (queued, running or retrying), ready or failed.",
		string(progress.StatusPending), string(progress.StatusReady), string(progress.StatusFailed))
}

type boardSchema string

func (boardSchema) Schema(r huma.Registry) *huma.Schema {
	values := make([]string, len(catalog.Boards))
	for i, board := range catalog.Boards {
		values[i] = string(board)
	}
	return enumSchema(r, "Board",
		"A leaderboard board. Ships: the fastest, the highest thrust-to-weight, the lightest, the most cargo bays. "+
			"Factories: the most science, the densest and the cleanest science, the biggest power plant.",
		values...)
}

type boardGroupSchema string

func (boardGroupSchema) Schema(r huma.Registry) *huma.Schema {
	values := make([]string, len(catalog.BoardGroups))
	for i, group := range catalog.BoardGroups {
		values[i] = string(group)
	}
	return enumSchema(r, "BoardGroup",
		"A leaderboard group: ships (space platforms that move) or factories (blueprints that are not space platforms).",
		values...)
}

type xpReasonSchema string

func (xpReasonSchema) Schema(r huma.Registry) *huma.Schema {
	reasons := catalog.XPReasons()
	values := make([]string, len(reasons))
	for i, reason := range reasons {
		values[i] = string(reason)
	}
	return enumSchema(r, "XPReason",
		"What earns XP: an upload, a favorite or collection add received, a comment, a record, a challenge place or taking part.",
		values...)
}

type badgeSchema string

func (badgeSchema) Schema(r huma.Registry) *huma.Schema {
	values := make([]string, len(catalog.Badges))
	for i, badge := range catalog.Badges {
		values[i] = string(badge)
	}
	return enumSchema(r, "Badge", "An achievement a profile shows.", values...)
}

// registerEnums points each named enum type at its component schema.
func registerEnums(registry huma.Registry) {
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.Visibility](), reflect.TypeFor[visibilitySchema]())
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.Compat21](), reflect.TypeFor[compat21Schema]())
	registry.RegisterTypeAlias(reflect.TypeFor[blueprint.Kind](), reflect.TypeFor[nodeKindSchema]())
	registry.RegisterTypeAlias(reflect.TypeFor[progress.Status](), reflect.TypeFor[phaseStatusSchema]())
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.Board](), reflect.TypeFor[boardSchema]())
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.Badge](), reflect.TypeFor[badgeSchema]())
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.BoardGroup](), reflect.TypeFor[boardGroupSchema]())
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.XPReason](), reflect.TypeFor[xpReasonSchema]())
}
