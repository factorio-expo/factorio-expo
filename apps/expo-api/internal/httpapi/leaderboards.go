package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// The leaderboard (docs/api.md#leaderboards) and the home page's picks
// (docs/api.md#home-picks), both showing leaves as search result cards, and
// the XP and badge rules (docs/api.md#xp-rules).

type leaderboardsInput struct {
	Limit int `query:"limit" minimum:"1" maximum:"50" default:"10" doc:"How many places each board returns."`
}

type leaderboardInput struct {
	Board catalog.Board `path:"board" doc:"The board."`
	Limit int           `query:"limit" minimum:"1" maximum:"50" default:"10" doc:"How many places to return."`
}

// boardPlaceBody is one place on a board.
type boardPlaceBody struct {
	Rank  int                      `json:"rank" doc:"The place, from 1."`
	Value float64                  `json:"value" doc:"The blueprint's value on the board, in the board's unit."`
	Card  catalog.SearchResultCard `json:"card" doc:"The leading blueprint of the entry, as a card."`
}

// leaderboardBody is one board.
type leaderboardBody struct {
	Board       catalog.Board      `json:"board" doc:"The board."`
	Group       catalog.BoardGroup `json:"group" doc:"The group the board shows under: ships or factories."`
	Title       string             `json:"title" doc:"The board's name."`
	Description string             `json:"description" doc:"What wins the board, in one line."`
	Unit        string             `json:"unit" doc:"The unit of the values."`
	LowerWins   bool               `json:"lower_wins" doc:"Whether the smallest value leads (the lightest ship)."`
	MinEntries  int                `json:"min_entries" doc:"How many entries the board needs before it shows."`
	Eligible    int                `json:"eligible" doc:"How many entries rank on it."`
	Visible     bool               `json:"visible" doc:"Whether the board shows: it has at least min_entries entries."`
	// LeaderSince is set once the minute task recorded the current leader.
	LeaderSince *time.Time       `json:"leader_since,omitempty" doc:"Since when the leader holds the record; absent until it is recorded (within a minute)."`
	Places      []boardPlaceBody `json:"places" doc:"The top places, best first; empty while the board is hidden."`
}

type leaderboardsBody struct {
	Boards []leaderboardBody `json:"boards" doc:"Every board, in display order."`
}

type leaderboardsOutput struct {
	Body leaderboardsBody
}

type leaderboardOutput struct {
	Body leaderboardBody
}

func (s *server) listLeaderboards(ctx context.Context, in *leaderboardsInput) (*leaderboardsOutput, error) {
	boards, err := s.catalog.Leaderboards(ctx, in.Limit)
	if err != nil {
		return nil, err
	}
	body := leaderboardsBody{Boards: make([]leaderboardBody, 0, len(boards))}
	for _, board := range boards {
		view, err := s.leaderboardView(ctx, board)
		if err != nil {
			return nil, err
		}
		body.Boards = append(body.Boards, view)
	}
	return &leaderboardsOutput{Body: body}, nil
}

func (s *server) getLeaderboard(ctx context.Context, in *leaderboardInput) (*leaderboardOutput, error) {
	board, err := s.catalog.Leaderboard(ctx, in.Board, in.Limit)
	if err != nil {
		return nil, err
	}
	view, err := s.leaderboardView(ctx, board)
	if err != nil {
		return nil, err
	}
	return &leaderboardOutput{Body: view}, nil
}

// leaderboardView adds the cards; a place whose entry stopped showing since
// the board was ranked is left out.
func (s *server) leaderboardView(ctx context.Context, board catalog.Leaderboard) (leaderboardBody, error) {
	view := leaderboardBody{
		Board: board.Board, Group: board.Group, Title: board.Title, Description: board.Description,
		Unit: board.Unit, LowerWins: board.LowerWins,
		MinEntries: catalog.BoardMinEntries, Eligible: board.Eligible, Visible: board.Visible,
		LeaderSince: board.LeaderSince, Places: make([]boardPlaceBody, 0, len(board.Places)),
	}
	refs := make([]catalog.LeafRef, 0, len(board.Places))
	for _, place := range board.Places {
		refs = append(refs, place.Leaf)
	}
	cards, err := s.leafCards(ctx, refs)
	if err != nil {
		return leaderboardBody{}, err
	}
	for _, place := range board.Places {
		card, ok := cards[place.Leaf.BlueprintID]
		if !ok {
			continue
		}
		view.Places = append(view.Places, boardPlaceBody{Rank: place.Rank, Value: place.Value, Card: card})
	}
	return view, nil
}

