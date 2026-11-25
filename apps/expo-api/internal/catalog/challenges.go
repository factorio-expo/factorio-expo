package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/challenge"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/analysisresult"
	"factorio-expo/internal/shortid"
)

// Design challenges (docs/api.md#challenges): published by staff
// (staffchallenges.go), entered by signed-in users with one of their listed
// single-blueprint entries, scored from stored facts when read
// (internal/challenge), voted on by the community.

// Challenge statuses, derived from the times.
const (
	ChallengeDraft    = "draft"
	ChallengeUpcoming = "upcoming"
	ChallengeOpen     = "open"
	ChallengeJudging  = "judging"
	ChallengeClosed   = "closed"
)

var (
	// ErrChallengeRefused wraps why a challenge submission, withdrawal or
	// vote was refused; its message is safe to show.
	ErrChallengeRefused = fault.New(fault.Conflict, "challenge_refused", "refused")
	// ErrChallengeForbidden wraps why the caller may not vote or enter at
	// all (banned, a newer account); its message is safe to show.
	ErrChallengeForbidden = fault.New(fault.Forbidden, "challenge_forbidden", "not allowed")
)

func challengeRefused(message string) error {
	return ErrChallengeRefused.Withf("%s", message)
}

func challengeForbidden(message string) error {
	return ErrChallengeForbidden.Withf("%s", message)
}

// Challenge is one challenge as the public API shows it.
type Challenge struct {
	ID           string    `json:"id" doc:"The challenge's id."`
	Slug         string    `json:"slug" doc:"The challenge's URL name."`
	Title        string    `json:"title" doc:"The challenge's title."`
	Summary      string    `json:"summary" doc:"A one-line summary."`
	Description  string    `json:"description" doc:"The story, criteria and examples, in Markdown."`
	Icons        string    `json:"icons" doc:"Signal icon tokens shown with the title."`
	Status       string    `json:"status" doc:"upcoming, open (submissions), judging (votes only) or closed."`
	StartsAt     time.Time `json:"starts_at" doc:"When submissions open."`
	EndsAt       time.Time `json:"ends_at" doc:"The submission deadline."`
	VotingEndsAt time.Time `json:"voting_ends_at" doc:"When voting ends."`
	// Rules are the goal and restrictions in words, with icon tokens.
	Rules               challenge.Description `json:"rules" doc:"The goal and restrictions in words, with icon tokens."`
	RequireNewRevisions bool                  `json:"require_new_revisions" doc:"Whether the scored revision must be made after the start."`
	VotersBeforeStart   bool                  `json:"voters_before_start" doc:"Whether only accounts made before the start may vote."`
	VotingEnabled       bool                  `json:"voting_enabled" doc:"Whether the community votes."`
	VotesPerUser        int32                 `json:"votes_per_user" doc:"How many votes each user has."`
	LiveLeaderboard     bool                  `json:"live_leaderboard" doc:"Whether scores show while the challenge is open."`
	// HiddenUntilStart: not listed or found before it starts.
	HiddenUntilStart bool `json:"hidden_until_start" doc:"Whether the challenge is neither listed nor found before it starts."`
	// EntryCount counts active submissions of listed entries.
	EntryCount int64 `json:"entry_count" doc:"How many active submissions of listed entries it has."`
}

// ChallengeEntry is one submission on the challenge page.
type ChallengeEntry struct {
	SubmissionID string       `json:"submission_id" doc:"The submission's id."`
	Entry        EntrySummary `json:"entry" doc:"The submitted entry's card."`
	Username     string       `json:"username" doc:"The submitter's username."`
	SubmittedAt  time.Time    `json:"submitted_at" doc:"When it was submitted."`
	// Status is pending, accepted or rejected; Reasons say why rejected.
	Status  string   `json:"status" doc:"pending (not analysed yet), accepted or rejected."`
	Reasons []string `json:"reasons" doc:"Why a rejected entry is rejected."`
	// Score, ScoreText and Rank are set for accepted entries once scores
	// show (always after the deadline, while open with a live leaderboard).
	Score     *float64 `json:"score,omitempty" doc:"The goal's value, for an accepted entry once scores show."`
	ScoreText string   `json:"score_text,omitempty" doc:"The score with its unit."`
	Rank      int      `json:"rank,omitempty" doc:"The place by score, for an accepted entry once scores show."`
	// Votes and VoteRank show once voting has ended.
	Votes    *int64 `json:"votes,omitempty" doc:"The votes it got, once voting has ended."`
	VoteRank int    `json:"vote_rank,omitempty" doc:"The place by votes, once voting has ended."`
	// VotedByViewer is whether the viewer gave it one of their votes.
	VotedByViewer bool `json:"voted_by_viewer" doc:"Whether the viewer gave it one of their votes."`
}

