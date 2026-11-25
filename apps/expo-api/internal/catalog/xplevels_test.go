package catalog

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/analysisresult"
)

func TestLevelOf(t *testing.T) {
	for _, tc := range []struct {
		xp    int64
		level int
		from  int64
		next  int64 // 0: none
	}{
		{0, 1, 0, 50}, {49, 1, 0, 50}, {50, 2, 50, 150}, {1999, 7, 1200, 2000},
		{4999, 9, 3000, 5000}, {5000, 10, 5000, 0}, {123456, 10, 5000, 0},
	} {
		level, from, next := levelOf(tc.xp)
		if level != tc.level || from != tc.from || (next == nil) != (tc.next == 0) || (next != nil && *next != tc.next) {
			t.Errorf("levelOf(%d) = %d, %d, %v; want %d, %d, %d", tc.xp, level, from, next, tc.level, tc.from, tc.next)
		}
	}
}

func TestBadgesOf(t *testing.T) {
	tally := xpTally{
		counted: map[XPReason]int64{xpUpload: 10, xpFavorite: 24, xpComment: 10},
		// A record and a third place earned once stay earned.
		all: map[XPReason]int64{xpUpload: 12, xpFavorite: 30, xpRecord: 1, xpChallengeThird: 1, xpChallengeParticipation: 1},
	}
	got := badgesOf(tally, true, false)
	want := []Badge{
		BadgeFirstUpload, BadgeTenUploads, BadgeFirstFavorite, BadgeTenComments, BadgeBookAuthor,
		BadgeRecordHolder, BadgeChallengeEntrant, BadgeChallengePodium,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("badges = %v, want %v", got, want)
	}
	if got := badgesOf(xpTally{}, false, false); len(got) != 0 {
		t.Fatalf("no ledger: %v", got)
	}
}

func TestRankBoardOnePlacePerEntry(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	leaf := func(entry, blueprintID string, at time.Time, facts shipFacts) boardLeaf {
		return boardLeaf{facts: leafFacts{ship: &facts}, row: db.ListBoardCandidatesRow{
			EntryID: entry, BlueprintID: blueprintID, RootKind: "book", EntryTitle: entry, Label: blueprintID,
			RevisionCreatedAt: pgtype.Timestamptz{Time: at, Valid: true},
		}}
	}
	ships := []boardLeaf{
		leaf("bookAAAAAAAA", "slow", early, shipFacts{speed: 10, weight: 100}),
		leaf("bookAAAAAAAA", "quick", early, shipFacts{speed: 50, weight: 300}),
		leaf("lateBBBBBBBB", "tied", early.Add(time.Hour), shipFacts{speed: 50, weight: 100}),
	}
	fastest := rankBoard(boardSpecOf(BoardFastest), ships)
	if len(fastest) != 2 || fastest[0].Leaf.BlueprintID != "quick" || fastest[0].Leaf.BookTitle != "bookAAAAAAAA" ||
		fastest[1].Leaf.BlueprintID != "tied" || fastest[1].Rank != 2 {
		t.Fatalf("fastest = %+v", fastest)
	}
	lightest := rankBoard(boardSpecOf(BoardLightest), ships)
	if lightest[0].Leaf.BlueprintID != "slow" || lightest[0].Value != 100 {
		t.Fatalf("lightest = %+v", lightest)
	}
	// No cargo bays: nobody ranks for cargo, nor for thrust without thrust.
	if cargo := rankBoard(boardSpecOf(BoardMostCargo), ships); len(cargo) != 0 {
		t.Fatalf("cargo = %+v", cargo)
	}
	if ratio := rankBoard(boardSpecOf(BoardThrustToWeight), ships); len(ratio) != 0 {
		t.Fatalf("thrust-to-weight = %+v", ratio)
	}
	// Ships never rank on a factory board.
	for _, board := range []Board{BoardMostScience, BoardDensestScience, BoardCleanestScience, BoardBiggestPowerPlant} {
		if places := rankBoard(boardSpecOf(board), ships); len(places) != 0 {
			t.Fatalf("%s ranks ships: %+v", board, places)
		}
	}
}

