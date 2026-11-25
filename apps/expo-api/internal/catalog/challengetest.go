package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"factorio-expo/apps/expo-api/internal/challenge"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// ChallengeTestBlueprint reads the stored facts of an entry's current
// revision as a rule check's input, for the console's "test a blueprint" box:
// any entry counts (a quick preview made for the test, or an existing one),
// whether or not it is in a challenge. catalog.ErrNotFound for an unknown id.
func (s *Service) ChallengeTestBlueprint(ctx context.Context, entryID string) (challenge.Blueprint, error) {
	id, err := parseID(entryID)
	if err != nil {
		return challenge.Blueprint{}, err
	}
	row, err := s.queries.GetChallengeTestFacts(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return challenge.Blueprint{}, ErrNotFound
	}
	if err != nil {
		return challenge.Blueprint{}, fmt.Errorf("read challenge test facts: %w", err)
	}
	return challengeBlueprint(testFacts(row))
}

func testFacts(row db.GetChallengeTestFactsRow) blueprintFacts {
	return blueprintFacts{
		RootKind: row.RootKind, BlueprintID: row.BlueprintID, RevisionCreatedAt: row.RevisionCreatedAt.Time,
		Entities: row.Entities, Recipes: row.Recipes, Mods: row.Mods, ModsVersion: row.ModsVersion,
		Production: row.Production, ProductionSchemaVersion: row.ProductionSchemaVersion,
		ProductionState: row.ProductionState, HasProduction: row.HasProduction, AnalysisJobState: row.AnalysisJobState,
	}
}