// ChallengeViewer is what the signed-in viewer may do on the challenge.
type ChallengeViewer struct {
	// EntryID is the viewer's active submission; empty for none.
	EntryID   string `json:"entry_id" doc:"The viewer's active submission; empty for none."`
	CanSubmit bool   `json:"can_submit" doc:"Whether the viewer may submit an entry now."`
	// CanVote is whether voting is open to them; VoteBlock says why not when
	// voting is open to others.
	CanVote   bool     `json:"can_vote" doc:"Whether voting is open to the viewer."`
	VoteBlock string   `json:"vote_block,omitempty" doc:"Why the viewer cannot vote when voting is open to others."`
	Votes     []string `json:"votes" doc:"The entry ids the viewer voted for."`
	VotesLeft int      `json:"votes_left" doc:"How many votes the viewer has left."`
}

// ChallengeDetail is the challenge page: the challenge, its entries (ranked
// first when scores show) and the viewer's state.
type ChallengeDetail struct {
	Challenge
	ScoresVisible bool             `json:"scores_visible" doc:"Whether scores and ranks are shown."`
	VotesVisible  bool             `json:"votes_visible" doc:"Whether vote counts are shown (once voting has ended)."`
	Entries       []ChallengeEntry `json:"entries" doc:"The submissions, ranked first once scores show."`
	Viewer        *ChallengeViewer `json:"viewer,omitempty" doc:"What the signed-in viewer may do; absent when signed out."`
}

// EntryChallenge is a challenge an entry is submitted to (its page's badge).
type EntryChallenge struct {
	Slug  string `json:"slug" doc:"The challenge's URL name."`
	Title string `json:"title" doc:"The challenge's title."`
}

// ChallengeStatus is the challenge's lifecycle status at now.
func ChallengeStatus(row db.Challenge, now time.Time) string {
	switch {
	case !row.PublishedAt.Valid:
		return ChallengeDraft
	case now.Before(row.StartsAt.Time):
		return ChallengeUpcoming
	case now.Before(row.EndsAt.Time):
		return ChallengeOpen
	case now.Before(row.VotingEndsAt.Time):
		return ChallengeJudging
	}
	return ChallengeClosed
}

// challengePublic: published, and started unless it shows before its start.
func challengePublic(row db.Challenge, now time.Time) bool {
	return row.PublishedAt.Valid && (!row.HiddenUntilStart || !now.Before(row.StartsAt.Time))
}

// votingOpen: votes are taken from the start until voting ends.
func votingOpen(row db.Challenge, now time.Time) bool {
	return row.PublishedAt.Valid && row.VotingEnabled &&
		!now.Before(row.StartsAt.Time) && now.Before(row.VotingEndsAt.Time)
}

// ChallengeRules decodes the challenge's stored rules.
func ChallengeRules(row db.Challenge) (challenge.Rules, error) {
	var rules challenge.Rules
	if err := json.Unmarshal(row.Rules, &rules); err != nil {
		return rules, fmt.Errorf("decode challenge %s rules: %w", row.ID, err)
	}
	return rules, nil
}

// ChallengeOptions are the challenge's check options.
func ChallengeOptions(row db.Challenge) challenge.Options {
	return challenge.Options{StartsAt: row.StartsAt.Time, RequireNewRevisions: row.RequireNewRevisions}
}

