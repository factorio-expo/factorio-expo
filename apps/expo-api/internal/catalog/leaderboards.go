package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"factorio-expo/apps/expo-api/internal/blueprint"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/internal/analysisresult"
)

// The leaderboard (docs/architecture.md#leaderboards): record boards in two
// groups, ranked from the stored analysis. Ships: the fastest, the best
// thrust-to-weight, the lightest and the most cargo, among space platforms
// that move. Factories: the most science, the densest and the cleanest
// science, and the biggest power plant, among builds that are not space
// platforms. Only complete analyses that need no mod but Space Age's rank, in
// the current revision of a listed entry; each entry takes one place per
// board, with its best leaf. A board shows once it has BoardMinEntries
// entries. The leaderboards task stores each board's leader every minute and
// awards the record (xp.go); the read is cached for boardCacheTTL. There are
// deliberately no per-item production boards.

// Board names a record board.
type Board string

const (
	BoardFastest           Board = "fastest"
	BoardThrustToWeight    Board = "thrust_to_weight"
	BoardLightest          Board = "lightest"
	BoardMostCargo         Board = "most_cargo"
	BoardMostScience       Board = "most_science"
	BoardDensestScience    Board = "densest_science"
	BoardCleanestScience   Board = "cleanest_science"
	BoardBiggestPowerPlant Board = "biggest_power_plant"
)

// BoardGroup is the leaderboard's tab a board shows under.
type BoardGroup string

const (
	BoardGroupShips     BoardGroup = "ships"
	BoardGroupFactories BoardGroup = "factories"
)

// BoardGroups lists the groups in display order.
var BoardGroups = []BoardGroup{BoardGroupShips, BoardGroupFactories}

// Boards lists every board in display order (boardSpecs' order).
var Boards = func() []Board {
	boards := make([]Board, len(boardSpecs))
	for i, spec := range boardSpecs {
		boards[i] = spec.board
	}
	return boards
}()

const (
	// BoardMinEntries is how many eligible entries a board needs to show,
	// so the first uploads never crown themselves.
	BoardMinEntries = 10
	// MaxBoardPlaces is the most places one read returns per board.
	MaxBoardPlaces = 50
	// boardCacheTTL is how long a computed set of boards is served.
	boardCacheTTL = 30 * time.Second
	// cargoBay is the entity a ship's cargo is counted in.
	cargoBay = "cargo-bay"
	// sciencePackSuffix names the science packs (automation-science-pack,
	// ..., promethium-science-pack).
	sciencePackSuffix = "-science-pack"
	// ratioBoardMinSPM is the science a build needs on the density and
	// cleanliness boards, so a lone assembler cannot win a ratio.
	ratioBoardMinSPM = 60
)

// shipFacts are the metrics a ship is ranked on.
type shipFacts struct {
	speed, thrust, weight float64
	cargoBays             int
}

// factoryFacts are the metrics a ground build is ranked on.
type factoryFacts struct {
	// spm is the science packs per minute made beyond what the build itself
	// uses, every pack and quality summed.
	spm float64
	// tiles is the footprint's area; 0 when unmeasured.
	tiles float64
	// pollutionPerMinute is the net pollution (absorption subtracted).
	pollutionPerMinute float64
	// netMW is the electricity made minus the electricity used.
	netMW float64
}

// leafFacts are a candidate's metrics: exactly one of ship and factory is set.
type leafFacts struct {
	ship    *shipFacts
	factory *factoryFacts
}

// boardSpec is how one board ranks.
type boardSpec struct {
	board Board
	group BoardGroup
	title string
	// description is the one line saying what wins.
	description string
	unit        string
	// lowerWins: the smallest value leads (the lightest).
	lowerWins bool
	value     func(leafFacts) (float64, bool)
}

// shipValue and factoryValue adapt a group's value function to leafFacts.
func shipValue(value func(shipFacts) (float64, bool)) func(leafFacts) (float64, bool) {
	return func(facts leafFacts) (float64, bool) {
		if facts.ship == nil {
			return 0, false
		}
		return value(*facts.ship)
	}
}

func factoryValue(value func(factoryFacts) (float64, bool)) func(leafFacts) (float64, bool) {
	return func(facts leafFacts) (float64, bool) {
		if facts.factory == nil {
			return 0, false
		}
		return value(*facts.factory)
	}
}

