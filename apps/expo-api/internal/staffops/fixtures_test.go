package staffops_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// opsFor and deletionFor build the staff operations and the account deletion
// over a test catalog.
func opsFor(service *catalog.Service) *staffops.Service {
	return staffops.New(service, deletionFor(service))
}

func deletionFor(service *catalog.Service) *accountdeletion.Service {
	return accountdeletion.New(service, testBanHolds)
}

func createOwnedEntry(t *testing.T, service *catalog.Service, owner, visibility string) catalog.Entry {
	t.Helper()
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: owner, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	return entry
}

func addUser(t *testing.T, pool *pgxpool.Pool, id, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, username, created_at)
		VALUES ($1, $2, now() - interval '30 days')`, id, name); err != nil {
		t.Fatalf("insert user %s: %v", name, err)
	}
}

func assertSearchJob(t *testing.T, pool *pgxpool.Pool, entryID, action string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(context.Background(), "SELECT action FROM entry_search_jobs WHERE entry_id = $1", entryID).Scan(&got); err != nil || got != action {
		t.Fatalf("search job for %s = %q, %v; want %q", entryID, got, err, action)
	}
}

// entryLeaves are the live revision's blueprint ids in tree order.
func entryLeaves(t *testing.T, pool *pgxpool.Pool, entryID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT b.id FROM blueprints b
		JOIN entries e ON e.current_revision_id = b.revision_id
		WHERE e.id = $1 ORDER BY b.position`, entryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// expectIPBan checks an address ban for action ends between from and to from now.
func expectIPBan(t *testing.T, pool *pgxpool.Pool, address []byte, action string, from, to time.Duration) {
	t.Helper()
	var left time.Duration
	var seconds float64
	if err := pool.QueryRow(context.Background(),
		"SELECT extract(epoch FROM expires_at - now())::float8 FROM ip_bans WHERE ip_hash = $1 AND action = $2",
		address, action).Scan(&seconds); err != nil {
		t.Fatalf("read %s address ban: %v", action, err)
	}
	left = time.Duration(seconds * float64(time.Second))
	if left < from || left > to {
		t.Fatalf("%s address ban ends in %s, want %s-%s", action, left, from, to)
	}
}

// failLeaf fails both phases of a leaf for good with code.
func failLeaf(t *testing.T, pool *pgxpool.Pool, blueprintID, code string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"UPDATE phase_jobs SET state = 'failed', failure_code = $2, failure_detail = 'detail' WHERE blueprint_id = $1",
		blueprintID, code); err != nil {
		t.Fatal(err)
	}
}

func ipBanCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM ip_bans").Scan(&n); err != nil {
		t.Fatalf("count address bans: %v", err)
	}
	return n
}

func ipHash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// isBan reports whether err is a ban of action.
func isBan(err error, action catalog.BanAction) bool {
	var ban *catalog.BanError
	return errors.As(err, &ban) && ban.Ban.Action == action
}

func listComments(t *testing.T, service *catalog.Service, entryID string, auth catalog.Authorization) catalog.CommentPage {
	t.Helper()
	page, err := service.ListComments(context.Background(), entryID, auth, catalog.CommentPageRequest{})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	return page
}

// newCommentFixtures is newSocialFixtures with accounts older than a day, so the
// new-account comment cap stays out of the way unless a test wants it.
func newCommentFixtures(t *testing.T) (*catalog.Service, *pgxpool.Pool, string, string) {
	t.Helper()
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "UPDATE users SET created_at = now() - interval '30 days'"); err != nil {
		t.Fatalf("age users: %v", err)
	}
	return service, pool, userA, userB
}

// newService connects to the throwaway Postgres named by TEST_DATABASE_URL and
// truncates the catalog tables, so each test starts from a known empty state.
// Without that variable the test is skipped, keeping `go test ./...` runnable
// without a database.
func newService(t *testing.T) (*catalog.Service, *pgxpool.Pool) {
	t.Helper()
	return newServiceWith(t, catalog.Options{})
}

// newServiceWith is newService with options (a progress hub, say).
func newServiceWith(t *testing.T, options catalog.Options) (*catalog.Service, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(),
		"TRUNCATE entries, revisions, books, blueprints, previews, phase_jobs, ip_bans, ban_holds CASCADE",
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if options.BanHolds == nil {
		options.BanHolds = testBanHolds
	}
	return catalog.New(pool, options), pool
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../test-data/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

// testBanHolds is the ban hold hasher of the catalog tests.
var testBanHolds = banhold.New("test-ip-secret")

func uploadFrom(t *testing.T, service *catalog.Service, fixture string, ipHash []byte) catalog.Entry {
	t.Helper()
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, fixture), IPHash: ipHash,
	})
	if err != nil {
		t.Fatalf("upload %s: %v", fixture, err)
	}
	return entry
}

// newSocialFixtures builds a catalog.Service with a clean slate for
// favorites/collections tests: it truncates entries (via newService) plus
// users and collections (which cascade to favorites and collection_entries),
// then inserts two fixed test users. Tags are deliberately left untouched:
// they are curated, migration-seeded rows, not per-test fixtures.
func newSocialFixtures(t *testing.T) (service *catalog.Service, pool *pgxpool.Pool, userA, userB string) {
	t.Helper()
	service, pool = newService(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "TRUNCATE users, collections CASCADE"); err != nil {
		t.Fatalf("truncate users: %v", err)
	}

	const insertUser = `
		INSERT INTO users (id, username) VALUES ($1, $2)`
	userA = "socialUserA1"
	userB = "socialUserB1"
	if _, err := pool.Exec(ctx, insertUser, userA, "social-test-alice"); err != nil {
		t.Fatalf("insert user A: %v", err)
	}
	if _, err := pool.Exec(ctx, insertUser, userB, "social-test-bob"); err != nil {
		t.Fatalf("insert user B: %v", err)
	}
	return service, pool, userA, userB
}

// addressA is a stand-in client address hash.
var addressA = []byte("address-a-hash-0123456789abcdef!")
