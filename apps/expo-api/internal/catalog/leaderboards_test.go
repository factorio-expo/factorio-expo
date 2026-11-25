package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/internal/analysisresult"
)

// ship is a platform's metrics for storePlatformAnalysis.
type ship struct {
	speed, thrust, weight float64
}

// storePlatformAnalysis stores test-data/analysis/analysis-platform.json with
// the given metrics as the leaf's complete analysis and closes its job.
func storePlatformAnalysis(t *testing.T, service *catalog.Service, pool *pgxpool.Pool, blueprintID string, metrics ship) {
	t.Helper()
	data, err := os.ReadFile("../../../../test-data/analysis/analysis-platform.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["state"] = "complete"
	envelope["space_platform"] = map[string]any{
		"is_space_platform": true, "max_speed_km_s": metrics.speed, "thrust_mn": metrics.thrust, "weight_tons": metrics.weight,
	}
	data, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := analysisresult.Parse(data, envelope["job_id"].(string))
	if err != nil || result == nil {
		t.Fatalf("parse platform analysis: %v", err)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.StoreProduction(context.Background(), blueprintID, stored); err != nil {
		t.Fatalf("store production: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM phase_jobs WHERE phase = 'analysis' AND blueprint_id = $1", blueprintID); err != nil {
		t.Fatal(err)
	}
}

// uploadShip uploads test-data/ship-hauler.txt as a public entry of userID and
// stores its analysis.
func uploadShip(t *testing.T, service *catalog.Service, pool *pgxpool.Pool, userID string, n int, metrics ship) catalog.Entry {
	t.Helper()
	title := fmt.Sprintf("Ship %d", n)
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, "ship-hauler.txt"), Title: &title, OwnerUserID: userID, Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create ship: %v", err)
	}
	storePlatformAnalysis(t, service, pool, entry.Revision.Root.ID, metrics)
	return entry
}

func boardOf(t *testing.T, service *catalog.Service, board catalog.Board) catalog.Leaderboard {
	t.Helper()
	view, err := service.Leaderboard(context.Background(), board, 50)
	if err != nil {
		t.Fatalf("read board %s: %v", board, err)
	}
	return view
}

func TestShipBoardsRankFromTenEntriesAndAwardRecords(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	// Nine of alice's ships, slow to fast, heavy to light.
	for n := 1; n <= 9; n++ {
		uploadShip(t, service, pool, userA, n, ship{speed: float64(10 * n), thrust: 100, weight: float64(1000 - 50*n)})
	}
	// Not ranked: a partial analysis, a modded leaf, a platform that does not move.
	partial := uploadShip(t, service, pool, userA, 20, ship{speed: 900, thrust: 900, weight: 10})
	if _, err := pool.Exec(ctx, "UPDATE blueprint_stats SET production_state = 'partial' WHERE blueprint_id = $1", partial.Revision.Root.ID); err != nil {
		t.Fatal(err)
	}
	modded := uploadShip(t, service, pool, userA, 21, ship{speed: 900, thrust: 900, weight: 10})
	if _, err := pool.Exec(ctx, `UPDATE blueprint_stats SET mods = '{"modsets":[],"unknown_entities":["warp-drive"],"unknown_recipes":[]}'
		WHERE blueprint_id = $1`, modded.Revision.Root.ID); err != nil {
		t.Fatal(err)
	}
	uploadShip(t, service, pool, userA, 22, ship{speed: 0, thrust: 0, weight: 5})

	if _, err := service.RefreshBoardLeaders(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if board := boardOf(t, service, catalog.BoardFastest); board.Visible || board.Eligible != 9 || len(board.Places) != 0 {
		t.Fatalf("nine ships: %+v, want a hidden board of 9", board)
	}

	// Bob's ship is the tenth and the fastest.
	fastest := uploadShip(t, service, pool, userB, 10, ship{speed: 500, thrust: 50, weight: 990})
	awarded, err := service.RefreshBoardLeaders(ctx)
	if err != nil || awarded == 0 {
		t.Fatalf("refresh = %d, %v", awarded, err)
	}
	board := boardOf(t, service, catalog.BoardFastest)
	if !board.Visible || board.Eligible != 10 || len(board.Places) != 10 || board.Places[0].Leaf.EntryID != fastest.ID ||
		board.Places[0].Value != 500 || board.Places[1].Value != 90 || board.LeaderSince == nil {
		t.Fatalf("fastest board = %+v", board)
	}
	lightest := boardOf(t, service, catalog.BoardLightest)
	if !lightest.LowerWins || lightest.Places[0].Value != 550 || lightest.Places[0].Leaf.Title != "Ship 9" {
		t.Fatalf("lightest board = %+v", lightest.Places[0])
	}
	if cargo := boardOf(t, service, catalog.BoardMostCargo); cargo.Eligible != 0 && cargo.Eligible != 10 {
		t.Fatalf("cargo board eligible = %d", cargo.Eligible)
	}

	bob := progressOf(t, service, "social-test-bob")
	if !slices.Contains(bob.Badges, catalog.BadgeRecordHolder) || len(bob.Records) == 0 ||
		bob.Records[0].EntryID != fastest.ID {
		t.Fatalf("bob = %+v, want the fastest record", bob)
	}
	xp := bob.XP
	// A second run awards nothing new.
	if again, err := service.RefreshBoardLeaders(ctx); err != nil || again != 0 {
		t.Fatalf("second refresh = %d, %v", again, err)
	}
	if progressOf(t, service, "social-test-bob").XP != xp {
		t.Fatal("a second refresh changed the XP")
	}

	// Deleting the record entry hides the board again and takes the points
	// back; the record-holder badge stays.
	if err := service.DeleteEntry(ctx, fastest.ID, catalog.Authorization{UserID: userB}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RefreshBoardLeaders(ctx); err != nil {
		t.Fatal(err)
	}
	if board := boardOf(t, service, catalog.BoardFastest); board.Visible {
		t.Fatalf("after the delete: %+v, want hidden", board)
	}
	bob = progressOf(t, service, "social-test-bob")
	if bob.XP != 0 || len(bob.Records) != 0 || !slices.Contains(bob.Badges, catalog.BadgeRecordHolder) {
		t.Fatalf("bob after the delete = %+v", bob)
	}
	var leaders int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM board_leaders").Scan(&leaders); err != nil {
		t.Fatal(err)
	}
	if leaders != 0 {
		t.Fatalf("board leaders = %d, want none", leaders)
	}
}

// factoryBuild is a ground build's analysis for storeFactoryAnalysis.
type factoryBuild struct {
	sciencePerSecond, pollutionPerSecond, netWatts float64
	width, height                                  int
}

// storeFactoryAnalysis stores test-data/analysis/analysis-succeeded.json as
// the leaf's complete analysis, making the build's science, pollution, net
// electricity and footprint those given.
func storeFactoryAnalysis(t *testing.T, service *catalog.Service, pool *pgxpool.Pool, blueprintID string, build factoryBuild) {
	t.Helper()
	data, err := os.ReadFile("../../../../test-data/analysis/analysis-succeeded.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["products"] = []any{map[string]any{
		"type": "item", "name": "automation-science-pack", "quality": "normal",
		"output_per_second": build.sciencePerSecond, "producer_count": 1,
	}}
	envelope["footprint"] = map[string]any{"width": build.width, "height": build.height}
	pollution := envelope["pollution"].(map[string]any)
	pollution["production_per_second"], pollution["absorption_per_second"], pollution["net_per_second"] =
		build.pollutionPerSecond, 0.0, build.pollutionPerSecond
	electricity := envelope["electricity"].(map[string]any)
	if build.netWatts > 0 {
		electricity["production_watts"] = build.netWatts + electricity["consumption_watts"].(float64)
		electricity["net_watts"] = build.netWatts
	}
	data, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := analysisresult.Parse(data, envelope["job_id"].(string))
	if err != nil || result == nil {
		t.Fatalf("parse factory analysis: %v", err)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.StoreProduction(context.Background(), blueprintID, stored); err != nil {
		t.Fatalf("store production: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM phase_jobs WHERE phase = 'analysis' AND blueprint_id = $1", blueprintID); err != nil {
		t.Fatal(err)
	}
}

// uploadFactory uploads test-data/bp-starter-smelter.txt as a public entry of userID and
// stores its analysis.
func uploadFactory(t *testing.T, service *catalog.Service, pool *pgxpool.Pool, userID string, n int, build factoryBuild) catalog.Entry {
	t.Helper()
	title := fmt.Sprintf("Factory %d", n)
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), Title: &title, OwnerUserID: userID, Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create factory: %v", err)
	}
	storeFactoryAnalysis(t, service, pool, entry.Revision.Root.ID, build)
	return entry
}

func TestFactoryBoardsRankAndRecordsReachCards(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	// Nine of alice's science builds: 66 to 114 SPM, 20x20, 1 pollution/s.
	for n := 1; n <= 9; n++ {
		uploadFactory(t, service, pool, userA, n, factoryBuild{
			sciencePerSecond: 1 + float64(n)*0.1, pollutionPerSecond: 1, width: 20, height: 20,
		})
	}
	// A small build ranks on most science only; a power plant makes no science.
	uploadFactory(t, service, pool, userA, 20, factoryBuild{sciencePerSecond: 0.5, pollutionPerSecond: 1, width: 3, height: 3})
	uploadFactory(t, service, pool, userA, 21, factoryBuild{netWatts: 40e6, width: 30, height: 30})
	// Bob's: the most science, but the biggest footprint and the most pollution.
	leader := uploadFactory(t, service, pool, userB, 10, factoryBuild{
		sciencePerSecond: 5, pollutionPerSecond: 50, width: 100, height: 100,
	})
	if _, err := service.RefreshBoardLeaders(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	most := boardOf(t, service, catalog.BoardMostScience)
	if !most.Visible || most.Group != catalog.BoardGroupFactories || most.Description == "" || most.Eligible != 11 ||
		most.Places[0].Leaf.EntryID != leader.ID || most.Places[0].Value != 300 {
		t.Fatalf("most science = %+v", most)
	}
	densest := boardOf(t, service, catalog.BoardDensestScience)
	if !densest.Visible || densest.Eligible != 10 || !densest.LowerWins || densest.Places[0].Leaf.Title != "Factory 9" ||
		densest.Places[9].Leaf.EntryID != leader.ID {
		t.Fatalf("densest science = %+v", densest)
	}
	if cleanest := boardOf(t, service, catalog.BoardCleanestScience); !cleanest.Visible || cleanest.Places[0].Leaf.Title != "Factory 9" {
		t.Fatalf("cleanest science = %+v", cleanest)
	}
	if power := boardOf(t, service, catalog.BoardBiggestPowerPlant); power.Visible || power.Eligible != 1 {
		t.Fatalf("power plant = %+v, want hidden with one entry", power)
	}
	if ships := boardOf(t, service, catalog.BoardFastest); ships.Eligible != 0 {
		t.Fatalf("factories rank as ships: %+v", ships)
	}

	// The leader's card and entry carry the record; another entry's none.
	bob := progressOf(t, service, "social-test-bob")
	if !slices.Contains(bob.Badges, catalog.BadgeRecordHolder) || len(bob.Records) != 1 ||
		bob.Records[0].Board != catalog.BoardMostScience || bob.Records[0].Group != catalog.BoardGroupFactories {
		t.Fatalf("bob = %+v, want the most science record", bob)
	}
	cards, err := service.EntryCards(ctx, []string{leader.ID, densest.Places[0].Leaf.EntryID}, "")
	if err != nil {
		t.Fatal(err)
	}
	records := cards[leader.ID].Records
	if len(records) != 1 || records[0].Board != catalog.BoardMostScience || records[0].Title != "Most science" ||
		records[0].BlueprintID != leader.Revision.Root.ID {
		t.Fatalf("leader card records = %+v", records)
	}
	if other := cards[densest.Places[0].Leaf.EntryID].Records; len(other) != 2 ||
		other[0].Board != catalog.BoardDensestScience || other[1].Board != catalog.BoardCleanestScience {
		t.Fatalf("densest card records = %+v, want densest then cleanest", other)
	}
	entry, err := service.GetEntry(ctx, leader.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Records) != 1 || len(catalog.LeafRecords(entry.Records, leader.Revision.Root.ID)) != 1 ||
		len(catalog.LeafRecords(entry.Records, "otherLeaf123")) != 0 {
		t.Fatalf("entry records = %+v", entry.Records)
	}
}