var boardSpecs = []boardSpec{
	{
		board: BoardFastest, group: BoardGroupShips, title: "Fastest ship", unit: "km/s",
		description: "The highest top speed of a space platform that moves.",
		value:       shipValue(func(f shipFacts) (float64, bool) { return f.speed, true }),
	},
	{
		board: BoardThrustToWeight, group: BoardGroupShips, title: "Highest thrust-to-weight", unit: "MN/t",
		description: "The most thrust per ton of platform: the snappiest acceleration.",
		value:       shipValue(func(f shipFacts) (float64, bool) { return f.thrust / f.weight, f.thrust > 0 }),
	},
	{
		board: BoardLightest, group: BoardGroupShips, title: "Lightest ship", unit: "t", lowerWins: true,
		description: "The lowest weight of a space platform that moves.",
		value:       shipValue(func(f shipFacts) (float64, bool) { return f.weight, true }),
	},
	{
		board: BoardMostCargo, group: BoardGroupShips, title: "Most cargo", unit: "cargo bays",
		description: "The most cargo bays on a space platform that moves.",
		value: shipValue(func(f shipFacts) (float64, bool) {
			return float64(f.cargoBays), f.cargoBays > 0
		}),
	},
	{
		board: BoardMostScience, group: BoardGroupFactories, title: "Most science", unit: "SPM",
		description: "The most science packs per minute the build puts out, every pack and quality summed.",
		value:       factoryValue(func(f factoryFacts) (float64, bool) { return f.spm, f.spm > 0 }),
	},
	{
		board: BoardDensestScience, group: BoardGroupFactories, title: "Densest science", unit: "tiles/SPM",
		lowerWins:   true,
		description: "The fewest tiles of footprint per science pack a minute, from 60 SPM up.",
		value: factoryValue(func(f factoryFacts) (float64, bool) {
			return f.tiles / f.spm, f.spm >= ratioBoardMinSPM && f.tiles > 0
		}),
	},
	{
		board: BoardCleanestScience, group: BoardGroupFactories, title: "Cleanest science", unit: "pollution/pack",
		lowerWins:   true,
		description: "The least pollution per science pack made, from 60 SPM up.",
		value: factoryValue(func(f factoryFacts) (float64, bool) {
			return f.pollutionPerMinute / f.spm, f.spm >= ratioBoardMinSPM
		}),
	},
	{
		board: BoardBiggestPowerPlant, group: BoardGroupFactories, title: "Biggest power plant", unit: "MW",
		description: "The most electricity left over once the build has powered itself.",
		value:       factoryValue(func(f factoryFacts) (float64, bool) { return f.netMW, f.netMW > 0 }),
	},
}

// boardSpecOf is the board's spec (the board_leaders CHECK is its twin).
func boardSpecOf(board Board) boardSpec {
	for _, spec := range boardSpecs {
		if spec.board == board {
			return spec
		}
	}
	return boardSpec{board: board, title: string(board)}
}

// boardOrder is the board's place in display order.
func boardOrder(board Board) int {
	for i, spec := range boardSpecs {
		if spec.board == board {
			return i
		}
	}
	return len(boardSpecs)
}

// better reports whether a beats b on the board.
func (spec boardSpec) better(a, b float64) bool {
	if spec.lowerWins {
		return a < b
	}
	return a > b
}

// LeafRef is the blueprint a place or pick shows, with what its card needs.
type LeafRef struct {
	EntryID     string
	BlueprintID string
	// Title is the card's title: a single blueprint's entry title, else the
	// blueprint's label; BookTitle is the entry's title for a book's leaf.
	Title       string
	Description string
	Icons       string
	BookTitle   string
}

// leafRef builds a LeafRef from an entry and one of its blueprints.
func leafRef(entryID, entryTitle, rootKind, blueprintID, label, description, icons string) LeafRef {
	ref := LeafRef{EntryID: entryID, BlueprintID: blueprintID, Title: label, Description: description, Icons: icons}
	if rootKind == string(blueprint.KindBlueprint) {
		ref.Title = entryTitle
	} else {
		ref.BookTitle = entryTitle
	}
	if ref.Title == "" {
		ref.Title = entryTitle
	}
	return ref
}

