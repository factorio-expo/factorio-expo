package catalog

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The award decisions of the XP events, without a database: who earns, for
// which reason and under which source key, or nothing.
func TestXPEventAwards(t *testing.T) {
	now := time.Now()
	owned := db.Entry{
		ID: "e1", OwnerUserID: pgtype.Text{String: "u1", Valid: true}, Visibility: "public",
		CurrentRevisionID: pgtype.Text{String: "r1", Valid: true},
	}
	anonymous := db.Entry{ID: "e2", Visibility: "public"}
	unlisted := owned
	unlisted.Visibility = "unlisted"
	cases := []struct {
		name  string
		event xpEvent
		want  xpAward
		earns bool
	}{
		{"an owned listed upload", xpUploaded{entry: owned},
			xpAward{userID: "u1", reason: xpUpload, sourceKey: "upload:e1", entryID: "e1"}, true},
		{"an unlisted upload", xpUploaded{entry: unlisted}, xpAward{}, false},
		{"an anonymous upload", xpUploaded{entry: anonymous}, xpAward{}, false},
		{"someone else's favorite", xpReceived{reason: xpFavorite, entry: owned, byUserID: "u2"},
			xpAward{userID: "u1", reason: xpFavorite, sourceKey: "favorite:u2:e1", entryID: "e1"}, true},
		{"a favorite of one's own entry", xpReceived{reason: xpFavorite, entry: owned, byUserID: "u1"}, xpAward{}, false},
		{"a collection add of an anonymous entry", xpReceived{reason: xpCollection, entry: anonymous, byUserID: "u2"}, xpAward{}, false},
		{"a comment", xpCommented{authorID: "u3", commentID: "c1"},
			xpAward{userID: "u3", reason: xpComment, sourceKey: "comment:c1", commentID: "c1"}, true},
		{"a record", xpRecordHeld{board: "fastest", ownerID: "u1", entryID: "e1"},
			xpAward{userID: "u1", reason: xpRecord, sourceKey: "record:fastest:e1", entryID: "e1", board: "fastest"}, true},
		{"a challenge entrant", xpChallengeEntered{challengeID: "ch", userID: "u1"},
			xpAward{userID: "u1", reason: xpChallengeParticipation, sourceKey: "challenge:ch:entered:u1", challenge: "ch"}, true},
		{"a challenge winner", xpChallengePlaced{challengeID: "ch", userID: "u1", rank: 1},
			xpAward{userID: "u1", reason: xpChallengeWin, sourceKey: "challenge:ch:place:u1", challenge: "ch"}, true},
		{"a challenge third", xpChallengePlaced{challengeID: "ch", userID: "u1", rank: 3},
			xpAward{userID: "u1", reason: xpChallengeThird, sourceKey: "challenge:ch:place:u1", challenge: "ch"}, true},
		{"a challenge fourth", xpChallengePlaced{challengeID: "ch", userID: "u1", rank: 4}, xpAward{}, false},
		{"an unranked entrant", xpChallengePlaced{challengeID: "ch", userID: "u1", rank: 0}, xpAward{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, earns := tc.event.xpAward(now)
			if earns != tc.earns || got != tc.want {
				t.Fatalf("xpAward = %+v, %v; want %+v, %v", got, earns, tc.want, tc.earns)
			}
		})
	}
}