func challengeFromRow(row db.Challenge, entryCount int64, now time.Time) (Challenge, error) {
	rules, err := ChallengeRules(row)
	if err != nil {
		return Challenge{}, err
	}
	return Challenge{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Summary: row.Summary,
		Description: row.Description, Icons: row.Icons, Status: ChallengeStatus(row, now),
		StartsAt: row.StartsAt.Time, EndsAt: row.EndsAt.Time, VotingEndsAt: row.VotingEndsAt.Time,
		Rules:               challenge.Describe(rules, ChallengeOptions(row)),
		RequireNewRevisions: row.RequireNewRevisions, VotersBeforeStart: row.VotersBeforeStart,
		VotingEnabled: row.VotingEnabled, VotesPerUser: row.VotesPerUser,
		LiveLeaderboard: row.LiveLeaderboard, HiddenUntilStart: row.HiddenUntilStart, EntryCount: entryCount,
	}, nil
}

// ListChallenges lists every published challenge, the newest first.
func (s *Service) ListChallenges(ctx context.Context) ([]Challenge, error) {
	rows, err := s.queries.ListPublishedChallenges(ctx)
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	now := time.Now()
	challenges := make([]Challenge, 0, len(rows))
	for _, row := range rows {
		item, err := challengeFromRow(publishedChallengeRow(row), row.EntryCount, now)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, item)
	}
	return challenges, nil
}

func publishedChallengeRow(row db.ListPublishedChallengesRow) db.Challenge {
	return db.Challenge{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Summary: row.Summary,
		Description: row.Description, Icons: row.Icons, Rules: row.Rules, StartsAt: row.StartsAt,
		EndsAt: row.EndsAt, VotingEndsAt: row.VotingEndsAt, RequireNewRevisions: row.RequireNewRevisions,
		VotersBeforeStart: row.VotersBeforeStart, VotingEnabled: row.VotingEnabled,
		VotesPerUser: row.VotesPerUser, LiveLeaderboard: row.LiveLeaderboard,
		HiddenUntilStart: row.HiddenUntilStart,
		PublishedAt:      row.PublishedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// publishedChallenge reads a published challenge by slug; drafts, and a
// challenge hidden until it starts, are not found.
func publishedChallenge(ctx context.Context, queries *db.Queries, slug string) (db.Challenge, error) {
	row, err := queries.GetChallengeBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !challengePublic(row, time.Now())) {
		return db.Challenge{}, ErrNotFound
	}
	if err != nil {
		return db.Challenge{}, fmt.Errorf("read challenge: %w", err)
	}
	return row, nil
}

// GetChallenge reads a published challenge's page for viewerUserID ("" when
// signed out).
func (s *Service) GetChallenge(ctx context.Context, slug, viewerUserID string) (ChallengeDetail, error) {
	row, err := publishedChallenge(ctx, s.queries, slug)
	if err != nil {
		return ChallengeDetail{}, err
	}
	now := time.Now()
	scored, err := s.scoreChallenge(ctx, row, now)
	if err != nil {
		return ChallengeDetail{}, err
	}
	status := ChallengeStatus(row, now)
	detail := ChallengeDetail{
		ScoresVisible: status == ChallengeJudging || status == ChallengeClosed ||
			(status == ChallengeOpen && row.LiveLeaderboard),
		VotesVisible: row.VotingEnabled && status == ChallengeClosed,
	}

	var votes []string
	if viewerUserID != "" {
		if votes, err = s.queries.ListUserChallengeVotes(ctx, db.ListUserChallengeVotesParams{
			ChallengeID: row.ID, UserID: viewerUserID,
		}); err != nil {
			return ChallengeDetail{}, fmt.Errorf("list challenge votes: %w", err)
		}
	}
	voted := map[string]bool{}
	for _, entryID := range votes {
		voted[entryID] = true
	}

	ids := make([]string, 0, len(scored))
	for _, item := range scored {
		ids = append(ids, item.entryID)
	}
	cards, err := s.entryCards(ctx, ids, viewerUserID)
	if err != nil {
		return ChallengeDetail{}, err
	}
	cardByID := make(map[string]EntrySummary, len(cards))
	for _, card := range cards {
		cardByID[card.ID] = card
	}
	if !detail.ScoresVisible {
		sort.SliceStable(scored, func(i, j int) bool { return scored[i].submittedAt.Before(scored[j].submittedAt) })
	}
	detail.Entries = make([]ChallengeEntry, 0, len(scored))
	viewerEntry := ""
	accepted := int64(0)
	for _, item := range scored {
		card, ok := cardByID[item.entryID]
		if !ok {
			continue
		}
		own := viewerUserID != "" && item.userID == viewerUserID
		if own {
			viewerEntry = item.entryID
		}
		// Only entries that meet the rules are public; the entrant alone also
		// sees theirs while it is pending or rejected, with the reasons.
		if item.verdict.Status != challenge.StatusAccepted && !own {
			continue
		}
		if item.verdict.Status == challenge.StatusAccepted {
			accepted++
		}
		entry := ChallengeEntry{
			SubmissionID: item.submissionID, Entry: card, Username: item.username,
			SubmittedAt: item.submittedAt, Status: item.verdict.Status, Reasons: item.verdict.Reasons,
			VotedByViewer: voted[item.entryID],
		}
		if detail.ScoresVisible && item.verdict.Score != nil {
			entry.Score = item.verdict.Score
			entry.ScoreText = item.scoreText
			entry.Rank = item.rank
		}
		if detail.VotesVisible {
			votes := item.votes
			entry.Votes = &votes
			entry.VoteRank = item.voteRank
		}
		detail.Entries = append(detail.Entries, entry)
	}

	item, err := challengeFromRow(row, accepted, now)
	if err != nil {
		return ChallengeDetail{}, err
	}
	detail.Challenge = item
	if viewerUserID != "" {
		viewer, err := s.challengeViewer(ctx, row, viewerUserID, now)
		if err != nil {
			return ChallengeDetail{}, err
		}
		viewer.EntryID = viewerEntry
		viewer.Votes = []string{}
		for _, entryID := range votes {
			if _, ok := cardByID[entryID]; ok {
				viewer.Votes = append(viewer.Votes, entryID)
			}
		}
		viewer.VotesLeft = max(0, int(row.VotesPerUser)-len(viewer.Votes))
		if viewer.VotesLeft == 0 && viewer.CanVote {
			viewer.VoteBlock = "You have used all your votes; take one back to vote for another entry."
		}
		detail.Viewer = &viewer
	}
	return detail, nil
}

// challengeViewer is whether the viewer may enter and vote.
func (s *Service) challengeViewer(ctx context.Context, row db.Challenge, userID string, now time.Time) (ChallengeViewer, error) {
	viewer := ChallengeViewer{CanSubmit: ChallengeStatus(row, now) == ChallengeOpen}
	if !votingOpen(row, now) {
		return viewer, nil
	}
	user, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		return viewer, fmt.Errorf("read voter: %w", err)
	}
	if row.VotersBeforeStart && !user.CreatedAt.Time.Before(row.StartsAt.Time) {
		viewer.VoteBlock = "Only accounts made before the challenge started can vote."
		return viewer, nil
	}
	viewer.CanVote = true
	return viewer, nil
}

