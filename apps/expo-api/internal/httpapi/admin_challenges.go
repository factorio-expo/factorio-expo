package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/challenge"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// Challenges (catalog/staffchallenges.go): the list, one challenge with its
// checked entries, create, edit, publish, delete, and submission overrides.

// StaffChallengePath names one challenge.
type StaffChallengePath struct {
	ChallengeID string `path:"challengeID" doc:"The challenge's id."`
}

// StaffSubmissionPath names one submission of a challenge.
type StaffSubmissionPath struct {
	StaffChallengePath
	SubmissionID string `path:"submissionID" doc:"The submission's id."`
}

// adminChallengeFields is a challenge write's body. Its fields are optional
// in the schema: the catalog answers a missing or invalid one with its own
// message (ChallengeFields.clean), as it always has.
type adminChallengeFields struct {
	Slug                string          `json:"slug,omitempty" doc:"The URL name: lowercase letters, digits and dashes, at most 80."`
	Title               string          `json:"title,omitempty" doc:"The title, 1 to 120 characters."`
	Summary             string          `json:"summary,omitempty" doc:"A short summary, at most 300 characters."`
	Description         string          `json:"description,omitempty" doc:"The description, at most 10000 characters."`
	Icons               string          `json:"icons,omitempty" doc:"The icons shown with the title, at most 500 characters."`
	Rules               challenge.Rules `json:"rules" doc:"What ranks the entries: the content, the kind, the goal and the restrictions."`
	StartsAt            time.Time       `json:"starts_at" doc:"When submissions open (RFC 3339)."`
	EndsAt              time.Time       `json:"ends_at" doc:"The submission deadline (RFC 3339), after the start."`
	VotingEndsAt        time.Time       `json:"voting_ends_at" doc:"When voting ends (RFC 3339), not before the deadline."`
	RequireNewRevisions bool            `json:"require_new_revisions,omitempty" doc:"Only revisions made after the start rank."`
	VotersBeforeStart   bool            `json:"voters_before_start,omitempty" doc:"Users may vote before the start."`
	VotingEnabled       bool            `json:"voting_enabled,omitempty" doc:"Users may vote on entries."`
	VotesPerUser        int32           `json:"votes_per_user,omitempty" doc:"Votes each user has, 1 to 20."`
	LiveLeaderboard     bool            `json:"live_leaderboard,omitempty" doc:"The leaderboard is shown before the deadline."`
	HiddenUntilStart    bool            `json:"hidden_until_start,omitempty" doc:"The challenge is hidden until it starts."`
	ConfirmRescore      bool            `json:"confirm_rescore,omitempty" doc:"Confirms changing the rules of a started challenge, which rescores every entry."`
}

// adminSubmissionVerdictBody overrides a submission's rule check. Both fields
// are optional in the schema: the catalog checks them.
type adminSubmissionVerdictBody struct {
	Verdict string `json:"verdict,omitempty" doc:"accepted or rejected; empty goes back to the rule check."`
	Reason  string `json:"reason,omitempty" doc:"Why. A verdict needs a reason."`
}

// staffExchangeBodyLimit bounds a pasted exchange string (the test box).
const staffExchangeBodyLimit = 1 << 20

// StaffTestPath names a test blueprint (a quick preview entry) of a challenge.
type StaffTestPath struct {
	StaffChallengePath
	EntryID string `path:"entryID" doc:"The test blueprint's entry id."`
}

// adminVoidVotesBody voids or restores a voter's votes.
type adminVoidVotesBody struct {
	UserID  string `json:"user_id" doc:"The voter's account id."`
	EntryID string `json:"entry_id,omitempty" doc:"Only the vote on this entry; empty for every vote of the voter in the challenge."`
	Reason  string `json:"reason,omitempty" doc:"Why. Voiding needs a reason; restoring does not."`
}

type adminVoidVotesInput struct {
	StaffChallengePath
	Body adminVoidVotesBody
}

type adminVoidVotesResponse struct {
	Changed int64 `json:"changed" doc:"How many votes changed."`
}

type adminVoidVotesOutput struct {
	Body adminVoidVotesResponse
}

type adminOversightOutput struct {
	Body staffops.VoteOversight
}

type adminTestStartBody struct {
	ExchangeString string `json:"exchange_string" doc:"The blueprint's exchange string (a single blueprint, not a book)."`
}

type adminTestStartInput struct {
	StaffChallengePath
	Body adminTestStartBody
}

type adminTestStartResponse struct {
	EntryID string `json:"entry_id" doc:"The quick preview entry the blueprint runs as; read its check until it is no longer pending."`
}

