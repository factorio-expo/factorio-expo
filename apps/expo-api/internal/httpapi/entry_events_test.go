package httpapi_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/progress"
)

// sseEvent is one parsed text/event-stream event.
type sseEvent struct {
	name, data string
}

// sseReader parses events off a live stream, skipping comments (heartbeats)
// and the retry field.
type sseReader struct {
	t       *testing.T
	scanner *bufio.Scanner
}

func (r sseReader) next() (sseEvent, bool) {
	r.t.Helper()
	var event sseEvent
	for r.scanner.Scan() {
		line := r.scanner.Text()
		switch {
		case line == "":
			if event.name != "" {
				return event, true
			}
		case strings.HasPrefix(line, "event: "):
			event.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			event.data = strings.TrimPrefix(line, "data: ")
		}
	}
	return event, false
}

func (r sseReader) expect(name string, into any) {
	r.t.Helper()
	event, ok := r.next()
	if !ok || event.name != name {
		r.t.Fatalf("event = %+v (ok %v), want %q", event, ok, name)
	}
	if into != nil {
		if err := json.Unmarshal([]byte(event.data), into); err != nil {
			r.t.Fatalf("decode %s %q: %v", name, event.data, err)
		}
	}
}

func openEvents(t *testing.T, server *httptest.Server, entryID, token string) (*http.Response, sseReader) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/entries/"+entryID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response, sseReader{t: t, scanner: bufio.NewScanner(response.Body)}
}

func createEventsEntry(t *testing.T, handler http.Handler, token, visibility string) catalog.Entry {
	t.Helper()
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"blueprint": string(exchange), "visibility": visibility})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/entries", token, body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create entry status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(recorder.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

// The stream opens with a snapshot of the live revision, sends one phase
// event per settled phase (failures with their code), then done, and ends.
// A late subscriber gets a done snapshot and done straight away.
func TestEntryEventsStreamPhasesUntilDone(t *testing.T) {
	handler, dispatcher := newTestAPI(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	entry := createEventsEntry(t, handler, "", "")

	response, events := openEvents(t, server, entry.ID, "")
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "text/event-stream" ||
		response.Header.Get("Cache-Control") != "no-cache, no-transform" ||
		response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("status %d, headers %v", response.StatusCode, response.Header)
	}
	var snapshot progress.Snapshot
	events.expect("snapshot", &snapshot)
	pending := progress.PhaseState{Status: progress.StatusPending}
	if snapshot.EntryID != entry.ID || snapshot.RevisionID != entry.Revision.ID || snapshot.Done ||
		len(snapshot.Leaves) != 1 || snapshot.Leaves[0].Preview != pending || snapshot.Leaves[0].Analysis != pending {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	leafID := snapshot.Leaves[0].BlueprintID

	pollUntilImported(t, dispatcher)
	got := map[string]progress.Change{}
	for range 2 {
		var change progress.Change
		events.expect("phase", &change)
		if change.BlueprintID != leafID {
			t.Fatalf("change for another leaf: %+v", change)
		}
		got[change.Phase] = change
	}
	if got[progress.PhasePreview].State != (progress.PhaseState{Status: progress.StatusReady}) ||
		got[progress.PhaseAnalysis].State != (progress.PhaseState{Status: progress.StatusFailed, Code: "analysis_failed"}) {
		t.Fatalf("changes = %+v", got)
	}
	var done map[string]string
	events.expect("done", &done)
	if done["entry_id"] != entry.ID || done["revision_id"] != entry.Revision.ID {
		t.Fatalf("done = %v", done)
	}
	if event, ok := events.next(); ok {
		t.Fatalf("stream continued after done: %+v", event)
	}

	_, late := openEvents(t, server, entry.ID, "")
	late.expect("snapshot", &snapshot)
	if !snapshot.Done || snapshot.Leaves[0].Analysis.Code != "analysis_failed" {
		t.Fatalf("late snapshot = %+v", snapshot)
	}
	late.expect("done", nil)
}

// Streams follow the entry read rule: unknown and private entries are 404 to
// anyone but the owner, and an open stream says `gone` once the entry is
// deleted.
func TestEntryEventsFollowEntryAccess(t *testing.T) {
	handler, _ := newTestAPI(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	suffix := time.Now().Format("150405.000000")
	suffix = strings.ReplaceAll(suffix, ".", "")
	token, _ := registerTestUser(t, handler, "events"+suffix)

	for _, id := range []string{"not-an-id", "000000000000"} {
		if response, _ := openEvents(t, server, id, ""); response.StatusCode != http.StatusNotFound {
			t.Fatalf("events for %s = %d, want 404", id, response.StatusCode)
		}
	}

	private := createEventsEntry(t, handler, token, "private")
	if response, _ := openEvents(t, server, private.ID, ""); response.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous events for a private entry = %d, want 404", response.StatusCode)
	}
	response, events := openEvents(t, server, private.ID, token)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owner events = %d", response.StatusCode)
	}
	events.expect("snapshot", nil)

	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, authedRequest(http.MethodDelete, "/api/v1/entries/"+private.ID, token, nil))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", deleted.Code)
	}
	var gone map[string]string
	events.expect("gone", &gone)
	if gone["entry_id"] != private.ID {
		t.Fatalf("gone = %v", gone)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
}