// homePickBody is one hand-picked leaf.
type homePickBody struct {
	Caption  string                   `json:"caption" doc:"Why staff picked it; empty for none."`
	PickedAt time.Time                `json:"picked_at" doc:"When it was picked."`
	Card     catalog.SearchResultCard `json:"card" doc:"The picked blueprint, as a card."`
}

type homePicksBody struct {
	Picks []homePickBody `json:"picks" doc:"The picks, newest first (at most six)."`
}

type homePicksOutput struct {
	Body homePicksBody
}

func (s *server) listHomePicks(ctx context.Context, _ *struct{}) (*homePicksOutput, error) {
	picks, err := s.catalog.ListHomePicks(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]catalog.LeafRef, 0, len(picks))
	for _, pick := range picks {
		refs = append(refs, pick.Leaf)
	}
	cards, err := s.leafCards(ctx, refs)
	if err != nil {
		return nil, err
	}
	body := homePicksBody{Picks: make([]homePickBody, 0, len(picks))}
	for _, pick := range picks {
		if card, ok := cards[pick.Leaf.BlueprintID]; ok {
			body.Picks = append(body.Picks, homePickBody{Caption: pick.Caption, PickedAt: pick.PickedAt, Card: card})
		}
	}
	return &homePicksOutput{Body: body}, nil
}

// leafCards are the leaves' cards keyed by blueprint id (catalog.NodeCards):
// a leaf whose entry is no longer listed, or that left the current revision,
// has none.
func (s *server) leafCards(ctx context.Context, refs []catalog.LeafRef) (map[string]catalog.SearchResultCard, error) {
	nodes := make([]catalog.CardNode, 0, len(refs))
	for _, ref := range refs {
		nodes = append(nodes, ref.CardNode())
	}
	list, err := s.catalog.NodeCards(ctx, nodes, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	cards := make(map[string]catalog.SearchResultCard, len(list))
	for _, card := range list {
		cards[card.NodeID] = card
	}
	return cards, nil
}

type xpRulesOutput struct {
	Body catalog.XPRules
}

func (s *server) getXPRules(context.Context, *struct{}) (*xpRulesOutput, error) {
	return &xpRulesOutput{Body: catalog.Rules()}, nil
}

// registerLeaderboards registers this file's operations.
func (s *server) registerLeaderboards(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listLeaderboards", Method: http.MethodGet, Path: "/leaderboards",
		Tags:    []string{"Leaderboards"},
		Summary: "Leaderboard",
		Description: "The record boards, ranked from the stored analysis, in two groups. Ships (space platforms that move): " +
			"fastest, highest thrust-to-weight, lightest, most cargo bays. Factories (blueprints that are not space " +
			"platforms): most science per minute, densest and cleanest science (from 60 SPM), biggest power plant " +
			"(net electricity). Only complete analyses of blueprints that need no mod but Space Age's rank, one place " +
			"per entry. A board shows once it has min_entries entries.",
	}, accessOptional, limitRead), s.listLeaderboards)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getLeaderboard", Method: http.MethodGet, Path: "/leaderboards/{board}",
		Tags:        []string{"Leaderboards"},
		Summary:     "One leaderboard board",
		Description: "One board, as in the list of boards.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getLeaderboard)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listHomePicks", Method: http.MethodGet, Path: "/home-picks",
		Tags:        []string{"Site"},
		Summary:     "Hand-picked blueprints",
		Description: "The blueprints staff picked for the home page, newest first, each with why. A pick whose blueprint left the current revision of a public entry is left out.",
	}, accessOptional, limitRead), s.listHomePicks)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getXPRules", Method: http.MethodGet, Path: "/xp-rules",
		Tags:    []string{"Site"},
		Summary: "XP and badge rules",
		Description: "Every way to earn XP with its points and daily cap (UTC days), the XP each level starts at, and " +
			"what earns each badge: the same constants the awards use. Points never affect search or any ranking.",
	}, accessOptional, limitRead), s.getXPRules)
}