type adminTestStartOutput struct {
	Body adminTestStartResponse
}

type adminTestResultOutput struct {
	Body staffops.ChallengeTestResult
}

type adminChallengeListResponse struct {
	Challenges []staffops.StaffChallenge `json:"challenges"`
}

type adminChallengeListOutput struct {
	Body adminChallengeListResponse
}

type adminChallengeParamsResponse struct {
	Params []challenge.Param `json:"params"`
}

type adminChallengeParamsOutput struct {
	Body adminChallengeParamsResponse
}

type adminChallengeOutput struct {
	Body staffops.StaffChallengeDetail
}

type adminChallengeCreatedResponse struct {
	ID string `json:"id" doc:"The new draft challenge's id."`
}

type adminChallengeCreatedOutput struct {
	Body adminChallengeCreatedResponse
}

type adminCreateChallengeInput struct {
	Body adminChallengeFields
}

type adminUpdateChallengeInput struct {
	StaffChallengePath
	Body adminChallengeFields
}

type adminSetVerdictInput struct {
	StaffSubmissionPath
	Body adminSubmissionVerdictBody
}

// listAdminChallenges lists every challenge with its entry count.
func (s *adminServer) listAdminChallenges(ctx context.Context, _ *struct{}) (*adminChallengeListOutput, error) {
	challenges, err := s.ops.ListStaffChallenges(ctx)
	if err != nil {
		return nil, err
	}
	return &adminChallengeListOutput{Body: adminChallengeListResponse{Challenges: challenges}}, nil
}

// getChallengeParams lists the parameters a rule can measure.
func (s *adminServer) getChallengeParams(context.Context, *struct{}) (*adminChallengeParamsOutput, error) {
	return &adminChallengeParamsOutput{Body: adminChallengeParamsResponse{Params: s.ops.ChallengeParams()}}, nil
}

// getAdminChallenge is one challenge with its checked entries.
func (s *adminServer) getAdminChallenge(ctx context.Context, in *StaffChallengePath) (*adminChallengeOutput, error) {
	detail, err := s.ops.GetStaffChallenge(ctx, in.ChallengeID)
	if err != nil {
		return nil, err
	}
	return &adminChallengeOutput{Body: detail}, nil
}

func (s *adminServer) createChallenge(ctx context.Context, in *adminCreateChallengeInput) (*adminChallengeCreatedOutput, error) {
	id, err := s.ops.CreateChallenge(ctx, staffUser(ctx), staffops.ChallengeFields(in.Body))
	if err != nil {
		return nil, err
	}
	return &adminChallengeCreatedOutput{Body: adminChallengeCreatedResponse{ID: id}}, nil
}

func (s *adminServer) updateChallenge(ctx context.Context, in *adminUpdateChallengeInput) (*struct{}, error) {
	return nil, s.ops.UpdateChallenge(ctx, staffUser(ctx), in.ChallengeID, staffops.ChallengeFields(in.Body))
}

func (s *adminServer) publishChallenge(ctx context.Context, in *StaffChallengePath) (*struct{}, error) {
	return nil, s.ops.PublishChallenge(ctx, staffUser(ctx), in.ChallengeID, true)
}

func (s *adminServer) unpublishChallenge(ctx context.Context, in *StaffChallengePath) (*struct{}, error) {
	return nil, s.ops.PublishChallenge(ctx, staffUser(ctx), in.ChallengeID, false)
}

func (s *adminServer) deleteChallenge(ctx context.Context, in *StaffChallengePath) (*struct{}, error) {
	return nil, s.ops.DeleteChallenge(ctx, staffUser(ctx), in.ChallengeID)
}

func (s *adminServer) setSubmissionVerdict(ctx context.Context, in *adminSetVerdictInput) (*struct{}, error) {
	return nil, s.ops.SetSubmissionVerdict(
		ctx, staffUser(ctx), in.ChallengeID, in.SubmissionID, in.Body.Verdict, in.Body.Reason,
	)
}

func (s *adminServer) getChallengeOversight(ctx context.Context, in *StaffChallengePath) (*adminOversightOutput, error) {
	oversight, err := s.ops.ChallengeOversight(ctx, in.ChallengeID)
	if err != nil {
		return nil, err
	}
	return &adminOversightOutput{Body: oversight}, nil
}

func (s *adminServer) voidVotes(ctx context.Context, in *adminVoidVotesInput, void bool) (*adminVoidVotesOutput, error) {
	changed, err := s.ops.VoidChallengeVotes(
		ctx, staffUser(ctx), in.ChallengeID, in.Body.UserID, in.Body.EntryID, void, in.Body.Reason,
	)
	if err != nil {
		return nil, err
	}
	return &adminVoidVotesOutput{Body: adminVoidVotesResponse{Changed: changed}}, nil
}

