package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// Design challenges (docs/api.md#challenges): the list, one challenge's page,
// entering one's own entry, withdrawing it, and votes.

// ChallengePath is the challenge an operation is about.
type ChallengePath struct {
	Slug string `path:"slug" doc:"The challenge's slug."`
}

// ChallengeVotePath is one entry of a challenge, as a vote's target.
type ChallengeVotePath struct {
	ChallengePath
	EntryID string `path:"entryID" doc:"The voted entry's public id (12 characters)."`
}

type challengeListBody struct {
	Challenges []catalog.Challenge `json:"challenges" doc:"The published challenges, newest first."`
}

type challengeListOutput struct {
	Body challengeListBody
}

type challengeDetailOutput struct {
	Body catalog.ChallengeDetail
}

type challengeEntryBody struct {
	EntryID string `json:"entry_id,omitempty" doc:"One of the caller's listed single-blueprint entries. It replaces their earlier entry; an unknown id answers 404."`
}

type challengeEntryInput struct {
	ChallengePath
	Body challengeEntryBody
}

// listChallenges lists every published challenge.
func (s *server) listChallenges(ctx context.Context, _ *struct{}) (*challengeListOutput, error) {
	challenges, err := s.catalog.ListChallenges(ctx)
	if err != nil {
		return nil, err
	}
	return &challengeListOutput{Body: challengeListBody{Challenges: challenges}}, nil
}

// getChallenge reads one challenge's page for the viewer (anonymous or not).
func (s *server) getChallenge(ctx context.Context, in *ChallengePath) (*challengeDetailOutput, error) {
	detail, err := s.catalog.GetChallenge(ctx, in.Slug, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &challengeDetailOutput{Body: detail}, nil
}

// submitChallengeEntry enters entry_id, one of the caller's listed single
// blueprints, replacing their earlier entry.
func (s *server) submitChallengeEntry(ctx context.Context, in *challengeEntryInput) (*struct{}, error) {
	return nil, s.catalog.SubmitChallengeEntry(ctx, in.Slug, viewerFrom(ctx), in.Body.EntryID)
}

// challengeCandidatesInput pages the caller's blueprints that can be entered.
type challengeCandidatesInput struct {
	ChallengePath
	PageQuery
	Query string `query:"q" maxLength:"120" doc:"Only entries whose title contains this text (case-insensitive)."`
}

// listChallengeCandidates lists the caller's public single blueprints.
func (s *server) listChallengeCandidates(ctx context.Context, in *challengeCandidatesInput) (*entryPageOutput, error) {
	entries, total, err := s.catalog.ListChallengeCandidates(
		ctx, viewerFrom(ctx), strings.TrimSpace(in.Query), int32(in.Limit), int32(in.Offset),
	)
	if err != nil {
		return nil, err
	}
	return &entryPageOutput{Body: entryPage{Entries: entries, Total: total}}, nil
}

// withdrawChallengeEntry takes the caller's entry out of the challenge.
func (s *server) withdrawChallengeEntry(ctx context.Context, in *ChallengePath) (*struct{}, error) {
	return nil, s.catalog.WithdrawChallengeEntry(ctx, in.Slug, viewerFrom(ctx))
}

// giveChallengeVote gives a vote on an entry of the challenge (PUT).
func (s *server) giveChallengeVote(ctx context.Context, in *ChallengeVotePath) (*struct{}, error) {
	return nil, s.catalog.VoteChallengeEntry(ctx, in.Slug, viewerFrom(ctx), in.EntryID, true)
}

// removeChallengeVote takes a vote back (DELETE).
func (s *server) removeChallengeVote(ctx context.Context, in *ChallengeVotePath) (*struct{}, error) {
	return nil, s.catalog.VoteChallengeEntry(ctx, in.Slug, viewerFrom(ctx), in.EntryID, false)
}

func (s *server) registerChallenges(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listChallenges", Method: http.MethodGet, Path: "/challenges",
		Tags:        []string{"Challenges"},
		Summary:     "The published challenges",
		Description: "Every published challenge, newest first. Hidden challenges appear once they start.",
	}, accessPublic, limitRead), s.listChallenges)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getChallenge", Method: http.MethodGet, Path: "/challenges/{slug}",
		Tags:    []string{"Challenges"},
		Summary: "One challenge's page",
		Description: "The challenge, its entries (ranked first once scores show) and the viewer's own " +
			"entry, votes and voting rights when signed in.",
		Errors: []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "submitChallengeEntry", Method: http.MethodPost, Path: "/challenges/{slug}/entry",
		Tags:         []string{"Challenges"},
		Summary:      "Enter one of your blueprints in a challenge",
		Description:  "Replaces the caller's earlier entry; entering the same entry again changes nothing.",
		MaxBodyBytes: 1 << 10,
		Errors:       []int{http.StatusConflict, http.StatusForbidden, http.StatusNotFound},
	}, accessRequired, limitWrite), s.submitChallengeEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listChallengeCandidates", Method: http.MethodGet, Path: "/challenges/{slug}/candidates",
		Tags:        []string{"Challenges"},
		Summary:     "Your blueprints that can be entered",
		Description: "Paged, newest first: the caller's public single blueprints, optionally filtered by title.",
		Errors:      []int{http.StatusNotFound},
	}, accessRequired, limitRead), s.listChallengeCandidates)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "withdrawChallengeEntry", Method: http.MethodDelete, Path: "/challenges/{slug}/entry",
		Tags:        []string{"Challenges"},
		Summary:     "Withdraw your entry from a challenge",
		Description: "Only while the challenge is open. With no entry, nothing changes.",
		Errors:      []int{http.StatusConflict, http.StatusNotFound},
	}, accessRequired, limitWrite), s.withdrawChallengeEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "voteChallengeEntry", Method: http.MethodPut, Path: "/challenges/{slug}/votes/{entryID}",
		Tags:        []string{"Challenges"},
		Summary:     "Vote for an entry",
		Description: "Only while voting is open. Voting twice for the same entry changes nothing.",
		Errors:      []int{http.StatusConflict, http.StatusForbidden, http.StatusNotFound},
	}, accessRequired, limitWrite), s.giveChallengeVote)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removeChallengeVote", Method: http.MethodDelete, Path: "/challenges/{slug}/votes/{entryID}",
		Tags:        []string{"Challenges"},
		Summary:     "Take back your vote for an entry",
		Description: "Only while voting is open. Taking back a vote not given changes nothing.",
		Errors:      []int{http.StatusConflict, http.StatusForbidden, http.StatusNotFound},
	}, accessRequired, limitWrite), s.removeChallengeVote)
}
