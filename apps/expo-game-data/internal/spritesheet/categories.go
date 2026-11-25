package spritesheet

import "factorio-expo/apps/expo-game-data/internal/prototypes"

// categoryName is the lookup type a prototype's icon is published under.
func categoryName(prototype prototypes.Prototype) string {
	switch {
	case prototype.Type == "entity", prototype.Type == "recipe", prototype.Type == "fluid",
		prototype.Type == "planet", prototype.Type == "space-location", prototype.Type == "virtual-signal",
		prototype.Type == "quality", prototype.Type == "quality-badge", prototype.Type == "tile",
		prototype.Type == "utility":
		return prototype.Type
	case prototype.Group != "" || isItemType(prototype.Type):
		return "item"
	default:
		return ""
	}
}

// iconOrder groups cells within a sheet; ties sort by name.
func iconOrder(types map[string]struct{}) int {
	for _, prototypeType := range []string{"item", "recipe", "fluid"} {
		if _, ok := types[prototypeType]; ok {
			return 0
		}
	}
	for order, prototypeType := range []string{"virtual-signal", "space-location", "planet"} {
		if _, ok := types[prototypeType]; ok {
			return order + 2
		}
	}
	return 5
}

func prototypeTypeOrder(prototypeType string) int {
	switch prototypeType {
	case "item":
		return 0
	case "entity":
		return 1
	case "recipe":
		return 2
	case "fluid":
		return 3
	case "virtual-signal":
		return 4
	case "space-location":
		return 5
	case "planet":
		return 6
	case "quality":
		return 7
	case "quality-badge":
		return 8
	case "tile":
		return 9
	case "utility":
		return 10
	default:
		return 11
	}
}

func sourcePriority(prototype prototypes.Prototype) int {
	switch categoryName(prototype) {
	case "item":
		return 0
	case "recipe":
		return 1
	default:
		return 2
	}
}

func isItemType(prototypeType string) bool {
	switch prototypeType {
	case "item", "ammo", "armor", "blueprint", "blueprint-book", "capsule",
		"deconstruction-item", "gun", "item-with-entity-data", "module", "rail-planner",
		"repair-tool", "space-platform-starter-pack", "spidertron-remote",
		"tool", "upgrade-item":
		return true
	default:
		return false
	}
}