func (s *adminServer) startChallengeTest(ctx context.Context, in *adminTestStartInput) (*adminTestStartOutput, error) {
	entryID, err := s.ops.StartChallengeTest(ctx, staffUser(ctx), in.ChallengeID, in.Body.ExchangeString)
	if err != nil {
		return nil, err
	}
	return &adminTestStartOutput{Body: adminTestStartResponse{EntryID: entryID}}, nil
}

func (s *adminServer) getChallengeTest(ctx context.Context, in *StaffTestPath) (*adminTestResultOutput, error) {
	result, err := s.ops.TestChallengeEntry(ctx, in.ChallengeID, in.EntryID)
	if err != nil {
		return nil, err
	}
	return &adminTestResultOutput{Body: result}, nil
}

func (s *adminServer) registerAdminChallenges(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listAdminChallenges", Method: http.MethodGet, Path: "/admin/challenges",
		Tags: []string{"Challenges"}, Summary: "Every challenge with its entry count",
	}), s.listAdminChallenges)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getChallengeParams", Method: http.MethodGet, Path: "/admin/challenges/params",
		Tags: []string{"Challenges"}, Summary: "The parameters a challenge rule can measure",
	}), s.getChallengeParams)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createChallenge", Method: http.MethodPost, Path: "/admin/challenges",
		Tags: []string{"Challenges"}, Summary: "Create a draft challenge",
		DefaultStatus: http.StatusCreated, MaxBodyBytes: staffWriteBodyLimit,
	}), s.createChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getAdminChallenge", Method: http.MethodGet, Path: "/admin/challenges/{challengeID}",
		Tags: []string{"Challenges"}, Summary: "One challenge with its checked entries",
		Errors: []int{http.StatusNotFound},
	}), s.getAdminChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateChallenge", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}",
		Tags: []string{"Challenges"}, Summary: "Edit a challenge",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), s.updateChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "publishChallenge", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}/publish",
		Tags: []string{"Challenges"}, Summary: "Publish a challenge",
		Errors: []int{http.StatusNotFound},
	}), s.publishChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "unpublishChallenge", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}/unpublish",
		Tags: []string{"Challenges"}, Summary: "Hide a challenge that has not started",
		Errors: []int{http.StatusNotFound},
	}), s.unpublishChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteChallenge", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}/delete",
		Tags: []string{"Challenges"}, Summary: "Delete a challenge that was never published",
		Errors: []int{http.StatusNotFound},
	}), s.deleteChallenge)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "setSubmissionVerdict", Method: http.MethodPost,
		Path:         "/admin/challenges/{challengeID}/submissions/{submissionID}/verdict",
		Tags:         []string{"Challenges"},
		Summary:      "Override a submission's rule check",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), s.setSubmissionVerdict)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getChallengeOversight", Method: http.MethodGet, Path: "/admin/challenges/{challengeID}/oversight",
		Tags: []string{"Challenges"}, Summary: "A challenge's voters and entries with the signs of vote farming",
		Errors: []int{http.StatusNotFound},
	}), s.getChallengeOversight)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "voidChallengeVotes", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}/votes/void",
		Tags: []string{"Challenges"}, Summary: "Void a voter's votes in a challenge",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), func(ctx context.Context, in *adminVoidVotesInput) (*adminVoidVotesOutput, error) {
		return s.voidVotes(ctx, in, true)
	})
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "unvoidChallengeVotes", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}/votes/unvoid",
		Tags: []string{"Challenges"}, Summary: "Count a voter's voided votes again",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), func(ctx context.Context, in *adminVoidVotesInput) (*adminVoidVotesOutput, error) {
		return s.voidVotes(ctx, in, false)
	})
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "startChallengeTest", Method: http.MethodPost, Path: "/admin/challenges/{challengeID}/test",
		Tags: []string{"Challenges"}, Summary: "Run a pasted blueprint as a test against a challenge's rules",
		DefaultStatus: http.StatusCreated, MaxBodyBytes: staffExchangeBodyLimit, Errors: []int{http.StatusNotFound},
	}), s.startChallengeTest)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getChallengeTest", Method: http.MethodGet, Path: "/admin/challenges/{challengeID}/test/{entryID}",
		Tags: []string{"Challenges"}, Summary: "Check a test blueprint (or any entry) against a challenge's rules",
		Errors: []int{http.StatusNotFound},
	}), s.getChallengeTest)
}
