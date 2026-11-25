package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// XP and badges (docs/architecture.md#xp-and-badges). Every award is one
// append-only xp_events row, written in the same transaction as the event that
// earned it (the upload, favorite, collection add or comment), or by the
// leaderboards and challenge_awards tasks (xpawards.go). Its source key names
// the event, so a retry, a repeat or a restore never awards twice. A row counts
// while its content lives, so removed content takes its points back without
// editing the ledger. Points never reach search or any ranking.

// XPReason is why a ledger row was earned (xp_events.reason).
type XPReason string

// XPReasons lists every reason in display order (the API's enum).
func XPReasons() []XPReason { return xpReasonOrder }

const (
	xpUpload                 XPReason = "upload"
	xpFavorite               XPReason = "favorite"
	xpCollection             XPReason = "collection"
	xpComment                XPReason = "comment"
	xpRecord                 XPReason = "record"
	xpChallengeWin           XPReason = "challenge_win"
	xpChallengeSecond        XPReason = "challenge_second"
	xpChallengeThird         XPReason = "challenge_third"
	xpChallengeParticipation XPReason = "challenge_participation"
)

// xpPoints is each reason's award.
var xpPoints = map[XPReason]int32{
	xpUpload:                 10,
	xpFavorite:               2,
	xpCollection:             3,
	xpComment:                1,
	xpRecord:                 50,
	xpChallengeWin:           500,
	xpChallengeSecond:        300,
	xpChallengeThird:         200,
	xpChallengeParticipation: 25,
}

// xpDailyCaps limit how many awards of a reason one user earns per UTC day.
var xpDailyCaps = map[XPReason]int32{xpUpload: 5, xpComment: 5}

// LevelThresholds are the XP each level starts at: level 1 at 0, level 10 at
// 5000.
var LevelThresholds = []int64{0, 50, 150, 300, 500, 800, 1200, 2000, 3000, 5000}

// Badge is one achievement a profile shows.
type Badge string

const (
	BadgeFirstUpload      Badge = "first_upload"
	BadgeTenUploads       Badge = "ten_uploads"
	BadgeFirstFavorite    Badge = "first_favorite"
	BadgeFavorites25      Badge = "favorites_25"
	BadgeTenComments      Badge = "ten_comments"
	BadgeBookAuthor       Badge = "book_author"
	BadgeRecordHolder     Badge = "record_holder"
	BadgeChallengeEntrant Badge = "challenge_entrant"
	BadgeChallengePodium  Badge = "challenge_podium"
	BadgeChallengeWinner  Badge = "challenge_winner"
	BadgeFirstChallenge   Badge = "first_challenge"
)

// Badges lists every badge in the order a profile shows them.
var Badges = []Badge{
	BadgeFirstUpload, BadgeTenUploads, BadgeFirstFavorite, BadgeFavorites25, BadgeTenComments,
	BadgeBookAuthor, BadgeRecordHolder, BadgeChallengeEntrant, BadgeChallengePodium,
	BadgeChallengeWinner, BadgeFirstChallenge,
}

// The badge thresholds (badgesOf, GetUserBadgeFacts and the rules table).
const (
	tenUploadsBadgeCount    = 10
	favoritesBadgeCount     = 25
	tenCommentsBadgeCount   = 10
	bookAuthorMinBlueprints = 10
)

// badgeCriteria is what earns each badge, in words (the about page's table
// through GET /api/v1/xp-rules); the numbers are the thresholds above.
var badgeCriteria = map[Badge]string{
	BadgeFirstUpload:      "Have a public upload.",
	BadgeTenUploads:       fmt.Sprintf("Have %d public uploads.", tenUploadsBadgeCount),
	BadgeFirstFavorite:    "Have one of your entries favorited by someone else.",
	BadgeFavorites25:      fmt.Sprintf("Have your entries favorited %d times by other users.", favoritesBadgeCount),
	BadgeTenComments:      fmt.Sprintf("Post %d comments.", tenCommentsBadgeCount),
	BadgeBookAuthor:       fmt.Sprintf("Have a public book of %d or more blueprints.", bookAuthorMinBlueprints),
	BadgeRecordHolder:     "Hold first place on a leaderboard board. Kept for good once earned.",
	BadgeChallengeEntrant: "Enter a design challenge. Kept for good once earned.",
	BadgeChallengePodium:  "Place in the top three of a design challenge. Kept for good once earned.",
	BadgeChallengeWinner:  "Win a design challenge. Kept for good once earned.",
	BadgeFirstChallenge:   "Take part in the first design challenge. Kept for good once earned.",
}

