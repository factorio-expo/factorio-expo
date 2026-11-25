package preview

import (
	"context"
	"net/http"
	"testing"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/internal/runnerapi"
)

// pollPublishing polls up to polls times with a progress hub, stopping once
// something was published, and returns what an entry's stream would have
// received.
func pollPublishing(t *testing.T, row db.ClaimDispatchJobsRow, fake *fakeRunner, polls int) []progress.Change {
	t.Helper()
	row.EntryID = idOf(9)
	hub := progress.NewHub()
	sub := hub.Subscribe(row.EntryID)
	defer sub.Close()
	queries := newFakeQueries(row)
	dispatcher := newDispatcher(queries, queries, queries, fake.client(t), testFiles(t), Options{PollInterval: 3 * time.Second, Progress: hub})
	for range polls {
		if err := dispatcher.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(sub.Changes()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var changes []progress.Change
	for len(sub.Changes()) > 0 {
		changes = append(changes, <-sub.Changes())
	}
	return changes
}

// A final failure reaches the entry's streams with its code, so a page can
// say why (and quick previews can be rejected); details stay internal.
func TestFinalFailurePublishesCode(t *testing.T) {
	t.Parallel()
	changes := pollPublishing(t, freshRow(), &fakeRunner{create: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"code": "invalid_blueprint", "title": "Unprocessable", "detail": "bad string",
		})
	}}, 1)
	blueprintID := idOf(1)
	failed := progress.PhaseState{Status: progress.StatusFailed, Code: "invalid_blueprint"}
	if len(changes) != 2 ||
		changes[0] != (progress.Change{EntryID: idOf(9), BlueprintID: blueprintID, Phase: progress.PhasePreview, State: failed}) ||
		changes[1] != (progress.Change{EntryID: idOf(9), BlueprintID: blueprintID, Phase: progress.PhaseAnalysis, State: failed}) {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestImportPublishesReadyAndWaitingPublishesNothing(t *testing.T) {
	requireEncoder(t)
	t.Parallel()
	image, artifact := pngImage(t)
	changes := pollPublishing(t, inFlightRow(), &fakeRunner{
		image: image,
		get: func(w http.ResponseWriter, _ *http.Request) {
			job := jobResponse(runnerapi.PhaseSucceeded, runnerapi.PhasePending)
			job.Artifact = artifact
			writeJSON(w, http.StatusOK, job)
		},
	}, 500)
	if len(changes) != 1 || changes[0].Phase != progress.PhasePreview ||
		changes[0].State != (progress.PhaseState{Status: progress.StatusReady}) {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestRetryPublishesNothing(t *testing.T) {
	t.Parallel()
	changes := pollPublishing(t, freshRow(), &fakeRunner{create: func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"title": "down"})
	}}, 1)
	if len(changes) != 0 {
		t.Fatalf("a retry leaves the phase pending, got %+v", changes)
	}
}