// BoardPlace is one place on a board.
type BoardPlace struct {
	Rank  int
	Value float64
	Leaf  LeafRef
	// ownerID is the entry's owner ("" when anonymous): the record's earner.
	ownerID           string
	revisionCreatedAt time.Time
}

// Leaderboard is one board as a read returns it.
type Leaderboard struct {
	Board       Board
	Group       BoardGroup
	Title       string
	Description string
	Unit        string
	// LowerWins: the smallest value leads.
	LowerWins bool
	// Eligible is how many entries rank; the board shows from BoardMinEntries.
	Eligible int
	Visible  bool
	// LeaderSince is when the leader took the lead, once the leaderboards
	// task has seen it.
	LeaderSince *time.Time
	// Places are the board's top places; none while it is hidden.
	Places []BoardPlace
}

// boardCache keeps the last computed boards for boardCacheTTL.
type boardCache struct {
	mu     sync.Mutex
	at     time.Time
	boards []rankedBoard
}

// rankedBoard is a board with every eligible entry ranked.
type rankedBoard struct {
	spec   boardSpec
	places []BoardPlace
}

func (c *boardCache) get(now time.Time) ([]rankedBoard, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.boards, c.boards != nil && now.Sub(c.at) < boardCacheTTL
}

func (c *boardCache) put(now time.Time, boards []rankedBoard) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.boards = now, boards
}

// Leaderboards returns every board with up to limit places each (hidden
// boards with none), in display order.
func (s *Service) Leaderboards(ctx context.Context, limit int) ([]Leaderboard, error) {
	if limit < 1 || limit > MaxBoardPlaces {
		limit = MaxBoardPlaces
	}
	boards, err := s.rankedBoards(ctx)
	if err != nil {
		return nil, err
	}
	leaders, err := s.queries.ListBoardLeaders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list board leaders: %w", err)
	}
	since := make(map[Board]db.BoardLeader, len(leaders))
	for _, leader := range leaders {
		since[Board(leader.Board)] = leader
	}
	result := make([]Leaderboard, 0, len(boards))
	for _, board := range boards {
		spec := board.spec
		view := Leaderboard{
			Board: spec.board, Group: spec.group, Title: spec.title, Description: spec.description,
			Unit: spec.unit, LowerWins: spec.lowerWins,
			Eligible: len(board.places), Visible: len(board.places) >= BoardMinEntries,
			Places: []BoardPlace{},
		}
		if view.Visible {
			view.Places = board.places[:min(limit, len(board.places))]
			if leader, ok := since[spec.board]; ok && leader.EntryID == board.places[0].Leaf.EntryID {
				at := leader.Since.Time
				view.LeaderSince = &at
			}
		}
		result = append(result, view)
	}
	return result, nil
}

// Leaderboard returns one board, like Leaderboards.
func (s *Service) Leaderboard(ctx context.Context, board Board, limit int) (Leaderboard, error) {
	boards, err := s.Leaderboards(ctx, limit)
	if err != nil {
		return Leaderboard{}, err
	}
	for _, view := range boards {
		if view.Board == board {
			return view, nil
		}
	}
	return Leaderboard{}, ErrNotFound
}

// rankedBoards serves the cached boards or computes them.
func (s *Service) rankedBoards(ctx context.Context) ([]rankedBoard, error) {
	now := time.Now()
	if boards, ok := s.boards.get(now); ok {
		return boards, nil
	}
	boards, err := s.computeBoards(ctx, s.queries)
	if err != nil {
		return nil, err
	}
	s.boards.put(now, boards)
	return boards, nil
}

// computeBoards reads every candidate leaf and ranks each board.
func (s *Service) computeBoards(ctx context.Context, queries *db.Queries) ([]rankedBoard, error) {
	rows, err := queries.ListBoardCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list board candidates: %w", err)
	}
	candidates := make([]boardLeaf, 0, len(rows))
	for _, row := range rows {
		facts, ok, err := leafFactsOf(row)
		if err != nil {
			return nil, err
		}
		if ok {
			candidates = append(candidates, boardLeaf{row: row, facts: facts})
		}
	}
	boards := make([]rankedBoard, 0, len(boardSpecs))
	for _, spec := range boardSpecs {
		boards = append(boards, rankedBoard{spec: spec, places: rankBoard(spec, candidates)})
	}
	return boards, nil
}