// XPAwardRule is one way to earn XP.
type XPAwardRule struct {
	Reason   XPReason `json:"reason" doc:"What earns the points."`
	Points   int32    `json:"points" doc:"The points each award gives."`
	DailyCap int32    `json:"daily_cap,omitempty" doc:"How many of these awards count per UTC day; absent for no cap."`
}

// XPLevel is the XP a level starts at.
type XPLevel struct {
	Level int   `json:"level" doc:"The level, from 1."`
	XP    int64 `json:"xp" doc:"The XP the level starts at."`
}

// BadgeRule is what earns a badge.
type BadgeRule struct {
	Badge    Badge  `json:"badge" doc:"The badge."`
	Criteria string `json:"criteria" doc:"What earns it, in words."`
}

// XPRules is the whole XP and badge table, from the constants the awards and
// badges use, so the about page never drifts from the code.
type XPRules struct {
	Awards        []XPAwardRule `json:"awards" doc:"Every way to earn XP, in display order."`
	Levels        []XPLevel     `json:"levels" doc:"Every level and the XP it starts at."`
	Badges        []BadgeRule   `json:"badges" doc:"Every badge and what earns it, in display order."`
	DailyCapReset string        `json:"daily_cap_reset" doc:"When the daily caps start over: midnight UTC."`
}

// xpReasonOrder is the display order of the award rules.
var xpReasonOrder = []XPReason{
	xpUpload, xpFavorite, xpCollection, xpComment, xpRecord,
	xpChallengeWin, xpChallengeSecond, xpChallengeThird, xpChallengeParticipation,
}

// Rules returns the XP and badge table.
func Rules() XPRules {
	rules := XPRules{DailyCapReset: "00:00 UTC"}
	for _, reason := range xpReasonOrder {
		rules.Awards = append(rules.Awards, XPAwardRule{
			Reason: reason, Points: xpPoints[reason], DailyCap: xpDailyCaps[reason],
		})
	}
	for i, threshold := range LevelThresholds {
		rules.Levels = append(rules.Levels, XPLevel{Level: i + 1, XP: threshold})
	}
	for _, badge := range Badges {
		rules.Badges = append(rules.Badges, BadgeRule{Badge: badge, Criteria: badgeCriteria[badge]})
	}
	return rules
}

// UserProgress is a profile's XP, level, badges and the records it holds.
type UserProgress struct {
	XP    int64 `json:"xp" doc:"The user's XP: the points of every award whose content still exists."`
	Level int   `json:"level" doc:"The user's level, 1 to 10."`
	// LevelXP and NextLevelXP bound the current level.
	LevelXP     int64        `json:"level_xp" doc:"The XP the current level starts at."`
	NextLevelXP *int64       `json:"next_level_xp,omitempty" doc:"The XP the next level starts at; absent at the top level."`
	Badges      []Badge      `json:"badges" doc:"The badges the user has earned, in display order."`
	Records     []HeldRecord `json:"records" doc:"The leaderboard boards the user leads now."`
}

// HeldRecord is a board a user leads.
type HeldRecord struct {
	Board       Board      `json:"board" doc:"The record board."`
	Group       BoardGroup `json:"group" doc:"The leaderboard group the board is in."`
	Title       string     `json:"title" doc:"The board's name."`
	Unit        string     `json:"unit" doc:"The unit of the value."`
	EntryID     string     `json:"entry_id" doc:"The leading entry."`
	BlueprintID string     `json:"blueprint_id" doc:"The leading blueprint."`
	EntryTitle  string     `json:"entry_title" doc:"The leading entry's title."`
	Value       float64    `json:"value" doc:"The record value, in the board's unit."`
	Since       time.Time  `json:"since" doc:"Since when the entry leads the board."`
}

