package progress

import (
	"encoding/json"
	"testing"
)

func pending() PhaseState { return PhaseState{Status: StatusPending} }

func TestSnapshotJSONShape(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{EntryID: "e", RevisionID: "r", Leaves: []Leaf{{
		BlueprintID: "b", Preview: PhaseState{Status: StatusReady},
		Analysis: PhaseState{Status: StatusFailed, Code: "analysis_failed"},
	}}}
	snapshot.Settle()
	got, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"entry_id":"e","revision_id":"r","leaves":[{"blueprint_id":"b",` +
		`"preview":{"status":"ready"},"analysis":{"status":"failed","code":"analysis_failed"}}],"done":true}`
	if string(got) != want {
		t.Fatalf("snapshot JSON\n got %s\nwant %s", got, want)
	}

	change, err := json.Marshal(Change{
		EntryID: "e", BlueprintID: "b", Phase: PhasePreview,
		State: PhaseState{Status: StatusFailed, Code: "render_failed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"blueprint_id":"b","phase":"preview","state":{"status":"failed","code":"render_failed"}}`; string(change) != want {
		t.Fatalf("change JSON\n got %s\nwant %s", change, want)
	}
}

func TestSnapshotEmptyIsDone(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{EntryID: "e", Leaves: []Leaf{}}
	snapshot.Settle()
	if !snapshot.Done {
		t.Fatal("an entry without leaves has nothing to wait for")
	}
}

func TestSnapshotApplySettles(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{EntryID: "e", Leaves: []Leaf{
		{BlueprintID: "a", Preview: pending(), Analysis: PhaseState{Status: StatusReady}},
	}}
	snapshot.Settle()
	if snapshot.Done {
		t.Fatal("a pending preview is not done")
	}
	ready := Change{BlueprintID: "a", Phase: PhasePreview, State: PhaseState{Status: StatusReady}}
	if snapshot.Apply(Change{BlueprintID: "other", Phase: PhasePreview, State: ready.State}) {
		t.Fatal("a change for another leaf must not apply")
	}
	if snapshot.Apply(Change{BlueprintID: "a", Phase: "bogus", State: ready.State}) {
		t.Fatal("an unknown phase must not apply")
	}
	if !snapshot.Apply(ready) || !snapshot.Done {
		t.Fatalf("apply ready: %+v", snapshot)
	}
	if snapshot.Apply(ready) {
		t.Fatal("a repeated change is not a change")
	}
}

func TestSnapshotDiff(t *testing.T) {
	t.Parallel()
	before := Snapshot{EntryID: "e", Leaves: []Leaf{
		{BlueprintID: "a", Preview: pending(), Analysis: pending()},
		{BlueprintID: "b", Preview: pending(), Analysis: pending()},
	}}
	after := Snapshot{EntryID: "e", Leaves: []Leaf{
		{BlueprintID: "a", Preview: PhaseState{Status: StatusReady}, Analysis: pending()},
		{BlueprintID: "b", Preview: pending(), Analysis: PhaseState{Status: StatusFailed, Code: "x"}},
	}}
	changes := before.Diff(after)
	if len(changes) != 2 ||
		changes[0] != (Change{EntryID: "e", BlueprintID: "a", Phase: PhasePreview, State: PhaseState{Status: StatusReady}}) ||
		changes[1] != (Change{EntryID: "e", BlueprintID: "b", Phase: PhaseAnalysis, State: PhaseState{Status: StatusFailed, Code: "x"}}) {
		t.Fatalf("diff = %+v", changes)
	}
}

func TestHubDeliversToEntrySubscribersOnly(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	mine, other := hub.Subscribe("e1"), hub.Subscribe("e2")
	defer other.Close()
	change := Change{EntryID: "e1", BlueprintID: "b", Phase: PhasePreview, State: PhaseState{Status: StatusReady}}
	hub.Publish(change)
	if got := <-mine.Changes(); got != change {
		t.Fatalf("got %+v", got)
	}
	select {
	case got := <-other.Changes():
		t.Fatalf("another entry's stream got %+v", got)
	default:
	}
	mine.Close()
	mine.Close()
	hub.Publish(change)
	if len(hub.subs["e1"]) != 0 {
		t.Fatal("closed subscription still registered")
	}
}

func TestHubLaggingSubscriberResyncs(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	sub := hub.Subscribe("e")
	defer sub.Close()
	for range subscriberBuffer + 5 {
		hub.EntryChanged("e")
	}
	select {
	case <-sub.Resync():
	default:
		t.Fatal("a subscriber that lagged must be asked to re-read")
	}
	if len(sub.Changes()) != subscriberBuffer {
		t.Fatalf("buffered %d changes", len(sub.Changes()))
	}
}

func TestNilHubIsSafe(t *testing.T) {
	t.Parallel()
	var hub *Hub
	hub.Publish(Change{EntryID: "e"})
	sub := hub.Subscribe("e")
	sub.Close()
}
