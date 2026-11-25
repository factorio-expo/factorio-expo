package catalog_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// opsFor and deletionFor build the staff operations and the account deletion
// over a test catalog, for tests that set up bans, tags or deleted accounts.
func opsFor(service *catalog.Service) *staffops.Service {
	return staffops.New(service, deletionFor(service))
}

func deletionFor(service *catalog.Service) *accountdeletion.Service {
	return accountdeletion.New(service, testBanHolds)
}

var (
	admin     = audit.Staff{Username: "alice-staff"}
	colleague = audit.Staff{Username: "bob-staff"}
)

func newStaffFixtures(t *testing.T) (*catalog.Service, *pgxpool.Pool, string, string) {
	t.Helper()
	service, pool, owner, reporter := newCommentFixtures(t)
	if _, err := pool.Exec(context.Background(), "TRUNCATE admin_actions"); err != nil {
		t.Fatalf("truncate admin actions: %v", err)
	}
	return service, pool, owner, reporter
}

// auditActions lists the recorded staff actions as "staff action target",
// oldest first.
func auditActions(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT staff_username || ' ' || action || ' ' || target_id FROM admin_actions ORDER BY created_at, id")
	if err != nil {
		t.Fatalf("read admin actions: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	return actions
}

func countWhere(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}
