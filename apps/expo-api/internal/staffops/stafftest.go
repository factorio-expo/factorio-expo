package staffops

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/challenge"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The challenge editor's "test a blueprint" box (docs/admin.md#challenges):
// staff paste an exchange string, it runs as an ordinary quick preview (so it
// is rendered and analysed like an upload, lives a day, is never listed or
// entered), and its stored facts are checked against the challenge's rules,
// draft or live, line by line.

// ChallengeTestResult is the check of one blueprint against a challenge.
type ChallengeTestResult struct {
	EntryID string `json:"entry_id" doc:"The quick preview entry the blueprint runs as."`
	// Status is pending (the analysis has not finished), accepted or rejected.
	Status    string   `json:"status" enum:"pending,accepted,rejected" doc:"pending while the analysis runs, then accepted or rejected."`
	Reasons   []string `json:"reasons" doc:"Why it is rejected."`
	ScoreText string   `json:"score_text" doc:"The goal's value for an accepted blueprint."`
	// Lines are every restriction and the goal with the value measured.
	Lines []challenge.Measurement `json:"lines" doc:"Every restriction and the goal with the value measured on the blueprint."`
}

// StartChallengeTest runs an exchange string as a quick preview for the test
// box and returns its entry id; poll TestChallengeEntry until it is not
// pending. Books are refused. The challenge only has to exist.
func (s *Service) StartChallengeTest(ctx context.Context, by audit.Staff, challengeID, exchange string) (string, error) {
	id, err := parseID(challengeID)
	if err != nil {
		return "", err
	}
	if _, err := s.queries.GetChallenge(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return "", catalog.ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("read challenge: %w", err)
	}
	// An anonymous quick preview: nobody owns it, and the staff test queue
	// shares the anonymous cap.
	entry, err := s.catalog.CreateQuickPreview(ctx, exchange, "", nil)
	if err != nil {
		return "", err
	}
	err = s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "challenge.test", TargetType: "challenge", TargetID: id,
			After: map[string]any{"entry_id": entry.ID},
		})
	})
	if err != nil {
		return "", err
	}
	return entry.ID, nil
}

// TestChallengeEntry checks an entry's current revision against the
// challenge's rules as they are now. Scoring ignores the new-revisions option
// (a test blueprint is new by definition).
func (s *Service) TestChallengeEntry(ctx context.Context, challengeID, entryID string) (ChallengeTestResult, error) {
	id, err := parseID(challengeID)
	if err != nil {
		return ChallengeTestResult{}, err
	}
	row, err := s.queries.GetChallenge(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChallengeTestResult{}, catalog.ErrNotFound
	}
	if err != nil {
		return ChallengeTestResult{}, fmt.Errorf("read challenge: %w", err)
	}
	rules, err := catalog.ChallengeRules(row)
	if err != nil {
		return ChallengeTestResult{}, err
	}
	submitted, err := s.catalog.ChallengeTestBlueprint(ctx, entryID)
	if err != nil {
		return ChallengeTestResult{}, err
	}
	options := catalog.ChallengeOptions(row)
	options.RequireNewRevisions = false
	return testResult(entryID, rules, options, submitted), nil
}

// testResult is the check and the measurements of one blueprint.
func testResult(entryID string, rules challenge.Rules, options challenge.Options, b challenge.Blueprint) ChallengeTestResult {
	verdict := challenge.Check(rules, options, b)
	result := ChallengeTestResult{
		EntryID: entryID, Status: verdict.Status, Reasons: verdict.Reasons, Lines: challenge.Measure(rules, b),
	}
	if result.Reasons == nil {
		result.Reasons = []string{}
	}
	if verdict.Score != nil {
		result.ScoreText = challenge.FormatValue(rules.Goal.Param, *verdict.Score)
	}
	return result
}
