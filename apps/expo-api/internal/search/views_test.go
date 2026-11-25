package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// fakeViewCounts is entry_view_counts: rows whose views differ from what was
// last sent.
type fakeViewCounts struct {
	rows   []db.ListUnindexedViewCountsRow
	marked map[string]int64
}

func (f *fakeViewCounts) ListUnindexedViewCounts(_ context.Context, limit int32) ([]db.ListUnindexedViewCountsRow, error) {
	var out []db.ListUnindexedViewCountsRow
	for _, row := range f.rows {
		if f.marked[row.EntryID] != row.Views && len(out) < int(limit) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeViewCounts) MarkViewCountsIndexed(_ context.Context, arg db.MarkViewCountsIndexedParams) error {
	for i, id := range arg.EntryIds {
		f.marked[id] = arg.Views[i]
	}
	return nil
}

type fakeViewIndex struct{ sent []viewCount }

func (f *fakeViewIndex) updateViewCounts(_ context.Context, counts []viewCount) error {
	f.sent = append(f.sent, counts...)
	return nil
}

func TestRefreshViewsSendsSearchableCountsAndMarksAll(t *testing.T) {
	queries := &fakeViewCounts{marked: map[string]int64{}}
	for i := range viewBatch + 2 {
		queries.rows = append(queries.rows, db.ListUnindexedViewCountsRow{
			EntryID: fmt.Sprintf("%012d", i), Views: int64(i + 1), Searchable: i%2 == 0,
		})
	}
	index := &fakeViewIndex{}
	if err := refreshViews(context.Background(), queries, index); err != nil {
		t.Fatal(err)
	}
	// Every row is marked, across more than one batch; only searchable ones
	// reach the index.
	if len(queries.marked) != viewBatch+2 {
		t.Fatalf("marked %d rows, want %d", len(queries.marked), viewBatch+2)
	}
	if len(index.sent) != (viewBatch+2)/2 {
		t.Fatalf("sent %d counts, want %d", len(index.sent), (viewBatch+2)/2)
	}
	if index.sent[1] != (viewCount{ID: "000000000002", ViewCount: 3}) {
		t.Fatalf("sent[1] = %+v", index.sent[1])
	}

	// Nothing changed: nothing is sent again.
	index.sent = nil
	if err := refreshViews(context.Background(), queries, index); err != nil || len(index.sent) != 0 {
		t.Fatalf("second refresh sent %v, %v", index.sent, err)
	}
}

func TestUpdateViewCountsPatchesEveryDocumentOfEachEntry(t *testing.T) {
	var patches []string
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPatch || !strings.HasPrefix(r.URL.Path, "/collections/blueprints_") {
			t.Errorf("%s %s, want a patch of the blueprints collection", r.Method, r.URL.Path)
		}
		patches = append(patches, r.URL.Query().Get("filter_by")+" "+string(body))
		w.WriteHeader(status)
		io.WriteString(w, `{"num_updated":1}`)
	}))
	defer server.Close()
	client := NewTypesenseClient(server.URL, "key", server.Client())

	counts := []viewCount{{ID: "aaaaaaaaaaaa", ViewCount: 7}, {ID: "bbbbbbbbbbbb", ViewCount: 1}}
	if err := client.updateViewCounts(context.Background(), counts); err != nil {
		t.Fatal(err)
	}
	wantPatches := []string{`entry_id:=aaaaaaaaaaaa {"view_count":7}`, `entry_id:=bbbbbbbbbbbb {"view_count":1}`}
	if !reflect.DeepEqual(patches, wantPatches) {
		t.Fatalf("patches = %q, want %q", patches, wantPatches)
	}

	status = http.StatusBadRequest
	if err := client.updateViewCounts(context.Background(), counts); err == nil {
		t.Fatal("a failed patch was ignored")
	}
}
