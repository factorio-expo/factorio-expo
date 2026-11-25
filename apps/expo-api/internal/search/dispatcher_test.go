package search

import (
	"context"
	"errors"
	"testing"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

type fakeSearchQueries struct {
	rows     []db.EntrySearchJob
	calls    []string
	retry    db.RetrySearchJobParams
	failed   db.MarkSearchJobFailedParams
	released db.ReleaseSearchJobParams
}

func (f *fakeSearchQueries) ClaimSearchJobs(context.Context, db.ClaimSearchJobsParams) ([]db.EntrySearchJob, error) {
	return f.rows, nil
}

func (f *fakeSearchQueries) DeleteSearchJob(context.Context, db.DeleteSearchJobParams) error {
	f.calls = append(f.calls, "delete")
	return nil
}

func (f *fakeSearchQueries) RetrySearchJob(_ context.Context, arg db.RetrySearchJobParams) error {
	f.calls, f.retry = append(f.calls, "retry"), arg
	return nil
}

func (f *fakeSearchQueries) ReleaseSearchJob(_ context.Context, arg db.ReleaseSearchJobParams) error {
	f.calls, f.released = append(f.calls, "release"), arg
	return nil
}

func (f *fakeSearchQueries) MarkSearchJobFailed(_ context.Context, arg db.MarkSearchJobFailedParams) error {
	f.calls, f.failed = append(f.calls, "fail"), arg
	return nil
}

type stubClient struct {
	indexErr error
	indexed  []string
	deleted  []string
}

func (c *stubClient) IndexEntry(_ context.Context, id string) error {
	c.indexed = append(c.indexed, id)
	return c.indexErr
}

func (c *stubClient) DeleteEntry(_ context.Context, id string) error {
	c.deleted = append(c.deleted, id)
	return nil
}

func searchJob(action string, retries int32) db.EntrySearchJob {
	return db.EntrySearchJob{
		EntryID: "000000000007", Action: action, RetryCount: retries,
	}
}

func pollSearch(t *testing.T, queries *fakeSearchQueries, client Indexer) {
	t.Helper()
	if err := newDispatcher(queries, client, Options{}).PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSuccessClearsJobAndRoutesByAction(t *testing.T) {
	t.Parallel()
	queries := &fakeSearchQueries{rows: []db.EntrySearchJob{searchJob(actionIndex, 0), searchJob(actionDelete, 0)}}
	client := &stubClient{}
	pollSearch(t, queries, client)
	if len(client.indexed) != 1 || len(client.deleted) != 1 || len(queries.calls) != 2 ||
		queries.calls[0] != "delete" || queries.calls[1] != "delete" {
		t.Fatalf("indexed %v deleted %v calls %v", client.indexed, client.deleted, queries.calls)
	}
}

func TestSearchFailureRetriesWithBackoffAndKeepsDetail(t *testing.T) {
	t.Parallel()
	queries := &fakeSearchQueries{rows: []db.EntrySearchJob{searchJob(actionIndex, 1)}}
	before := time.Now()
	pollSearch(t, queries, &stubClient{indexErr: errors.New("typesense down")})
	if len(queries.calls) != 1 || queries.calls[0] != "retry" ||
		queries.retry.FailureCode != "sync_failed" || queries.retry.FailureDetail != "typesense down" {
		t.Fatalf("calls %v retry %+v", queries.calls, queries.retry)
	}
	// Second retry: 10s backoff.
	if at := queries.retry.AvailableAt.Time; at.Before(before.Add(9*time.Second)) || at.After(before.Add(12*time.Second)) {
		t.Fatalf("available_at = %v, want about 10s ahead of %v", at, before)
	}
}

func TestSearchFailureFailsAtBudget(t *testing.T) {
	t.Parallel()
	queries := &fakeSearchQueries{rows: []db.EntrySearchJob{searchJob(actionIndex, maxRetries)}}
	pollSearch(t, queries, &stubClient{indexErr: errors.New("typesense down")})
	if len(queries.calls) != 1 || queries.calls[0] != "fail" || queries.failed.FailureDetail != "typesense down" {
		t.Fatalf("calls %v failed %+v", queries.calls, queries.failed)
	}
}