func TestFactoryFacts(t *testing.T) {
	facts := factoryFactsOf(analysisresult.Facts{
		OutRates: map[string]float64{
			"automation-science-pack": 1, "logistic-science-pack": 0.5, "iron-gear-wheel": 9,
			// Made and used up: no net science.
			"military-science-pack": 0.25,
		},
		InRates:                    map[string]float64{"military-science-pack": 0.25, "iron-gear-wheel": 9},
		PollutionNetPerSecond:      0.45,
		ElectricityProductionWatts: 2e6, ElectricityNetWatts: -1e6,
		Footprint: &analysisresult.Footprint{Width: 30, Height: 30},
	})
	if facts.spm != 90 || facts.tiles != 900 || facts.pollutionPerMinute != 27 || facts.netMW != -1 {
		t.Fatalf("facts = %+v", *facts)
	}
	value := func(board Board, f factoryFacts) (float64, bool) {
		return boardSpecOf(board).value(leafFacts{factory: &f})
	}
	if spm, ok := value(BoardMostScience, *facts); !ok || spm != 90 {
		t.Fatalf("most science = %v, %v", spm, ok)
	}
	if density, ok := value(BoardDensestScience, *facts); !ok || density != 10 {
		t.Fatalf("densest = %v, %v", density, ok)
	}
	if clean, ok := value(BoardCleanestScience, *facts); !ok || clean != 0.3 {
		t.Fatalf("cleanest = %v, %v", clean, ok)
	}
	if _, ok := value(BoardBiggestPowerPlant, *facts); ok {
		t.Fatal("a build that uses more power than it makes ranks as a power plant")
	}
	// Below 60 SPM a build ranks on most science only; without a footprint
	// it cannot be the densest.
	small := factoryFacts{spm: 30, tiles: 9, pollutionPerMinute: 1}
	if _, ok := value(BoardDensestScience, small); ok {
		t.Fatal("30 SPM ranks on density")
	}
	if _, ok := value(BoardCleanestScience, small); ok {
		t.Fatal("30 SPM ranks on cleanliness")
	}
	if _, ok := value(BoardDensestScience, factoryFacts{spm: 120}); ok {
		t.Fatal("no footprint ranks on density")
	}
	if mw, ok := value(BoardBiggestPowerPlant, factoryFacts{netMW: 40}); !ok || mw != 40 {
		t.Fatalf("power plant = %v, %v", mw, ok)
	}
}

func TestBoardsHaveGroupsAndDescriptions(t *testing.T) {
	groups := map[BoardGroup]int{}
	for _, spec := range boardSpecs {
		if spec.title == "" || spec.description == "" || spec.unit == "" || !slices.Contains(BoardGroups, spec.group) {
			t.Errorf("board %s is incomplete: %+v", spec.board, spec)
		}
		groups[spec.group]++
	}
	if groups[BoardGroupShips] != 4 || groups[BoardGroupFactories] != 4 {
		t.Fatalf("boards per group = %v", groups)
	}
}

func TestRulesListEveryAwardLevelAndBadge(t *testing.T) {
	rules := Rules()
	if len(rules.Levels) != len(LevelThresholds) || rules.Levels[9].Level != 10 || rules.Levels[9].XP != 5000 {
		t.Fatalf("levels = %+v", rules.Levels)
	}
	if len(rules.Awards) != len(xpPoints) {
		t.Fatalf("awards = %+v, want one per reason", rules.Awards)
	}
	points := map[XPReason]XPAwardRule{}
	for _, award := range rules.Awards {
		points[award.Reason] = award
	}
	for reason, want := range map[XPReason][2]int32{
		xpUpload: {10, 5}, xpFavorite: {2, 0}, xpCollection: {3, 0}, xpComment: {1, 5}, xpRecord: {50, 0},
		xpChallengeWin: {500, 0}, xpChallengeSecond: {300, 0}, xpChallengeThird: {200, 0}, xpChallengeParticipation: {25, 0},
	} {
		if got := points[reason]; got.Points != want[0] || got.DailyCap != want[1] {
			t.Errorf("%s = %+v, want %v", reason, got, want)
		}
	}
	if len(rules.Badges) != len(Badges) {
		t.Fatalf("badges = %+v", rules.Badges)
	}
	for _, badge := range rules.Badges {
		if badge.Criteria == "" {
			t.Errorf("badge %s has no criteria", badge.Badge)
		}
	}
}
