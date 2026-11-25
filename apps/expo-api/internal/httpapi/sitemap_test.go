package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

type sitemapPage struct {
	Entries []struct {
		ID        string    `json:"id"`
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"entries"`
	Next string `json:"next"`
}

func getSitemapPage(t *testing.T, handler http.Handler, query string) sitemapPage {
	t.Helper()
	recorder := serve(handler, httptest.NewRequest(http.MethodGet, "/api/v1/sitemap/entries"+query, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("sitemap %s = %d, %s", query, recorder.Code, recorder.Body)
	}
	var page sitemapPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

// The sitemap lists only public entries, in id order, paged by the last id.
func TestSitemapListsPublicEntriesByID(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	token, _ := registerTestUser(t, handler, "sitemap-owner")
	blueprint := readFixture(t, "bp-starter-smelter.txt")
	public := []string{
		uploadAnonymous(t, handler, map[string]any{"blueprint": blueprint}).ID,
		uploadAnonymous(t, handler, map[string]any{"blueprint": blueprint}).ID,
		createEntryWithVisibility(t, handler, token, "public").ID,
	}
	createEntryWithVisibility(t, handler, token, "unlisted")
	createPrivateEntry(t, handler, token)
	slices.Sort(public)

	var listed []string
	query := "?limit=2"
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatal("sitemap paging did not end")
		}
		page := getSitemapPage(t, handler, query)
		for _, entry := range page.Entries {
			if entry.UpdatedAt.IsZero() {
				t.Errorf("entry %s has no updated_at", entry.ID)
			}
			listed = append(listed, entry.ID)
		}
		if page.Next == "" {
			break
		}
		query = "?limit=2&after=" + page.Next
	}
	if !slices.Equal(listed, public) {
		t.Fatalf("sitemap = %v, want the public entries %v", listed, public)
	}

	if page := getSitemapPage(t, handler, ""); len(page.Entries) != 3 || page.Next != "" {
		t.Fatalf("default page = %d entries, next %q; want all 3 and no next", len(page.Entries), page.Next)
	}
}