// levelOf is the level an XP total reaches and its bounds.
func levelOf(xp int64) (level int, from int64, next *int64) {
	for i, threshold := range LevelThresholds {
		if xp >= threshold {
			level, from = i+1, threshold
		}
	}
	if level < len(LevelThresholds) {
		value := LevelThresholds[level]
		next = &value
	}
	return level, from, next
}

// xpTally is a user's ledger per reason.
type xpTally struct {
	// counted: rows whose content lives; all: every row ever earned.
	counted, all map[XPReason]int64
	points       int64
}

// badgesOf turns a tally and the other badge facts into the earned badges.
// Upload, favorite and comment badges count only content that still lives;
// records and challenge places, once earned, stay.
func badgesOf(tally xpTally, bookAuthor, firstChallenge bool) []Badge {
	earned := map[Badge]bool{
		BadgeFirstUpload:      tally.counted[xpUpload] >= 1,
		BadgeTenUploads:       tally.counted[xpUpload] >= tenUploadsBadgeCount,
		BadgeFirstFavorite:    tally.counted[xpFavorite] >= 1,
		BadgeFavorites25:      tally.counted[xpFavorite] >= favoritesBadgeCount,
		BadgeTenComments:      tally.counted[xpComment] >= tenCommentsBadgeCount,
		BadgeBookAuthor:       bookAuthor,
		BadgeRecordHolder:     tally.all[xpRecord] >= 1,
		BadgeChallengeEntrant: tally.all[xpChallengeParticipation] >= 1,
		BadgeChallengePodium:  tally.all[xpChallengeWin]+tally.all[xpChallengeSecond]+tally.all[xpChallengeThird] >= 1,
		BadgeChallengeWinner:  tally.all[xpChallengeWin] >= 1,
		BadgeFirstChallenge:   firstChallenge,
	}
	badges := []Badge{}
	for _, badge := range Badges {
		if earned[badge] {
			badges = append(badges, badge)
		}
	}
	return badges
}

// readUserProgress reads a user's XP, level, badges and held records.
func readUserProgress(ctx context.Context, queries *db.Queries, userID string) (UserProgress, error) {
	rows, err := queries.SummarizeUserXP(ctx, userID)
	if err != nil {
		return UserProgress{}, fmt.Errorf("summarize xp: %w", err)
	}
	tally := xpTally{counted: map[XPReason]int64{}, all: map[XPReason]int64{}}
	for _, row := range rows {
		reason := XPReason(row.Reason)
		tally.counted[reason] = row.CountedEvents
		tally.all[reason] = row.AllEvents
		tally.points += row.CountedPoints
	}
	facts, err := queries.GetUserBadgeFacts(ctx, db.GetUserBadgeFactsParams{
		UserID: userID, BookMinBlueprints: bookAuthorMinBlueprints,
	})
	if err != nil {
		return UserProgress{}, fmt.Errorf("read badge facts: %w", err)
	}
	records, err := queries.ListUserRecords(ctx, pgtype.Text{String: userID, Valid: true})
	if err != nil {
		return UserProgress{}, fmt.Errorf("list held records: %w", err)
	}
	level, from, next := levelOf(tally.points)
	progress := UserProgress{
		XP: tally.points, Level: level, LevelXP: from, NextLevelXP: next,
		Badges:  badgesOf(tally, facts.BookAuthor, facts.FirstChallenge),
		Records: make([]HeldRecord, 0, len(records)),
	}
	for _, record := range records {
		spec := boardSpecOf(Board(record.Board))
		progress.Records = append(progress.Records, HeldRecord{
			Board: spec.board, Group: spec.group, Title: spec.title, Unit: spec.unit, EntryID: record.EntryID, BlueprintID: record.BlueprintID,
			EntryTitle: record.EntryTitle, Value: record.Value, Since: record.Since.Time,
		})
	}
	return progress, nil
}