// scoredEntry is one active submission after the rule check.
type scoredEntry struct {
	submissionID      string
	entryID           string
	userID            string
	username          string
	submittedAt       time.Time
	revisionCreatedAt time.Time
	// blueprintID is the scored revision's blueprint ("" for a book or none).
	blueprintID  string
	verdict      challenge.Verdict
	staffVerdict string
	staffReason  string
	scoreText    string
	rank         int
	votes        int64
	voteRank     int
}

// scoreChallenge is the challenge's scored board (computeChallengeScores),
// from the cache while it is fresh.
func (s *Service) scoreChallenge(ctx context.Context, row db.Challenge, now time.Time) ([]scoredEntry, error) {
	if scored, ok := s.scores.get(row, now); ok {
		return scored, nil
	}
	scored, err := computeChallengeScores(ctx, s.queries, row)
	if err != nil {
		return nil, err
	}
	s.scores.put(row, now, scored)
	return scored, nil
}

// computeChallengeScores reads every active submission and its votes through
// queries (the caller's transaction, when it holds the challenge's lock) and
// scores them (scoreSubmissions); uncached.
func computeChallengeScores(ctx context.Context, queries *db.Queries, row db.Challenge) ([]scoredEntry, error) {
	rules, err := ChallengeRules(row)
	if err != nil {
		return nil, err
	}
	facts, err := queries.ListChallengeSubmissionFacts(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("list challenge submissions: %w", err)
	}
	counts, err := queries.ListChallengeVoteCounts(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("count challenge votes: %w", err)
	}
	votes := make(map[string]int64, len(counts))
	for _, count := range counts {
		votes[count.EntryID] = count.Votes
	}
	return scoreSubmissions(rules, ChallengeOptions(row), facts, votes)
}

