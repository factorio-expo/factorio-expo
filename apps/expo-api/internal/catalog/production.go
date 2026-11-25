package catalog

import (
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/internal/analysisresult"
)

// A leaf's production availability: its analysis phase state
// (progress.LeafOf), except that a stored snapshot that no longer decodes is
// presented as failed.
const (
	ProductionPending = progress.StatusPending
	ProductionFailed  = progress.StatusFailed
	ProductionReady   = progress.StatusReady
)

// SpacePlatform is the bridge's theoretical platform profile. A nil pointer
// means the blueprint is not a space platform.
type SpacePlatform = analysisresult.SpacePlatform
