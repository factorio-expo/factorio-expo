package catalog

import (
	"context"
	"fmt"

	"factorio-expo/apps/expo-api/internal/challenge"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// AwardClosedChallenges is the challenge_awards task: for each published
// challenge whose voting has ended and whose awards are not out, it ranks the
// final board (as the page shows it) and awards the first three places by
// score and every entrant not rejected, then marks the challenge awarded, in
// one transaction under the challenge's row lock. The source keys make a
// second run award nothing. It returns how many challenges it awarded.
func (s *Service) AwardClosedChallenges(ctx context.Context) (int, error) {
	rows, err := s.queries.ListUnawardedClosedChallenges(ctx)
	if err != nil {
		return 0, fmt.Errorf("list closed challenges: %w", err)
	}
	awarded := 0
	for _, row := range rows {
		done, err := s.awardChallenge(ctx, row.ID)
		if err != nil {
			return awarded, err
		}
		if done {
			awarded++
		}
	}
	return awarded, nil
}

// awardChallenge awards one closed challenge unless another run did.
func (s *Service) awardChallenge(ctx context.Context, challengeID string) (bool, error) {
	done := false
	err := s.inTransaction(ctx, func(queries *db.Queries) error {
		row, err := queries.LockChallenge(ctx, challengeID)
		if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		if row.AwardedAt.Valid {
			return nil
		}
		// Read under the challenge's lock, in this transaction.
		scored, err := computeChallengeScores(ctx, queries, row)
		if err != nil {
			return err
		}
		for _, entry := range scored {
			if entry.verdict.Status == challenge.StatusRejected {
				continue
			}
			if _, err := recordXP(ctx, queries, xpChallengeEntered{challengeID: row.ID, userID: entry.userID}); err != nil {
				return err
			}
			if _, err := recordXP(ctx, queries, xpChallengePlaced{
				challengeID: row.ID, userID: entry.userID, rank: entry.rank,
			}); err != nil {
				return err
			}
		}
		marked, err := queries.MarkChallengeAwarded(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("mark challenge awarded: %w", err)
		}
		done = marked > 0
		return nil
	})
	return done, err
}