// scoreSubmissions checks every active submission of a listed entry against
// the rules (staff verdicts override the check) and ranks them (rankScored).
func scoreSubmissions(
	rules challenge.Rules, options challenge.Options, facts []db.ListChallengeSubmissionFactsRow, votes map[string]int64,
) ([]scoredEntry, error) {
	scored := make([]scoredEntry, 0, len(facts))
	for _, fact := range facts {
		if !fact.Listed {
			continue
		}
		submitted, err := challengeBlueprint(submissionFacts(fact))
		if err != nil {
			return nil, err
		}
		verdict := challenge.Check(rules, options, submitted)
		switch fact.StaffVerdict.String {
		case challenge.StatusRejected:
			reason := fact.StaffReason
			if reason == "" {
				reason = "disqualified by the organizers"
			}
			verdict = challenge.Verdict{Status: challenge.StatusRejected, Reasons: []string{reason}}
		case challenge.StatusAccepted:
			if score, ok := challenge.GoalValue(rules, submitted); ok {
				verdict = challenge.Verdict{Status: challenge.StatusAccepted, Reasons: []string{}, Score: &score}
			}
		}
		item := scoredEntry{
			submissionID: fact.ID, entryID: fact.EntryID, userID: fact.UserID, username: fact.Username,
			submittedAt: fact.SubmittedAt.Time, revisionCreatedAt: fact.RevisionCreatedAt.Time,
			verdict: verdict, votes: votes[fact.EntryID],
			staffVerdict: fact.StaffVerdict.String, staffReason: fact.StaffReason,
		}
		if verdict.Score != nil {
			item.scoreText = challenge.FormatValue(rules.Goal.Param, *verdict.Score)
		}
		scored = append(scored, item)
	}
	rankScored(rules.Goal.Direction, scored)
	return scored, nil
}

// rankScored orders checked submissions and gives them their places, in
// place: accepted first, ranked by the goal (direction; ties: the earlier
// revision, then the earlier submission), then pending, then rejected. The
// vote ranking counts accepted entries with votes only (ties: the earlier
// submission).
func rankScored(direction string, scored []scoredEntry) {
	order := map[string]int{challenge.StatusAccepted: 0, challenge.StatusPending: 1, challenge.StatusRejected: 2}
	sort.SliceStable(scored, func(i, j int) bool {
		a, b := scored[i], scored[j]
		if order[a.verdict.Status] != order[b.verdict.Status] {
			return order[a.verdict.Status] < order[b.verdict.Status]
		}
		if a.verdict.Score != nil && b.verdict.Score != nil && *a.verdict.Score != *b.verdict.Score {
			return challenge.Better(direction, *a.verdict.Score, *b.verdict.Score)
		}
		if !a.revisionCreatedAt.Equal(b.revisionCreatedAt) {
			return a.revisionCreatedAt.Before(b.revisionCreatedAt)
		}
		return a.submittedAt.Before(b.submittedAt)
	})
	for i := range scored {
		scored[i].rank = 0
		if scored[i].verdict.Status == challenge.StatusAccepted {
			scored[i].rank = i + 1
		}
	}

	byVotes := make([]int, 0, len(scored))
	for i := range scored {
		scored[i].voteRank = 0
		if scored[i].verdict.Status == challenge.StatusAccepted {
			byVotes = append(byVotes, i)
		}
	}
	sort.SliceStable(byVotes, func(i, j int) bool {
		a, b := scored[byVotes[i]], scored[byVotes[j]]
		if a.votes != b.votes {
			return a.votes > b.votes
		}
		return a.submittedAt.Before(b.submittedAt)
	})
	for place, index := range byVotes {
		if scored[index].votes > 0 {
			scored[index].voteRank = place + 1
		}
	}
}

