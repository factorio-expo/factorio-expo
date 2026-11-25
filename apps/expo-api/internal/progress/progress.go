// Package progress is the entry-processing progress model behind the
// `GET /api/v1/entries/{id}/events` stream: the per-leaf phase states a client
// sees, and an in-process Hub that tells open streams when a phase settled.
// Production runs one API process, so the hub is memory
// only; streams re-read PostgreSQL on connect and periodically, so a missed
// signal only delays an update.
package progress

// Phase names, matching the runner phases.
const (
	PhasePreview  = "preview"
	PhaseAnalysis = "analysis"
)

// Phase states. Pending covers queued, running and retrying work.
const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

// Status is the state of one phase (the "PhaseStatus" schema of the API).
type Status string

// PhaseState is one phase of one leaf. Code is the failure code of a failed
// phase; failure details stay internal (they can name runner jobs).
type PhaseState struct {
	Status Status `json:"status" doc:"The phase's state: pending (queued, running or retrying), ready or failed."`
	Code   string `json:"code,omitempty" doc:"The failure code of a failed phase."`
}

// Settled reports whether the phase reached a final state.
func (p PhaseState) Settled() bool {
	return p.Status == StatusReady || p.Status == StatusFailed
}

// Leaf is the progress of one leaf blueprint.
type Leaf struct {
	BlueprintID string     `json:"blueprint_id" doc:"The blueprint's id."`
	Preview     PhaseState `json:"preview" doc:"The preview phase."`
	Analysis    PhaseState `json:"analysis" doc:"The analysis phase."`
}

// Settled reports whether both phases are final.
func (l Leaf) Settled() bool { return l.Preview.Settled() && l.Analysis.Settled() }

// phase returns a pointer to the named phase, or nil for an unknown name.
func (l *Leaf) phase(name string) *PhaseState {
	switch name {
	case PhasePreview:
		return &l.Preview
	case PhaseAnalysis:
		return &l.Analysis
	}
	return nil
}

// Snapshot is the progress of an entry's live revision: the `snapshot` event.
type Snapshot struct {
	EntryID    string `json:"entry_id" doc:"The entry's id."`
	RevisionID string `json:"revision_id,omitempty" doc:"The live revision's id."`
	Leaves     []Leaf `json:"leaves" doc:"The progress of each blueprint of the live revision."`
	// Done is true once every leaf is settled (an entry without leaves is done).
	Done bool `json:"done" doc:"True once every leaf is settled (an entry without leaves is done)."`
}

// Settle recomputes Done from the leaves.
func (s *Snapshot) Settle() {
	s.Done = true
	for _, leaf := range s.Leaves {
		if !leaf.Settled() {
			s.Done = false
			return
		}
	}
}

// Change is what the hub carries. A phase change names the leaf, phase and
// its new state; a Change with an empty Phase only says "the entry changed"
// (new or deleted revision, deleted entry) and makes streams re-read it.
type Change struct {
	EntryID     string     `json:"-"`
	BlueprintID string     `json:"blueprint_id" doc:"The blueprint whose phase changed."`
	Phase       string     `json:"phase" enum:"preview,analysis" doc:"preview or analysis."`
	State       PhaseState `json:"state" doc:"The phase's new state."`
}

// Apply applies a phase change to the snapshot. It reports whether the
// change is about one of its leaves and alters that leaf's state.
func (s *Snapshot) Apply(change Change) bool {
	for i := range s.Leaves {
		if s.Leaves[i].BlueprintID != change.BlueprintID {
			continue
		}
		phase := s.Leaves[i].phase(change.Phase)
		if phase == nil || *phase == change.State {
			return false
		}
		*phase = change.State
		s.Settle()
		return true
	}
	return false
}

// Diff lists the phase changes that turn s into next. Both must describe the
// same revision; callers send a whole snapshot when the revision changed.
func (s Snapshot) Diff(next Snapshot) []Change {
	previous := make(map[string]Leaf, len(s.Leaves))
	for _, leaf := range s.Leaves {
		previous[leaf.BlueprintID] = leaf
	}
	var changes []Change
	for _, leaf := range next.Leaves {
		old := previous[leaf.BlueprintID]
		if old.Preview != leaf.Preview {
			changes = append(changes, Change{
				EntryID: next.EntryID, BlueprintID: leaf.BlueprintID, Phase: PhasePreview, State: leaf.Preview,
			})
		}
		if old.Analysis != leaf.Analysis {
			changes = append(changes, Change{
				EntryID: next.EntryID, BlueprintID: leaf.BlueprintID, Phase: PhaseAnalysis, State: leaf.Analysis,
			})
		}
	}
	return changes
}
