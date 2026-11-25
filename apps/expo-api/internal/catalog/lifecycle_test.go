package catalog_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The Go lifecycle rules (lifecycle.go) and their SQL twins (entry_live,
// entry_visible_to, entry_listed) must agree on every kind of entry row, and
// entry_lasting (no Go twin: no Go code decides it) is entry_live without
// quick previews.
func TestLifecycleRulesMatchSQL(t *testing.T) {
	_, pool, owner, other := newSocialFixtures(t)
	ctx := context.Background()

	count := 0
	for _, visibility := range []string{"public", "unlisted", "private"} {
		for _, owned := range []bool{false, true} {
			for _, quickPreview := range []bool{false, true} {
				for _, deleted := range []bool{false, true} {
					for _, expires := range []string{"never", "past", "future"} {
						for _, hasRevision := range []bool{false, true} {
							// The entries CHECKs: only owned entries leave public,
							// and a quick preview always expires.
							if (visibility != "public" && !owned) || (quickPreview && expires == "never") {
								continue
							}
							count++
							id := fmt.Sprintf("lifecycle%03d", count)
							var ownerID *string
							if owned {
								ownerID = &owner
							}
							if _, err := pool.Exec(ctx, `
								INSERT INTO entries (id, owner_user_id, visibility, quick_preview, deleted_at, expires_at)
								VALUES ($1, $2, $3, $4,
									CASE WHEN $5 THEN now() END,
									CASE $6 WHEN 'past' THEN now() - interval '1 hour'
									        WHEN 'future' THEN now() + interval '1 hour' END)`,
								id, ownerID, visibility, quickPreview, deleted, expires); err != nil {
								t.Fatalf("insert entry %s: %v", id, err)
							}
							if hasRevision {
								revision := fmt.Sprintf("lifecyrev%03d", count)
								if _, err := pool.Exec(ctx, `
									INSERT INTO revisions (id, entry_id, version_number, original_string, tree)
									VALUES ($1, $2, 1, '', '{}')`, revision, id); err != nil {
									t.Fatalf("insert revision %s: %v", revision, err)
								}
								if _, err := pool.Exec(ctx,
									"UPDATE entries SET current_revision_id = $1 WHERE id = $2", revision, id); err != nil {
									t.Fatalf("set revision of %s: %v", id, err)
								}
							}
						}
					}
				}
			}
		}
	}

	rows, err := pool.Query(ctx, `
		SELECT id, owner_user_id, visibility, quick_preview, deleted_at, expires_at, current_revision_id,
		       now(), entry_live(entries), entry_listed(entries), entry_lasting(entries),
		       entry_visible_to(entries, NULL), entry_visible_to(entries, $1), entry_visible_to(entries, $2)
		FROM entries WHERE id LIKE 'lifecycle%'`, owner, other)
	if err != nil {
		t.Fatalf("read entries: %v", err)
	}
	defer rows.Close()
	checked := 0
	for rows.Next() {
		var entry db.Entry
		var now pgtype.Timestamptz
		var live, listed, lasting, visibleSignedOut, visibleOwner, visibleOther bool
		if err := rows.Scan(&entry.ID, &entry.OwnerUserID, &entry.Visibility, &entry.QuickPreview,
			&entry.DeletedAt, &entry.ExpiresAt, &entry.CurrentRevisionID,
			&now, &live, &listed, &lasting, &visibleSignedOut, &visibleOwner, &visibleOther); err != nil {
			t.Fatalf("scan: %v", err)
		}
		checked++
		if got := catalog.EntryLive(entry, now.Time); got != live {
			t.Errorf("%s %+v: Go live = %v, SQL entry_live = %v", entry.ID, entry, got, live)
		}
		if want := live && !entry.QuickPreview; lasting != want {
			t.Errorf("%s %+v: SQL entry_lasting = %v, want %v", entry.ID, entry, lasting, want)
		}
		if got := catalog.EntryListed(entry, now.Time); got != listed {
			t.Errorf("%s %+v: Go listed = %v, SQL entry_listed = %v", entry.ID, entry, got, listed)
		}
		for viewer, want := range map[string]bool{"": visibleSignedOut, owner: visibleOwner, other: visibleOther} {
			if got := catalog.VisibleTo(entry.Visibility, entry.OwnerUserID, viewer); got != want {
				t.Errorf("%s %+v viewer %q: Go visibleTo = %v, SQL entry_visible_to = %v",
					entry.ID, entry, viewer, got, want)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read entries: %v", err)
	}
	if checked != count || count < 30 {
		t.Fatalf("checked %d of %d rows", checked, count)
	}
}

// catalog.BanActive and its SQL twin ban_active agree on every kind of ban.
func TestBanActiveMatchesSQL(t *testing.T) {
	_, pool, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	count := 0
	for _, expires := range []string{"never", "past", "future"} {
		for _, revoked := range []bool{false, true} {
			count++
			if _, err := pool.Exec(ctx, `
				INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by, expires_at, revoked_at)
				VALUES ($1, $2, ARRAY['upload'], 'spam', 'staff',
					CASE $3 WHEN 'past' THEN now() - interval '1 hour'
					        WHEN 'future' THEN now() + interval '1 hour' END,
					CASE WHEN $4 THEN now() END)`,
				fmt.Sprintf("banactive%03d", count), user, expires, revoked); err != nil {
				t.Fatalf("insert ban: %v", err)
			}
		}
	}
	rows, err := pool.Query(ctx, `
		SELECT id, expires_at, revoked_at, now(), ban_active(user_bans)
		FROM user_bans WHERE id LIKE 'banactive%'`)
	if err != nil {
		t.Fatalf("read bans: %v", err)
	}
	defer rows.Close()
	checked := 0
	for rows.Next() {
		var ban db.UserBan
		var now pgtype.Timestamptz
		var active bool
		if err := rows.Scan(&ban.ID, &ban.ExpiresAt, &ban.RevokedAt, &now, &active); err != nil {
			t.Fatalf("scan: %v", err)
		}
		checked++
		if got := catalog.BanActive(ban, now.Time); got != active {
			t.Errorf("%s: Go BanActive = %v, SQL ban_active = %v", ban.ID, got, active)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read bans: %v", err)
	}
	if checked != count {
		t.Fatalf("checked %d of %d bans", checked, count)
	}
}
