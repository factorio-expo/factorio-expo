package progress

// LeafFacts is what is stored about one leaf's phases (the catalog reads
// them from the leaf_progress view). An empty job state means no job row.
type LeafFacts struct {
	BlueprintID string
	// HasPreview: a previews row exists.
	HasPreview         bool
	PreviewJobState    string
	PreviewFailureCode string
	// HasProduction: a production snapshot is stored (it may be stale).
	HasProduction       bool
	AnalysisJobState    string
	AnalysisFailureCode string
}

// LeafOf is the one rule for a leaf's phase states, used by the leaf detail,
// the revision tree, progress snapshots and events, and the publish check.
// The stored result and the latest job are separate facts: an open job is
// pending (also over a stored result, for a re-run), a stored result is
// otherwise ready (whatever a later job did, so a failed re-run keeps the old
// result), a job that failed for good without a result is failed with its
// code, and no result nor job is pending. Readers show a stored result
// whatever the status.
func LeafOf(facts LeafFacts) Leaf {
	return Leaf{
		BlueprintID: facts.BlueprintID,
		Preview:     phaseOf(facts.HasPreview, facts.PreviewJobState, facts.PreviewFailureCode),
		Analysis:    phaseOf(facts.HasProduction, facts.AnalysisJobState, facts.AnalysisFailureCode),
	}
}

func phaseOf(stored bool, jobState, failureCode string) PhaseState {
	switch {
	case jobState == "pending":
		return PhaseState{Status: StatusPending}
	case stored:
		return PhaseState{Status: StatusReady}
	case jobState == "failed":
		return PhaseState{Status: StatusFailed, Code: failureCode}
	}
	return PhaseState{Status: StatusPending}
}

// State returns the named phase's state (pending for an unknown name).
func (l Leaf) State(phase string) PhaseState {
	if state := l.phase(phase); state != nil {
		return *state
	}
	return PhaseState{Status: StatusPending}
}

// Ready reports whether both phases are ready.
func (l Leaf) Ready() bool {
	return l.Preview.Status == StatusReady && l.Analysis.Status == StatusReady
}
