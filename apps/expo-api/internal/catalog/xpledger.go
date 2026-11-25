package catalog

import (
	"context"
	"fmt"
	"strconv"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// The XP ledger's one way in: recordXP(ctx, queries, event), on the
// transaction of the event it rewards. Each event decides on its own, without
// the database, who earns, for which reason and under which source key
// (xpEvent.xpAward); the ledger then applies what needs the database: the
// source key already earned, a deleted or banned account (xp.sql AwardXP) and
// the reason's daily cap (xpDailyCaps, under an advisory lock).

// xpEvent is something that may earn XP.
type xpEvent interface {
	// xpAward is the row to append, or false when the event earns nothing.
	xpAward(now time.Time) (xpAward, bool)
}

// xpAward is one ledger row to append.
type xpAward struct {
	userID    string
	reason    XPReason
	sourceKey string
	entryID   string
	commentID string
	challenge string
	board     Board
}

// xpUploaded: an entry the owner listed now (an upload, a publish or claim, a
// change to public). Once per entry; an anonymous entry earns nothing.
type xpUploaded struct{ entry db.Entry }

func (e xpUploaded) xpAward(now time.Time) (xpAward, bool) {
	if !e.entry.OwnerUserID.Valid || !entryListed(e.entry, now) {
		return xpAward{}, false
	}
	return xpAward{
		userID: e.entry.OwnerUserID.String, reason: xpUpload,
		sourceKey: "upload:" + e.entry.ID, entryID: e.entry.ID,
	}, true
}

// xpReceived: someone else favorited the entry or added it to a collection
// (reason xpFavorite or xpCollection). Once per user and entry, never for
// one's own.
type xpReceived struct {
	reason   XPReason
	entry    db.Entry
	byUserID string
}

func (e xpReceived) xpAward(time.Time) (xpAward, bool) {
	if !e.entry.OwnerUserID.Valid || e.entry.OwnerUserID.String == e.byUserID {
		return xpAward{}, false
	}
	return xpAward{
		userID: e.entry.OwnerUserID.String, reason: e.reason,
		sourceKey: string(e.reason) + ":" + e.byUserID + ":" + e.entry.ID, entryID: e.entry.ID,
	}, true
}

// xpCommented: a posted comment (at most 5 a day).
type xpCommented struct{ authorID, commentID string }

func (e xpCommented) xpAward(time.Time) (xpAward, bool) {
	return xpAward{
		userID: e.authorID, reason: xpComment, sourceKey: "comment:" + e.commentID, commentID: e.commentID,
	}, true
}

// xpRecordHeld: the entry leads a leaderboard board (once per board and
// entry); an ownerless entry earns nothing.
type xpRecordHeld struct {
	board   Board
	ownerID string
	entryID string
}

func (e xpRecordHeld) xpAward(time.Time) (xpAward, bool) {
	return xpAward{
		userID: e.ownerID, reason: xpRecord, board: e.board,
		sourceKey: "record:" + string(e.board) + ":" + e.entryID, entryID: e.entryID,
	}, true
}

// challengePlaces are the XP of the top three places by score.
var challengePlaces = []XPReason{xpChallengeWin, xpChallengeSecond, xpChallengeThird}

// xpChallengeEntered: an entrant of a closed challenge whose submission was
// not rejected. Once per challenge and user.
type xpChallengeEntered struct{ challengeID, userID string }

func (e xpChallengeEntered) xpAward(time.Time) (xpAward, bool) {
	return xpAward{
		userID: e.userID, reason: xpChallengeParticipation, challenge: e.challengeID,
		sourceKey: "challenge:" + e.challengeID + ":entered:" + e.userID,
	}, true
}

// xpChallengePlaced: a top-three place by score (rank from 1) in a closed
// challenge; any other rank earns nothing. Once per challenge and user.
type xpChallengePlaced struct {
	challengeID, userID string
	rank                int
}

func (e xpChallengePlaced) xpAward(time.Time) (xpAward, bool) {
	if e.rank < 1 || e.rank > len(challengePlaces) {
		return xpAward{}, false
	}
	return xpAward{
		userID: e.userID, reason: challengePlaces[e.rank-1], challenge: e.challengeID,
		sourceKey: "challenge:" + e.challengeID + ":place:" + e.userID,
	}, true
}

// recordXP appends the event's award unless the event earns nothing, its
// source already earned, the account is deleted or banned, or the reason's
// daily cap is reached. It reports whether a row was written.
func recordXP(ctx context.Context, queries *db.Queries, event xpEvent) (bool, error) {
	award, ok := event.xpAward(time.Now())
	if !ok || award.userID == "" {
		return false, nil
	}
	dailyCap := xpDailyCaps[award.reason]
	if dailyCap > 0 {
		if err := queries.LockUserXPReason(ctx, db.LockUserXPReasonParams{
			UserID: award.userID, Reason: string(award.reason),
		}); err != nil {
			return false, fmt.Errorf("lock xp cap: %w", err)
		}
	}
	written, err := queries.AwardXP(ctx, db.AwardXPParams{
		ID: shortid.New(), UserID: award.userID, Reason: string(award.reason),
		Points: xpPoints[award.reason], SourceKey: award.sourceKey,
		EntryID: pgText(award.entryID), CommentID: pgText(award.commentID),
		ChallengeID: pgText(award.challenge), Board: pgText(string(award.board)),
		DailyCap: dailyCap,
	})
	if err != nil {
		return false, fmt.Errorf("award xp %s: %w", strconv.Quote(award.sourceKey), err)
	}
	return written > 0, nil
}
