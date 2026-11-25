package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The boards, the home picks and the profile's progress, read through the
// public API.
func TestLeaderboardsHomePicksAndProfileProgress(t *testing.T) {
	handler, _ := newTestAPI(t)
	truncateSocialFixtures(t)
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "TRUNCATE home_picks"); err != nil {
		t.Fatal(err)
	}
	token, _ := registerTestUser(t, handler, "board-alice")
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	created := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", token,
		jsonBody(t, map[string]string{"blueprint": string(exchange), "title": "Picked"})))
	var entry struct {
		ID       string `json:"id"`
		Revision struct {
			Root struct{ ID string } `json:"root"`
		} `json:"revision"`
	}
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &entry) != nil {
		t.Fatalf("create = %d: %s", created.Code, created.Body)
	}

	get := func(path string, into any) int {
		t.Helper()
		recorder := serve(handler, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code == http.StatusOK && into != nil {
			if err := json.Unmarshal(recorder.Body.Bytes(), into); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
		}
		return recorder.Code
	}

	var boards struct {
		Boards []struct {
			Board       string
			Group       string
			Description string
			Visible     bool
			MinEntries  int `json:"min_entries"`
			Places      []any
		}
	}
	if code := get("/api/v1/leaderboards?limit=5", &boards); code != http.StatusOK || len(boards.Boards) != 8 {
		t.Fatalf("leaderboards = %d %+v", code, boards)
	}
	groups := map[string]int{}
	for _, board := range boards.Boards {
		groups[board.Group]++
		if board.Visible || board.MinEntries != 10 || board.Places == nil || len(board.Places) != 0 || board.Description == "" {
			t.Errorf("board %+v, want hidden with no places", board)
		}
	}
	if groups["ships"] != 4 || groups["factories"] != 4 {
		t.Errorf("groups = %v", groups)
	}
	if code := get("/api/v1/leaderboards/most_science", nil); code != http.StatusOK {
		t.Errorf("one board = %d", code)
	}
	if code := get("/api/v1/leaderboards/slowest", nil); code == http.StatusOK {
		t.Errorf("unknown board = %d", code)
	}

	if _, err := pool.Exec(context.Background(),
		"INSERT INTO home_picks (blueprint_id, caption, picked_by) VALUES ($1, 'A tidy one', 'staff')",
		entry.Revision.Root.ID); err != nil {
		t.Fatal(err)
	}
	// The entry leads a board (as the leaderboards task stores it): its cards
	// and its page name the record.
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO board_leaders (board, entry_id, blueprint_id, value) VALUES ('most_science', $1, $2, 120)",
		entry.ID, entry.Revision.Root.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "TRUNCATE board_leaders") })
	var page struct {
		Records []struct {
			Board, Group, Title string
			BlueprintID         string `json:"blueprint_id"`
		}
	}
	if code := get("/api/v1/entries/"+entry.ID, &page); code != http.StatusOK || len(page.Records) != 1 ||
		page.Records[0].Board != "most_science" || page.Records[0].Group != "factories" ||
		page.Records[0].Title != "Most science" || page.Records[0].BlueprintID != entry.Revision.Root.ID {
		t.Fatalf("entry records = %d %+v", code, page.Records)
	}
	var picks struct {
		Picks []struct {
			Caption string
			Card    struct {
				EntryID string `json:"entry_id"`
				NodeID  string `json:"node_id"`
				Title   string
				Kind    string
				Records []struct{ Board string }
			}
		}
	}
	if code := get("/api/v1/home-picks", &picks); code != http.StatusOK || len(picks.Picks) != 1 {
		t.Fatalf("home picks = %d %+v", code, picks)
	}
	if pick := picks.Picks[0]; pick.Caption != "A tidy one" || pick.Card.EntryID != entry.ID ||
		pick.Card.NodeID != entry.Revision.Root.ID || pick.Card.Title != "Picked" || pick.Card.Kind != "blueprint" ||
		len(pick.Card.Records) != 1 || pick.Card.Records[0].Board != "most_science" {
		t.Errorf("pick = %+v", pick)
	}

	var rules struct {
		Awards []struct {
			Reason   string
			Points   int
			DailyCap int `json:"daily_cap"`
		}
		Levels []struct{ Level, XP int }
		Badges []struct{ Badge, Criteria string }
	}
	if code := get("/api/v1/xp-rules", &rules); code != http.StatusOK || len(rules.Awards) != 9 ||
		len(rules.Levels) != 10 || len(rules.Badges) != 11 || rules.Awards[0].Reason != "upload" ||
		rules.Awards[0].Points != 10 || rules.Awards[0].DailyCap != 5 || rules.Levels[9].XP != 5000 {
		t.Errorf("xp rules = %d %+v", code, rules)
	}

	var profile struct {
		Progress struct {
			XP          int64    `json:"xp"`
			Level       int      `json:"level"`
			NextLevelXP int64    `json:"next_level_xp"`
			Badges      []string `json:"badges"`
			Records     []any    `json:"records"`
		}
	}
	if code := get("/api/v1/users/board-alice", &profile); code != http.StatusOK {
		t.Fatalf("profile = %d", code)
	}
	if got := profile.Progress; got.XP != 10 || got.Level != 1 || got.NextLevelXP != 50 ||
		len(got.Badges) != 1 || got.Badges[0] != "first_upload" || got.Records == nil {
		t.Errorf("progress = %+v", got)
	}
}