// blueprintFacts are the stored facts of one scored revision, whichever query
// read them (a submission's or the test box's entry).
type blueprintFacts struct {
	RootKind                string
	BlueprintID             string
	RevisionCreatedAt       time.Time
	Entities, Recipes, Mods []byte
	ModsVersion             string
	Production              []byte
	ProductionSchemaVersion pgtype.Int4
	ProductionState         pgtype.Text
	HasProduction           bool
	AnalysisJobState        string
}

func submissionFacts(fact db.ListChallengeSubmissionFactsRow) blueprintFacts {
	return blueprintFacts{
		RootKind: fact.RootKind, BlueprintID: fact.BlueprintID, RevisionCreatedAt: fact.RevisionCreatedAt.Time,
		Entities: fact.Entities, Recipes: fact.Recipes, Mods: fact.Mods, ModsVersion: fact.ModsVersion,
		Production: fact.Production, ProductionSchemaVersion: fact.ProductionSchemaVersion,
		ProductionState: fact.ProductionState, HasProduction: fact.HasProduction, AnalysisJobState: fact.AnalysisJobState,
	}
}

// challengeBlueprint turns a revision's stored facts into the rule check's
// input.
func challengeBlueprint(fact blueprintFacts) (challenge.Blueprint, error) {
	b := challenge.Blueprint{
		Single:            fact.RootKind == string(blueprint.KindBlueprint) && fact.BlueprintID != "",
		RevisionCreatedAt: fact.RevisionCreatedAt,
		Entities:          []challenge.EntityCount{},
		Recipes:           []challenge.EntityCount{},
		Analysis:          challenge.AnalysisPending,
	}
	if !b.Single {
		return b, nil
	}
	var entities []blueprint.EntityStat
	if err := json.Unmarshal(fact.Entities, &entities); err != nil {
		return b, fmt.Errorf("decode entities of %s: %w", fact.BlueprintID, err)
	}
	for _, entity := range entities {
		b.Entities = append(b.Entities, challenge.EntityCount{Name: entity.Name, Quality: entity.Quality, Count: int(entity.Count)})
	}
	var recipes []blueprint.RecipeStat
	if err := json.Unmarshal(fact.Recipes, &recipes); err != nil {
		return b, fmt.Errorf("decode recipes of %s: %w", fact.BlueprintID, err)
	}
	for _, recipe := range recipes {
		b.Recipes = append(b.Recipes, challenge.EntityCount{Name: recipe.Name, Quality: recipe.Quality, Count: int(recipe.Count)})
	}
	detection, err := storedMods(db.BlueprintStat{Mods: fact.Mods, ModsVersion: fact.ModsVersion})
	if err != nil {
		return b, fmt.Errorf("decode mods of %s: %w", fact.BlueprintID, err)
	}
	b.Mods = detection

	// The phase rule of progress.LeafOf: an open job is pending, else a
	// stored result is done, else a job failed for good is failed.
	switch {
	case fact.AnalysisJobState == "pending":
	case fact.HasProduction:
		b.Analysis = challenge.AnalysisDone
		b.Partial = fact.ProductionState.String == "partial"
		snapshot, err := analysisresult.ParseSnapshot(fact.Production, fact.ProductionSchemaVersion.Int32)
		if err == nil {
			b.Snapshot = snapshot
		}
	case fact.AnalysisJobState == "failed":
		b.Analysis = challenge.AnalysisFailed
	}
	return b, nil
}

