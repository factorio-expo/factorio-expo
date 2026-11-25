package progress

import "testing"

func TestLeafOf(t *testing.T) {
	t.Parallel()
	pending := PhaseState{Status: StatusPending}
	ready := PhaseState{Status: StatusReady}
	tests := []struct {
		name  string
		facts LeafFacts
		want  Leaf
	}{
		{"no job yet", LeafFacts{}, Leaf{Preview: pending, Analysis: pending}},
		{"jobs pending", LeafFacts{PreviewJobState: "pending", AnalysisJobState: "pending"},
			Leaf{Preview: pending, Analysis: pending}},
		{"failed jobs keep their codes",
			LeafFacts{PreviewJobState: "failed", PreviewFailureCode: "invalid_blueprint",
				AnalysisJobState: "failed", AnalysisFailureCode: "analysis_failed"},
			Leaf{Preview: PhaseState{Status: StatusFailed, Code: "invalid_blueprint"},
				Analysis: PhaseState{Status: StatusFailed, Code: "analysis_failed"}}},
		{"stored results win over a later failed job",
			LeafFacts{HasPreview: true, PreviewJobState: "failed", PreviewFailureCode: "stale",
				HasProduction: true, AnalysisJobState: "failed", AnalysisFailureCode: "stale"},
			Leaf{Preview: ready, Analysis: ready}},
		{"an open job over a stored result is a re-run",
			LeafFacts{HasPreview: true, PreviewJobState: "pending", HasProduction: true, AnalysisJobState: "pending"},
			Leaf{Preview: pending, Analysis: pending}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.facts.BlueprintID, test.want.BlueprintID = "leaf", "leaf"
			if got := LeafOf(test.facts); got != test.want {
				t.Fatalf("LeafOf(%+v) = %+v, want %+v", test.facts, got, test.want)
			}
		})
	}
}

func TestLeafState(t *testing.T) {
	t.Parallel()
	leaf := LeafOf(LeafFacts{HasPreview: true, AnalysisJobState: "failed", AnalysisFailureCode: "x"})
	if leaf.State(PhasePreview).Status != StatusReady || leaf.State(PhaseAnalysis).Code != "x" ||
		leaf.State("other").Status != StatusPending || leaf.Ready() {
		t.Fatalf("leaf = %+v", leaf)
	}
}