// boardLeaf is a candidate that ranks somewhere, with its metrics.
type boardLeaf struct {
	row   db.ListBoardCandidatesRow
	facts leafFacts
}

// leafFactsOf reads a candidate's metrics; ok is false when it ranks nowhere
// (modded, unreadable analysis, a platform that does not move).
func leafFactsOf(row db.ListBoardCandidatesRow) (leafFacts, bool, error) {
	detection, err := storedMods(db.BlueprintStat{Mods: row.Mods, ModsVersion: row.ModsVersion})
	if err != nil {
		return leafFacts{}, false, fmt.Errorf("decode mods of %s: %w", row.BlueprintID, err)
	}
	if !onlySpaceAge(detection) {
		return leafFacts{}, false, nil
	}
	snapshot, err := analysisresult.ParseSnapshot(row.Production, row.ProductionSchemaVersion)
	if err != nil {
		// An analysis this build cannot read ranks nowhere.
		return leafFacts{}, false, nil
	}
	facts := snapshot.Facts()
	if facts.Platform == nil {
		return leafFacts{factory: factoryFactsOf(facts)}, true, nil
	}
	metrics, ok, err := shipFactsOf(row, facts.Platform)
	if err != nil || !ok {
		return leafFacts{}, false, err
	}
	return leafFacts{ship: &metrics}, true, nil
}

// shipFactsOf reads a platform's metrics; ok is false when it does not move.
func shipFactsOf(row db.ListBoardCandidatesRow, platform *analysisresult.SpacePlatform) (shipFacts, bool, error) {
	if platform.MaxSpeedKMS == nil || platform.ThrustMN == nil || platform.WeightTons == nil ||
		*platform.MaxSpeedKMS <= 0 || *platform.WeightTons <= 0 {
		return shipFacts{}, false, nil
	}
	var entities []blueprint.EntityStat
	if err := json.Unmarshal(row.Entities, &entities); err != nil {
		return shipFacts{}, false, fmt.Errorf("decode entities of %s: %w", row.BlueprintID, err)
	}
	facts := shipFacts{speed: *platform.MaxSpeedKMS, thrust: *platform.ThrustMN, weight: *platform.WeightTons}
	for _, entity := range entities {
		if entity.Name == cargoBay {
			facts.cargoBays += int(entity.Count)
		}
	}
	return facts, true, nil
}

// factoryFactsOf reads a ground build's metrics from its analysis.
func factoryFactsOf(facts analysisresult.Facts) *factoryFacts {
	result := &factoryFacts{
		tiles:              facts.FootprintTiles(),
		pollutionPerMinute: facts.PollutionNetPerSecond * 60,
		netMW:              facts.ElectricityNetWatts / 1e6,
	}
	for name, out := range facts.OutRates {
		if !strings.HasSuffix(name, sciencePackSuffix) {
			continue
		}
		if net := out - facts.InRates[name]; net > 0 {
			result.spm += net * 60
		}
	}
	return result
}

// onlySpaceAge: detected, and nothing but the game's built-in mods.
func onlySpaceAge(detection *mods.Detection) bool {
	if detection == nil || len(detection.UnknownEntities) > 0 || len(detection.UnknownRecipes) > 0 {
		return false
	}
	for _, match := range detection.Modsets {
		if !match.Builtin {
			return false
		}
	}
	return true
}

// rankBoard gives each entry one place with its best leaf (the first in leaf
// order on a tie) and ranks them; ties go to the earlier revision, then the
// entry id.
func rankBoard(spec boardSpec, candidates []boardLeaf) []BoardPlace {
	best := map[string]int{}
	places := []BoardPlace{}
	for _, candidate := range candidates {
		value, ok := spec.value(candidate.facts)
		if !ok {
			continue
		}
		row := candidate.row
		place := BoardPlace{
			Value: value, ownerID: row.OwnerUserID.String,
			revisionCreatedAt: row.RevisionCreatedAt.Time,
			Leaf:              leafRef(row.EntryID, row.EntryTitle, row.RootKind, row.BlueprintID, row.Label, row.Description, row.Icons),
		}
		if index, seen := best[row.EntryID]; seen {
			if spec.better(value, places[index].Value) {
				places[index] = place
			}
			continue
		}
		best[row.EntryID] = len(places)
		places = append(places, place)
	}
	sort.SliceStable(places, func(i, j int) bool {
		a, b := places[i], places[j]
		if a.Value != b.Value {
			return spec.better(a.Value, b.Value)
		}
		if !a.revisionCreatedAt.Equal(b.revisionCreatedAt) {
			return a.revisionCreatedAt.Before(b.revisionCreatedAt)
		}
		return a.Leaf.EntryID < b.Leaf.EntryID
	})
	for i := range places {
		places[i].Rank = i + 1
	}
	return places
}

