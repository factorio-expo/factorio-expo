package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/search"
)

// staleIndex answers every search with hits for the given entries, as an index
// that has not caught up with a visibility change yet would.
type staleIndex struct {
	search.Client
	entryIDs []string
}

func (s *staleIndex) Search(context.Context, search.Query) (search.Result, error) {
	hits := make([]search.Hit, 0, len(s.entryIDs))
	for _, id := range s.entryIDs {
		hits = append(hits, search.Hit{EntryID: id, Title: "Indexed title"})
	}
	return search.Result{Total: int64(len(hits)), Hits: hits}, nil
}

var visibilities = []string{"public", "unlisted", "private"}

// Every change between the three visibilities, made by the owner through
// PATCH, takes effect at once on every way an entry can be reached: direct
// reads, the owner's public profile, search results, other users' favorites
// and the search outbox.
func TestVisibilityTransitions(t *testing.T) {
	for _, from := range visibilities {
		for _, to := range visibilities {
			if from == to {
				continue
			}
			t.Run(from+" to "+to, func(t *testing.T) { checkVisibilityTransition(t, from, to) })
		}
	}
}

func checkVisibilityTransition(t *testing.T, from, to string) {
	truncateSocialFixtures(t)
	index := &staleIndex{}
	var pool *pgxpool.Pool
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Search = index
		pool = deps.Pool
	})
	ownerToken, _ := registerTestUser(t, handler, "vis-owner")
	otherToken, _ := registerTestUser(t, handler, "vis-other")
	entry := createEntryWithVisibility(t, handler, ownerToken, from)
	index.entryIDs = []string{entry.ID}
	base := "/api/v1/entries/" + entry.ID

	// Another user favorites it while they still can see it.
	if from != "private" {
		if code := serve(handler, authedRequest(http.MethodPost, base+"/favorite", otherToken, nil)).Code; code != http.StatusNoContent && code != http.StatusOK {
			t.Fatalf("favorite = %d", code)
		}
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}

	patch := serve(handler, authedRequest(http.MethodPatch, base, ownerToken, []byte(`{"visibility":"`+to+`"}`)))
	if patch.Code != http.StatusOK {
		t.Fatalf("owner PATCH visibility = %d, %s", patch.Code, patch.Body)
	}
	var updated catalog.Entry
	if err := json.Unmarshal(patch.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if string(updated.Visibility) != to {
		t.Fatalf("visibility after PATCH = %q, want %q", updated.Visibility, to)
	}

	viewers := map[string]string{"anonymous": "", "other user": otherToken, "owner": ownerToken}
	for name, token := range viewers {
		want := http.StatusOK
		if to == "private" && name != "owner" {
			want = http.StatusNotFound
		}
		if code := serve(handler, authedRequest(http.MethodGet, base, token, nil)).Code; code != want {
			t.Errorf("%s GET entry = %d, want %d", name, code, want)
		}

		listed := to == "public"
		profile := serve(handler, authedRequest(http.MethodGet, "/api/v1/users/vis-owner/entries", token, nil))
		if profile.Code != http.StatusOK || strings.Contains(profile.Body.String(), entry.ID) != listed {
			t.Errorf("%s profile = %d %s, want listed=%v", name, profile.Code, profile.Body, listed)
		}

		results := serve(handler, authedRequest(http.MethodGet, "/api/v1/search?q=x", token, nil))
		if results.Code != http.StatusOK || strings.Contains(results.Body.String(), entry.ID) != listed {
			t.Errorf("%s search with a stale index = %d %s, want listed=%v", name, results.Code, results.Body, listed)
		}
	}

	if from != "private" {
		favorites := serve(handler, authedRequest(http.MethodGet, "/api/v1/me/favorites", otherToken, nil))
		if shown := strings.Contains(favorites.Body.String(), entry.ID); shown != (to != "private") {
			t.Errorf("other user's favorites show it = %v, want %v", shown, to != "private")
		}
	}

	// The change is queued for the search projection, which indexes a public
	// entry and removes any other (search/sync_test.go).
	var queued int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1", entry.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Errorf("search jobs queued = %d, want 1", queued)
	}
}

// Only the owner may change visibility, and an anonymous entry stays public.
func TestVisibilityChangeRules(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	ownerToken, _ := registerTestUser(t, handler, "vis-rules-owner")
	otherToken, _ := registerTestUser(t, handler, "vis-rules-other")
	patch := func(entryID, token, editToken, body string) int {
		request := authedRequest(http.MethodPatch, "/api/v1/entries/"+entryID, token, []byte(body))
		if editToken != "" {
			request.Header.Set("X-Edit-Token", editToken)
		}
		return serve(handler, request).Code
	}

	owned := createEntryWithVisibility(t, handler, ownerToken, "public")
	for _, test := range []struct {
		name, token, body string
		want              int
	}{
		{"other user", otherToken, `{"visibility":"private"}`, http.StatusForbidden},
		{"anonymous", "", `{"visibility":"unlisted"}`, http.StatusForbidden},
		// Outside the documented enum: validation refuses it.
		{"unknown value", ownerToken, `{"visibility":"hidden"}`, http.StatusUnprocessableEntity},
		{"empty value", ownerToken, `{"visibility":""}`, http.StatusUnprocessableEntity},
	} {
		if code := patch(owned.ID, test.token, "", test.body); code != test.want {
			t.Errorf("%s PATCH = %d, want %d", test.name, code, test.want)
		}
	}

	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	created := postJSON(t, handler, "/api/v1/entries", map[string]string{"blueprint": string(exchange)})
	var anonymous catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &anonymous); err != nil || anonymous.EditToken == "" {
		t.Fatalf("anonymous entry = %s, %v", created.Body, err)
	}
	for _, visibility := range []string{"unlisted", "private"} {
		if code := patch(anonymous.ID, "", anonymous.EditToken, `{"visibility":"`+visibility+`"}`); code != http.StatusForbidden {
			t.Errorf("anonymous entry to %s = %d, want 403", visibility, code)
		}
	}
	if code := patch(anonymous.ID, "", anonymous.EditToken, `{"visibility":"public"}`); code != http.StatusOK {
		t.Errorf("anonymous entry to public = %d, want 200", code)
	}
}
