package staffops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/challenge"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// Staff console -> Challenges (docs/admin.md): create, edit, publish and
// delete challenges, and override a submission's rule check. Every write
// records an admin_actions row.

var challengeSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ChallengeFields are a challenge's editable fields.
type ChallengeFields struct {
	Slug                string          `json:"slug"`
	Title               string          `json:"title"`
	Summary             string          `json:"summary"`
	Description         string          `json:"description"`
	Icons               string          `json:"icons"`
	Rules               challenge.Rules `json:"rules"`
	StartsAt            time.Time       `json:"starts_at"`
	EndsAt              time.Time       `json:"ends_at"`
	VotingEndsAt        time.Time       `json:"voting_ends_at"`
	RequireNewRevisions bool            `json:"require_new_revisions"`
	VotersBeforeStart   bool            `json:"voters_before_start"`
	VotingEnabled       bool            `json:"voting_enabled"`
	VotesPerUser        int32           `json:"votes_per_user"`
	LiveLeaderboard     bool            `json:"live_leaderboard"`
	HiddenUntilStart    bool            `json:"hidden_until_start"`
	// ConfirmRescore confirms changing the rules of a started challenge,
	// which rescores every entry.
	ConfirmRescore bool `json:"confirm_rescore"`
}

// StaffChallenge is a challenge as the console shows it.
type StaffChallenge struct {
	ID          string                `json:"id"`
	Slug        string                `json:"slug"`
	Title       string                `json:"title"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Icons       string                `json:"icons"`
	Status      string                `json:"status"`
	Rules       challenge.Rules       `json:"rules"`
	RulesText   challenge.Description `json:"rules_text"`

	StartsAt            time.Time  `json:"starts_at"`
	EndsAt              time.Time  `json:"ends_at"`
	VotingEndsAt        time.Time  `json:"voting_ends_at"`
	RequireNewRevisions bool       `json:"require_new_revisions"`
	VotersBeforeStart   bool       `json:"voters_before_start"`
	VotingEnabled       bool       `json:"voting_enabled"`
	VotesPerUser        int32      `json:"votes_per_user"`
	LiveLeaderboard     bool       `json:"live_leaderboard"`
	HiddenUntilStart    bool       `json:"hidden_until_start"`
	PublishedAt         *time.Time `json:"published_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	EntryCount          int64      `json:"entry_count"`
}

// StaffChallengeEntry is one active submission with its check.
type StaffChallengeEntry struct {
	SubmissionID string    `json:"submission_id"`
	EntryID      string    `json:"entry_id"`
	Title        string    `json:"title"`
	Username     string    `json:"username"`
	SubmittedAt  time.Time `json:"submitted_at"`
	Status       string    `json:"status"`
	Reasons      []string  `json:"reasons"`
	ScoreText    string    `json:"score_text"`
	Rank         int       `json:"rank"`
	Votes        int64     `json:"votes"`
	StaffVerdict string    `json:"staff_verdict"`
	StaffReason  string    `json:"staff_reason"`
}

// StaffChallengeDetail is one challenge's console page.
type StaffChallengeDetail struct {
	StaffChallenge
	Entries []StaffChallengeEntry `json:"entries"`
}

func staffChallenge(row db.Challenge, entryCount int64, now time.Time) (StaffChallenge, error) {
	rules, err := catalog.ChallengeRules(row)
	if err != nil {
		return StaffChallenge{}, err
	}
	item := StaffChallenge{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Summary: row.Summary,
		Description: row.Description, Icons: row.Icons, Status: catalog.ChallengeStatus(row, now),
		Rules: rules, RulesText: challenge.Describe(rules, catalog.ChallengeOptions(row)),
		StartsAt: row.StartsAt.Time, EndsAt: row.EndsAt.Time, VotingEndsAt: row.VotingEndsAt.Time,
		RequireNewRevisions: row.RequireNewRevisions, VotersBeforeStart: row.VotersBeforeStart,
		VotingEnabled: row.VotingEnabled, VotesPerUser: row.VotesPerUser, LiveLeaderboard: row.LiveLeaderboard,
		HiddenUntilStart: row.HiddenUntilStart, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time, EntryCount: entryCount,
	}
	if row.PublishedAt.Valid {
		published := row.PublishedAt.Time
		item.PublishedAt = &published
	}
	return item, nil
}