// RefreshBoardLeaders is the leaderboards task: it ranks the boards afresh,
// stores each shown board's leader (since moves only when the lead changes),
// drops the leaders of hidden boards and awards each leader's owner the
// record (once per board and entry). It returns how many records it awarded.
func (s *Service) RefreshBoardLeaders(ctx context.Context) (int, error) {
	boards, err := s.computeBoards(ctx, s.queries)
	if err != nil {
		return 0, err
	}
	awarded := 0
	err = s.inTransaction(ctx, func(queries *db.Queries) error {
		shown := []string{}
		for _, board := range boards {
			if len(board.places) < BoardMinEntries {
				continue
			}
			leader := board.places[0]
			shown = append(shown, string(board.spec.board))
			if _, err := queries.UpsertBoardLeader(ctx, db.UpsertBoardLeaderParams{
				Board: string(board.spec.board), EntryID: leader.Leaf.EntryID,
				BlueprintID: leader.Leaf.BlueprintID, UserID: pgText(leader.ownerID), Value: leader.Value,
			}); err != nil {
				return fmt.Errorf("store board leader: %w", err)
			}
			written, err := recordXP(ctx, queries, xpRecordHeld{
				board: board.spec.board, ownerID: leader.ownerID, entryID: leader.Leaf.EntryID,
			})
			if err != nil {
				return err
			}
			if written {
				awarded++
			}
		}
		if err := queries.DeleteBoardLeadersExcept(ctx, shown); err != nil {
			return fmt.Errorf("drop board leaders: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	s.boards.put(time.Now(), boards)
	return awarded, nil
}

// BoardRecord is a board a blueprint leads now, as a card or entry page shows
// it (a chip naming the board).
type BoardRecord struct {
	Board       Board      `json:"board" doc:"The board the blueprint leads."`
	Group       BoardGroup `json:"group" doc:"The leaderboard group the board is in."`
	Title       string     `json:"title" doc:"The board's name, such as Fastest ship."`
	BlueprintID string     `json:"blueprint_id" doc:"The leading blueprint."`
}

// currentRecords are the stored leaders (board_leaders) in display order: what
// the leaderboard shows as each board's first place, read once per request
// and matched to cards in Go, so a listing never queries per card and the
// search index never carries them.
type currentRecords []db.BoardLeader

// readCurrentRecords reads every board's stored leader (one row per board).
func readCurrentRecords(ctx context.Context, queries *db.Queries) (currentRecords, error) {
	leaders, err := queries.ListBoardLeaders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list board leaders: %w", err)
	}
	sort.SliceStable(leaders, func(i, j int) bool {
		return boardOrder(Board(leaders[i].Board)) < boardOrder(Board(leaders[j].Board))
	})
	return leaders, nil
}

// ofEntry is the records any blueprint of the entry holds; never nil.
func (records currentRecords) ofEntry(entryID string) []BoardRecord {
	held := []BoardRecord{}
	for _, leader := range records {
		if leader.EntryID == entryID {
			spec := boardSpecOf(Board(leader.Board))
			held = append(held, BoardRecord{
				Board: spec.board, Group: spec.group, Title: spec.title, BlueprintID: leader.BlueprintID,
			})
		}
	}
	return held
}

// LeafRecords is the records of one blueprint among an entry's records; never
// nil.
func LeafRecords(records []BoardRecord, blueprintID string) []BoardRecord {
	held := []BoardRecord{}
	for _, record := range records {
		if record.BlueprintID == blueprintID {
			held = append(held, record)
		}
	}
	return held
}
