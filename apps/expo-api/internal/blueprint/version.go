package blueprint

import "strconv"

// Factorio packs the exporting game's version into a 64-bit integer as four
// 16-bit groups: main, major, minor, and an internal developer counter. Only
// the first three are meaningful to users.
const versionGroupBits = 16

// formatVersion renders Factorio's packed version integer as the familiar
// dotted form, e.g. 562949954404356 becomes "2.0.15". The trailing developer
// counter is dropped, and a missing version yields an empty string rather than
// a misleading "0.0.0".
func formatVersion(packed uint64) string {
	if packed == 0 {
		return ""
	}
	groups := make([]byte, 0, 12)
	for shift := 3; shift >= 1; shift-- {
		part := (packed >> (uint(shift) * versionGroupBits)) & 0xffff
		if len(groups) > 0 {
			groups = append(groups, '.')
		}
		groups = strconv.AppendUint(groups, part, 10)
	}
	return string(groups)
}

// GameVersion reports the game version that exported a submission, taken from
// its root node. It says nothing about which mods were active.
func GameVersion(node Node) string {
	switch {
	case node.Kind == KindBlueprint:
		return node.Blueprint.Version
	case node.Kind.IsPlanner():
		return node.Planner.Version
	}
	return node.Book.Version
}