// ChallengeParams is the parameter catalogue goals and thresholds pick from.
func (s *Service) ChallengeParams() []challenge.Param { return challenge.Params }

// ListStaffChallenges lists every challenge, drafts too, the newest first.
func (s *Service) ListStaffChallenges(ctx context.Context) ([]StaffChallenge, error) {
	rows, err := s.queries.ListStaffChallenges(ctx)
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	now := time.Now()
	challenges := make([]StaffChallenge, 0, len(rows))
	for _, row := range rows {
		item, err := staffChallenge(db.Challenge{
			ID: row.ID, Slug: row.Slug, Title: row.Title, Summary: row.Summary,
			Description: row.Description, Icons: row.Icons, Rules: row.Rules, StartsAt: row.StartsAt,
			EndsAt: row.EndsAt, VotingEndsAt: row.VotingEndsAt, RequireNewRevisions: row.RequireNewRevisions,
			VotersBeforeStart: row.VotersBeforeStart, VotingEnabled: row.VotingEnabled,
			VotesPerUser: row.VotesPerUser, LiveLeaderboard: row.LiveLeaderboard,
			HiddenUntilStart: row.HiddenUntilStart, PublishedAt: row.PublishedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}, row.EntryCount, now)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, item)
	}
	return challenges, nil
}

// GetStaffChallenge reads one challenge with its checked submissions.
func (s *Service) GetStaffChallenge(ctx context.Context, challengeID string) (StaffChallengeDetail, error) {
	id, err := parseID(challengeID)
	if err != nil {
		return StaffChallengeDetail{}, err
	}
	row, err := s.queries.GetChallenge(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffChallengeDetail{}, catalog.ErrNotFound
	}
	if err != nil {
		return StaffChallengeDetail{}, fmt.Errorf("read challenge: %w", err)
	}
	now := time.Now()
	standings, err := s.catalog.ChallengeStandings(ctx, row, now)
	if err != nil {
		return StaffChallengeDetail{}, err
	}
	item, err := staffChallenge(row, int64(len(standings)), now)
	if err != nil {
		return StaffChallengeDetail{}, err
	}
	detail := StaffChallengeDetail{StaffChallenge: item, Entries: make([]StaffChallengeEntry, 0, len(standings))}
	for _, entry := range standings {
		detail.Entries = append(detail.Entries, StaffChallengeEntry{
			SubmissionID: entry.SubmissionID, EntryID: entry.EntryID, Title: entry.Title,
			Username: entry.Username, SubmittedAt: entry.SubmittedAt, Status: entry.Verdict.Status,
			Reasons: entry.Verdict.Reasons, ScoreText: entry.ScoreText, Rank: entry.Rank, Votes: entry.Votes,
			StaffVerdict: entry.StaffVerdict, StaffReason: entry.StaffReason,
		})
	}
	return detail, nil
}

// clean trims and checks the fields; the rules come back normalized.
func (f ChallengeFields) clean() (ChallengeFields, []byte, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = strings.TrimSpace(f.Title)
	f.Summary = strings.TrimSpace(f.Summary)
	f.Description = strings.TrimSpace(f.Description)
	f.Icons = strings.TrimSpace(f.Icons)
	switch {
	case !challengeSlug.MatchString(f.Slug) || len(f.Slug) > 80:
		return f, nil, invalidStaffRequest("the slug is lowercase letters, digits and dashes, at most 80")
	case f.Title == "" || utf8.RuneCountInString(f.Title) > 120:
		return f, nil, invalidStaffRequest("the title is 1 to 120 characters")
	case utf8.RuneCountInString(f.Summary) > 300:
		return f, nil, invalidStaffRequest("the summary is at most 300 characters")
	case utf8.RuneCountInString(f.Description) > 10000:
		return f, nil, invalidStaffRequest("the description is at most 10000 characters")
	case utf8.RuneCountInString(f.Icons) > 500:
		return f, nil, invalidStaffRequest("the icons are at most 500 characters")
	case f.StartsAt.IsZero() || f.EndsAt.IsZero() || f.VotingEndsAt.IsZero():
		return f, nil, invalidStaffRequest("the start, deadline and voting end are required")
	case !f.StartsAt.Before(f.EndsAt):
		return f, nil, invalidStaffRequest("the deadline must be after the start")
	case f.VotingEndsAt.Before(f.EndsAt):
		return f, nil, invalidStaffRequest("voting cannot end before the deadline")
	case f.VotesPerUser < 1 || f.VotesPerUser > 20:
		return f, nil, invalidStaffRequest("votes per user are 1 to 20")
	}
	rules, err := f.Rules.Normalize()
	if err != nil {
		return f, nil, invalidStaffRequest("%s", strings.TrimPrefix(err.Error(), challenge.ErrInvalidRules.Error()+": "))
	}
	f.Rules = rules
	encoded, err := json.Marshal(rules)
	if err != nil {
		return f, nil, fmt.Errorf("encode challenge rules: %w", err)
	}
	return f, encoded, nil
}

