package search

import (
	"slices"
	"strconv"
	"strings"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// The release family values the game version filter selects (the
// game_version parameter) and counts.
const (
	versionPre20         = "pre-2.0"
	versionV20           = "v2.0"
	versionV21           = "v2.1"
	versionV21Compatible = "v2.1-compatible"
)

// versionGroups are the release families a blueprint's source game version
// belongs to: pre-2.0 for 0.x and 1.x, else v<major>.<minor> (v2.0, v2.1), and
// v2.1-compatible for a 2.1 blueprint or an older one that blueprint.Compat21
// found compatible with 2.1 ([2.1 compatibility] in docs/architecture.md). A
// version that does not parse has none.
func versionGroups(gameVersion string, compat catalog.Compat21) []string {
	fields := strings.Split(gameVersion, ".")
	if len(fields) < 2 {
		return []string{}
	}
	major, majorErr := strconv.Atoi(fields[0])
	minor, minorErr := strconv.Atoi(fields[1])
	if majorErr != nil || minorErr != nil {
		return []string{}
	}
	var groups []string
	if major < 2 {
		groups = append(groups, versionPre20)
	} else {
		groups = append(groups, "v"+strconv.Itoa(major)+"."+strconv.Itoa(minor))
	}
	if (major == 2 && minor == 1) || compat == catalog.Compat21Compatible {
		groups = append(groups, versionV21Compatible)
	}
	return slices.Compact(groups)
}

// topProduct is the output with the highest rate, the lexically first on a
// tie; "" without outputs.
func topProduct(outRates map[string]float64) string {
	best := ""
	for item, rate := range outRates {
		if best == "" || rate > outRates[best] || (rate == outRates[best] && item < best) {
			best = item
		}
	}
	return best
}