// SubmitChallengeEntry enters one of the user's listed single-blueprint
// entries into an open challenge. It replaces the user's earlier submission;
// entering the same entry again changes nothing.
func (s *Service) SubmitChallengeEntry(ctx context.Context, slug, userID, entryID string) error {
	id, err := parseID(entryID)
	if err != nil {
		return err
	}
	return s.inChange(ctx, func(c *change) error {
		queries := c.queries
		row, err := publishedChallenge(ctx, queries, slug)
		if err != nil {
			return err
		}
		c.challengeChanged(row.ID)
		if ChallengeStatus(row, time.Now()) != ChallengeOpen {
			return challengeRefused("the challenge is not open for entries")
		}
		voter, err := queries.LockVoter(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		if voter.Banned {
			return challengeForbidden("your account is banned")
		}
		entry, err := queries.GetListedEntry(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return challengeRefused("only your own public entries can be entered")
		}
		if err != nil {
			return fmt.Errorf("read entry: %w", err)
		}
		if !entry.OwnerUserID.Valid || entry.OwnerUserID.String != userID {
			return challengeRefused("only your own public entries can be entered")
		}
		revision, err := queries.GetRevision(ctx, entry.CurrentRevisionID.String)
		if err != nil {
			return fmt.Errorf("read revision: %w", err)
		}
		if kind, _ := revisionRootKind(revision.Tree); kind != string(blueprint.KindBlueprint) {
			return challengeRefused("only a single blueprint can be entered, not a book or a planner")
		}
		current, err := queries.ActiveChallengeSubmission(ctx, db.ActiveChallengeSubmissionParams{
			ChallengeID: row.ID, UserID: userID,
		})
		switch {
		case err == nil && current.EntryID == id:
			return nil
		case err == nil:
			if err := queries.WithdrawChallengeSubmission(ctx, db.WithdrawChallengeSubmissionParams{
				ID: current.ID, Reason: pgText("replaced"),
			}); err != nil {
				return fmt.Errorf("replace challenge submission: %w", err)
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("read challenge submission: %w", err)
		}
		if _, err := queries.InsertChallengeSubmission(ctx, db.InsertChallengeSubmissionParams{
			ID: shortid.New(), ChallengeID: row.ID, EntryID: id, UserID: userID,
		}); err != nil {
			return fmt.Errorf("insert challenge submission: %w", err)
		}
		return nil
	})
}

// WithdrawChallengeEntry takes the user's submission out of an open
// challenge; with none, nothing changes.
func (s *Service) WithdrawChallengeEntry(ctx context.Context, slug, userID string) error {
	return s.inChange(ctx, func(c *change) error {
		queries := c.queries
		row, err := publishedChallenge(ctx, queries, slug)
		if err != nil {
			return err
		}
		c.challengeChanged(row.ID)
		if ChallengeStatus(row, time.Now()) != ChallengeOpen {
			return challengeRefused("entries can only be withdrawn while the challenge is open")
		}
		current, err := queries.ActiveChallengeSubmission(ctx, db.ActiveChallengeSubmissionParams{
			ChallengeID: row.ID, UserID: userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read challenge submission: %w", err)
		}
		if err := queries.WithdrawChallengeSubmission(ctx, db.WithdrawChallengeSubmissionParams{
			ID: current.ID, Reason: pgText("user"),
		}); err != nil {
			return fmt.Errorf("withdraw challenge submission: %w", err)
		}
		return nil
	})
}

// VoteChallengeEntry gives (vote) or takes back one of the user's votes on an
// entry of the challenge, while voting is open. Giving a vote twice or taking
// back a vote not given changes nothing.
func (s *Service) VoteChallengeEntry(ctx context.Context, slug, userID, entryID string, vote bool) error {
	id, err := parseID(entryID)
	if err != nil {
		return err
	}
	return s.inChange(ctx, func(c *change) error {
		queries := c.queries
		row, err := publishedChallenge(ctx, queries, slug)
		if err != nil {
			return err
		}
		c.challengeChanged(row.ID)
		now := time.Now()
		if !votingOpen(row, now) {
			return challengeRefused("voting is not open")
		}
		voter, err := queries.LockVoter(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		if err != nil {
			return fmt.Errorf("lock voter: %w", err)
		}
		key := db.DeleteChallengeVoteParams{ChallengeID: row.ID, UserID: userID, EntryID: id}
		if !vote {
			if err := queries.DeleteChallengeVote(ctx, key); err != nil {
				return fmt.Errorf("delete challenge vote: %w", err)
			}
			return nil
		}
		if voter.Banned {
			return challengeForbidden("your account is banned")
		}
		if row.VotersBeforeStart && !voter.CreatedAt.Time.Before(row.StartsAt.Time) {
			return challengeForbidden("only accounts made before the challenge started can vote")
		}
		submission, err := queries.ActiveChallengeSubmissionOfEntry(ctx, db.ActiveChallengeSubmissionOfEntryParams{
			ChallengeID: row.ID, EntryID: id,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return challengeRefused("this entry is not in the challenge")
		}
		if err != nil {
			return fmt.Errorf("read challenge submission: %w", err)
		}
		if submission.UserID == userID {
			return challengeRefused("you cannot vote for your own entry")
		}
		if _, err := queries.GetListedEntry(ctx, id); errors.Is(err, pgx.ErrNoRows) {
			return challengeRefused("this entry is not in the challenge")
		} else if err != nil {
			return fmt.Errorf("read entry: %w", err)
		}
		given, err := queries.ListUserChallengeVotes(ctx, db.ListUserChallengeVotesParams{ChallengeID: row.ID, UserID: userID})
		if err != nil {
			return fmt.Errorf("list challenge votes: %w", err)
		}
		if slices.Contains(given, id) {
			return nil
		}
		used, err := queries.CountUserChallengeVotes(ctx, db.CountUserChallengeVotesParams{ChallengeID: row.ID, UserID: userID})
		if err != nil {
			return fmt.Errorf("count challenge votes: %w", err)
		}
		if used >= int64(row.VotesPerUser) {
			return challengeRefused(fmt.Sprintf("you have used all %d votes; take one back first", row.VotesPerUser))
		}
		if err := queries.InsertChallengeVote(ctx, db.InsertChallengeVoteParams(key)); err != nil {
			return fmt.Errorf("insert challenge vote: %w", err)
		}
		return nil
	})
}

// entryChallenges lists the published challenges an entry is in.
func entryChallenges(ctx context.Context, queries *db.Queries, entryID string) ([]EntryChallenge, error) {
	rows, err := queries.ListEntryChallenges(ctx, entryID)
	if err != nil {
		return nil, fmt.Errorf("list entry challenges: %w", err)
	}
	challenges := make([]EntryChallenge, 0, len(rows))
	for _, row := range rows {
		challenges = append(challenges, EntryChallenge{Slug: row.Slug, Title: row.Title})
	}
	return challenges, nil
}

// revisionRootKind reads the root node's kind from a revision's tree.
func revisionRootKind(tree []byte) (string, error) {
	var root struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(tree, &root); err != nil {
		return "", err
	}
	return root.Kind, nil
}

// ChallengeStanding is one active submission of a challenge after the rule
// check, as the console lists it: in rank order, accepted entries first.
type ChallengeStanding struct {
	SubmissionID string
	EntryID      string
	// Title is the entry's title.
	Title        string
	Username     string
	SubmittedAt  time.Time
	Verdict      challenge.Verdict
	ScoreText    string
	Rank         int
	Votes        int64
	StaffVerdict string
	StaffReason  string
}

// ChallengeStandings checks every active submission of the challenge against
// its rules and ranks them (scoreChallenge), with the entries' titles.
func (s *Service) ChallengeStandings(ctx context.Context, row db.Challenge, now time.Time) ([]ChallengeStanding, error) {
	// Never from the cache: staff act on what they see.
	scored, err := computeChallengeScores(ctx, s.queries, row)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(scored))
	for _, item := range scored {
		ids = append(ids, item.entryID)
	}
	cards, err := s.entryCards(ctx, ids, "")
	if err != nil {
		return nil, err
	}
	titles := make(map[string]string, len(cards))
	for _, card := range cards {
		titles[card.ID] = card.Title
	}
	standings := make([]ChallengeStanding, 0, len(scored))
	for _, entry := range scored {
		standings = append(standings, ChallengeStanding{
			SubmissionID: entry.submissionID, EntryID: entry.entryID, Title: titles[entry.entryID],
			Username: entry.username, SubmittedAt: entry.submittedAt, Verdict: entry.verdict,
			ScoreText: entry.scoreText, Rank: entry.rank, Votes: entry.votes,
			StaffVerdict: entry.staffVerdict, StaffReason: entry.staffReason,
		})
	}
	return standings, nil
}