func challengeAudit(row db.Challenge) map[string]any {
	return map[string]any{
		"slug": row.Slug, "title": row.Title, "rules": json.RawMessage(row.Rules),
		"starts_at": row.StartsAt.Time, "ends_at": row.EndsAt.Time, "voting_ends_at": row.VotingEndsAt.Time,
		"require_new_revisions": row.RequireNewRevisions, "voters_before_start": row.VotersBeforeStart,
		"voting_enabled": row.VotingEnabled, "votes_per_user": row.VotesPerUser,
		"live_leaderboard": row.LiveLeaderboard, "hidden_until_start": row.HiddenUntilStart,
	}
}

// CreateChallenge makes a draft challenge and returns its id.
func (s *Service) CreateChallenge(ctx context.Context, by audit.Staff, fields ChallengeFields) (string, error) {
	fields, rules, err := fields.clean()
	if err != nil {
		return "", err
	}
	id := shortid.New()
	err = s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		row, err := queries.InsertChallenge(ctx, db.InsertChallengeParams{
			ID: id, Slug: fields.Slug, Title: fields.Title, Summary: fields.Summary,
			Description: fields.Description, Icons: fields.Icons, Rules: rules,
			StartsAt: pgTime(fields.StartsAt), EndsAt: pgTime(fields.EndsAt), VotingEndsAt: pgTime(fields.VotingEndsAt),
			RequireNewRevisions: fields.RequireNewRevisions, VotersBeforeStart: fields.VotersBeforeStart,
			VotingEnabled: fields.VotingEnabled, VotesPerUser: fields.VotesPerUser, LiveLeaderboard: fields.LiveLeaderboard,
			HiddenUntilStart: fields.HiddenUntilStart,
		})
		if uniqueViolation(err) {
			return invalidStaffRequest("a challenge with that slug exists")
		}
		if err != nil {
			return fmt.Errorf("create challenge: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "challenge.create", TargetType: "challenge", TargetID: row.ID, After: challengeAudit(row),
		})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateChallenge changes a challenge. Once it has started, changing what
// decides the ranking (the rules or the new-revisions option) rescores every
// entry, so it needs ConfirmRescore.
func (s *Service) UpdateChallenge(ctx context.Context, by audit.Staff, challengeID string, fields ChallengeFields) error {
	fields, rules, err := fields.clean()
	if err != nil {
		return err
	}
	id, err := parseID(challengeID)
	if err != nil {
		return err
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		queries := c.Queries()
		c.ChallengeChanged(id)
		before, err := queries.LockChallenge(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		started := before.PublishedAt.Valid && !time.Now().Before(before.StartsAt.Time)
		rescores := !sameJSON(before.Rules, rules) || before.RequireNewRevisions != fields.RequireNewRevisions
		if started && rescores && !fields.ConfirmRescore {
			return invalidStaffRequest("the challenge has started: changing its rules rescores every entry; confirm it")
		}
		after, err := queries.UpdateChallenge(ctx, db.UpdateChallengeParams{
			ID: id, Slug: fields.Slug, Title: fields.Title, Summary: fields.Summary,
			Description: fields.Description, Icons: fields.Icons, Rules: rules,
			StartsAt: pgTime(fields.StartsAt), EndsAt: pgTime(fields.EndsAt), VotingEndsAt: pgTime(fields.VotingEndsAt),
			RequireNewRevisions: fields.RequireNewRevisions, VotersBeforeStart: fields.VotersBeforeStart,
			VotingEnabled: fields.VotingEnabled, VotesPerUser: fields.VotesPerUser, LiveLeaderboard: fields.LiveLeaderboard,
			HiddenUntilStart: fields.HiddenUntilStart,
		})
		if uniqueViolation(err) {
			return invalidStaffRequest("a challenge with that slug exists")
		}
		if err != nil {
			return fmt.Errorf("update challenge: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "challenge.update", TargetType: "challenge", TargetID: id,
			Before: challengeAudit(before), After: challengeAudit(after),
		})
	})
}

// PublishChallenge shows (published) or hides a challenge. Only a challenge
// that has not started can be hidden again.
func (s *Service) PublishChallenge(ctx context.Context, by audit.Staff, challengeID string, published bool) error {
	id, err := parseID(challengeID)
	if err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		before, err := queries.LockChallenge(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		if !published && before.PublishedAt.Valid && !time.Now().Before(before.StartsAt.Time) {
			return invalidStaffRequest("a challenge that has started cannot be unpublished")
		}
		if _, err := queries.SetChallengePublished(ctx, db.SetChallengePublishedParams{ID: id, Published: published}); err != nil {
			return fmt.Errorf("publish challenge: %w", err)
		}
		action := "challenge.publish"
		if !published {
			action = "challenge.unpublish"
		}
		return audit.Write(ctx, queries, by, audit.Record{Action: action, TargetType: "challenge", TargetID: id})
	})
}

