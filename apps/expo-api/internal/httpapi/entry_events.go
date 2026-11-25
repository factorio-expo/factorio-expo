package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/internal/shortid"
)

// Stream timings. Heartbeats keep proxies and CDNs (idle cutoffs)
// from closing a quiet stream; the resync re-read covers a missed hub signal;
// the lifetime bound makes clients reconnect (and re-snapshot) now and then.
const (
	eventsHeartbeat    = 20 * time.Second
	eventsResync       = 30 * time.Second
	eventsMaxLifetime  = 30 * time.Minute
	eventsWriteTimeout = 10 * time.Second
	// eventsRetryMillis is the reconnect delay the stream suggests to EventSource.
	eventsRetryMillis = 5000
)

// eventStream writes one text/event-stream response.
type eventStream struct {
	w          http.ResponseWriter
	controller *http.ResponseController
}

func (s eventStream) write(text string) error {
	// The server's WriteTimeout would cut the stream; bound each write instead.
	_ = s.controller.SetWriteDeadline(time.Now().Add(eventsWriteTimeout))
	if _, err := fmt.Fprint(s.w, text); err != nil {
		return err
	}
	return s.controller.Flush()
}

func (s eventStream) send(event string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.write("event: " + event + "\ndata: " + string(body) + "\n\n")
}

// streamEntryEvents streams the live revision's per-leaf phase changes
// (docs/api.md "Entry progress events"): a snapshot on connect, a `phase`
// event per change, a fresh `snapshot` when the revision changes, `done` once
// every leaf is settled and `gone` when the entry is deleted or hidden.
// Errors before the stream starts answer as usual.
func (s *server) streamEntryEvents(_ context.Context, in *EntryPath) (*huma.StreamResponse, error) {
	return &huma.StreamResponse{Body: func(ctx huma.Context) {
		r, w := humago.Unwrap(ctx)
		s.serveEntryEvents(w, r, in.EntryID)
	}}, nil
}

// The events of an entry's stream, documented as the 200 response.
type (
	entryDoneEvent struct {
		EntryID    string `json:"entry_id" doc:"The entry's id."`
		RevisionID string `json:"revision_id" doc:"The revision the progress is about."`
	}
	entryGoneEvent struct {
		EntryID string `json:"entry_id" doc:"The entry that is gone (deleted, expired or hidden)."`
	}
)

// entryEventTypes are the stream's events and their data.
var entryEventTypes = map[string]any{
	"snapshot": progress.Snapshot{},
	"phase":    progress.Change{},
	"done":     entryDoneEvent{},
	"gone":     entryGoneEvent{},
}

func (s *server) serveEntryEvents(w http.ResponseWriter, r *http.Request, entryID string) {
	key := s.clientKey(r)
	if !s.streams.acquire(key) {
		w.Header().Set("Retry-After", "30")
		httpError(w, "too many open event streams", http.StatusTooManyRequests)
		return
	}
	defer s.streams.release(key)

	viewer := viewerID(r)
	if !shortid.Valid(entryID) {
		writeError(w, catalog.ErrNotFound)
		return
	}
	// Subscribe before the first read, so a change in between is not lost.
	sub := s.progress.Subscribe(entryID)
	defer sub.Close()
	current, err := s.catalog.EntryProgress(r.Context(), entryID, viewer)
	if err != nil {
		writeError(w, err)
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-transform")
	// Proxies that honour it (nginx-style) must not buffer the stream.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	stream := eventStream{w: w, controller: http.NewResponseController(w)}
	if stream.write(fmt.Sprintf("retry: %d\n\n", eventsRetryMillis)) != nil ||
		stream.send("snapshot", current) != nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), eventsMaxLifetime)
	defer cancel()
	heartbeat := time.NewTicker(eventsHeartbeat)
	defer heartbeat.Stop()
	resync := time.NewTicker(eventsResync)
	defer resync.Stop()
	for !current.Done {
		var err error
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			err = stream.write(": ping\n\n")
		case <-resync.C:
			current, err = s.reloadProgress(ctx, stream, current, entryID, viewer)
		case <-sub.Resync():
			current, err = s.reloadProgress(ctx, stream, current, entryID, viewer)
		case change := <-sub.Changes():
			if change.Phase == "" {
				current, err = s.reloadProgress(ctx, stream, current, entryID, viewer)
			} else if current.Apply(change) {
				err = stream.send("phase", change)
			}
		}
		if err != nil {
			if !errors.Is(err, errStreamEnded) && ctx.Err() == nil {
				slog.Warn("entry event stream", "entry_id", current.EntryID, "error", err)
			}
			return
		}
	}
	_ = stream.send("done", entryDoneEvent{EntryID: current.EntryID, RevisionID: current.RevisionID})
}

// errStreamEnded ends a stream that has said its last word (`gone`).
var errStreamEnded = errors.New("stream ended")

// reloadProgress re-reads the entry and sends what changed: phase events for
// the same revision, a whole snapshot for another one, `gone` when the entry
// is no longer viewable.
func (s *server) reloadProgress(
	ctx context.Context, stream eventStream, current progress.Snapshot, entryID, viewer string,
) (progress.Snapshot, error) {
	next, err := s.catalog.EntryProgress(ctx, entryID, viewer)
	if errors.Is(err, catalog.ErrNotFound) {
		if err := stream.send("gone", entryGoneEvent{EntryID: current.EntryID}); err != nil {
			return current, err
		}
		return current, errStreamEnded
	}
	if err != nil {
		// A database hiccup: keep the stream and try again on the next resync.
		slog.Warn("reload entry progress", "entry_id", current.EntryID, "error", err)
		return current, nil
	}
	if next.RevisionID != current.RevisionID {
		return next, stream.send("snapshot", next)
	}
	for _, change := range current.Diff(next) {
		if err := stream.send("phase", change); err != nil {
			return current, err
		}
	}
	return next, nil
}

// registerEntryEvents registers this file's operations.
func (s *server) registerEntryEvents(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "streamEntryEvents", Method: http.MethodGet, Path: "/entries/{entryID}/events",
		Tags:    []string{"Entries"},
		Summary: "Live render and analysis progress of an entry",
		Description: "A server-sent event stream: a snapshot on connect, a phase event per change, a " +
			"fresh snapshot when the revision changes, done once every leaf is settled and gone " +
			"when the entry is deleted or hidden.",
		Responses: eventStreamResponse(api, entryEventTypes),
		Errors:    []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.streamEntryEvents)
}