// DeleteChallenge deletes a challenge that was never published.
func (s *Service) DeleteChallenge(ctx context.Context, by audit.Staff, challengeID string) error {
	id, err := parseID(challengeID)
	if err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		before, err := queries.LockChallenge(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		if before.PublishedAt.Valid {
			return invalidStaffRequest("unpublish the challenge before deleting it")
		}
		if _, err := queries.DeleteChallenge(ctx, id); err != nil {
			return fmt.Errorf("delete challenge: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "challenge.delete", TargetType: "challenge", TargetID: id, Before: challengeAudit(before),
		})
	})
}

// SetSubmissionVerdict overrides a submission's rule check: accepted ranks it
// whatever the restrictions say (it still needs a goal value), rejected
// disqualifies it, "" goes back to the check. A verdict needs a reason.
func (s *Service) SetSubmissionVerdict(
	ctx context.Context, by audit.Staff, challengeID, submissionID, verdict, reason string,
) error {
	challengeID, err := parseID(challengeID)
	if err != nil {
		return err
	}
	if submissionID, err = parseID(submissionID); err != nil {
		return err
	}
	if verdict != "" && verdict != challenge.StatusAccepted && verdict != challenge.StatusRejected {
		return invalidStaffRequest("the verdict is accepted, rejected or empty")
	}
	if reason, err = audit.CleanReason(reason); err != nil {
		return err
	}
	if verdict != "" && reason == "" {
		return invalidStaffRequest("give a reason")
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		queries := c.Queries()
		c.ChallengeChanged(challengeID)
		before, err := queries.GetChallengeSubmission(ctx, db.GetChallengeSubmissionParams{ID: submissionID, ChallengeID: challengeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read challenge submission: %w", err)
		}
		if _, err := queries.SetChallengeSubmissionVerdict(ctx, db.SetChallengeSubmissionVerdictParams{
			ID: submissionID, ChallengeID: challengeID, Verdict: pgText(verdict), Reason: reason,
		}); err != nil {
			return fmt.Errorf("set submission verdict: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "challenge.verdict", TargetType: "challenge_submission", TargetID: submissionID, Reason: reason,
			Before: map[string]any{"verdict": before.StaffVerdict.String, "entry_id": before.EntryID},
			After:  map[string]any{"verdict": verdict},
		})
	})
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

// sameJSON compares two JSON documents by value.
func sameJSON(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return bytes.Equal(a, b)
	}
	encodedLeft, _ := json.Marshal(left)
	encodedRight, _ := json.Marshal(right)
	return bytes.Equal(encodedLeft, encodedRight)
}
