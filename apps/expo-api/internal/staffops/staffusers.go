package staffops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Staff console -> Users (docs/admin.md#users): every account,
// a force logout, and deleting an account (step one of account deletion,
// accountdelete.go).

// staffUserPageSize is how many accounts one Users page lists.
const staffUserPageSize = 50

// staffUserEntryLimit caps the entries an account's page lists.
const staffUserEntryLimit = 50

// UserStatuses are the Users page filters.
var UserStatuses = []string{"all", "active", "banned", "deleted"}

// StaffUser is an account as admins see it.
type StaffUser struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at"`
	// Entries and Comments count the live ones.
	Entries  int64 `json:"entries"`
	Comments int64 `json:"comments"`
	// BanActions are what the account's active bans block, sorted.
	BanActions []string `json:"ban_actions"`
	// Providers are the linked sign-in providers, sorted; Passkeys counts
	// the account's passkeys.
	Providers []string `json:"providers"`
	Passkeys  int64    `json:"passkeys"`
}

// StaffUserCounts are how many accounts each filter shows.
type StaffUserCounts struct {
	All     int64 `json:"all"`
	Active  int64 `json:"active"`
	Banned  int64 `json:"banned"`
	Deleted int64 `json:"deleted"`
}

// StaffUserPage is one page of the Users list.
type StaffUserPage struct {
	Users  []StaffUser     `json:"users"`
	Counts StaffUserCounts `json:"counts"`
	// NextCursor fetches the next (older) page; empty on the last one.
	NextCursor string `json:"next_cursor"`
}

// StaffUserQuery filters the Users list.
type StaffUserQuery struct {
	// Search matches part of the username.
	Search string
	// Status is one of UserStatuses; empty means all.
	Status string
	// Cursor is a previous page's NextCursor.
	Cursor string
}

// ListStaffUsers lists accounts newest first, deleted ones included.
func (s *Service) ListStaffUsers(ctx context.Context, by audit.Staff, query StaffUserQuery) (StaffUserPage, error) {
	status := query.Status
	if status == "" {
		status = "all"
	}
	if !slices.Contains(UserStatuses, status) {
		return StaffUserPage{}, invalidStaffRequest("status must be all, active, banned or deleted")
	}
	params := db.ListStaffUsersParams{
		Query: strings.TrimSpace(query.Search), Status: status, PageSize: staffUserPageSize + 1,
	}
	if query.Cursor != "" {
		at, id, ok := ParseTimeCursor(query.Cursor)
		if !ok {
			return StaffUserPage{}, invalidStaffRequest("invalid cursor")
		}
		params.BeforeCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.BeforeID = id
	}
	rows, err := s.queries.ListStaffUsers(ctx, params)
	if err != nil {
		return StaffUserPage{}, fmt.Errorf("list users: %w", err)
	}
	counts, err := s.queries.StaffUserCounts(ctx)
	if err != nil {
		return StaffUserPage{}, fmt.Errorf("count users: %w", err)
	}
	page := StaffUserPage{
		Users:  make([]StaffUser, 0, min(len(rows), staffUserPageSize)),
		Counts: StaffUserCounts{All: counts.Total, Active: counts.Active, Banned: counts.Banned, Deleted: counts.Deleted},
	}
	for i, row := range rows {
		if i == staffUserPageSize {
			last := page.Users[len(page.Users)-1]
			page.NextCursor = FormatTimeCursor(last.CreatedAt, last.ID)
			break
		}
		page.Users = append(page.Users, StaffUser{
			ID: row.ID, Username: row.Username, CreatedAt: row.CreatedAt.Time,
			DeletedAt: optionalTime(row.DeletedAt), Entries: row.Entries, Comments: row.Comments,
			BanActions: row.BanActions, Providers: row.Providers, Passkeys: row.Passkeys,
		})
	}
	return page, nil
}

// FormatTimeCursor is a keyset cursor of a row's time and id:
// "<RFC 3339 nano>_<id>".
func FormatTimeCursor(at time.Time, id string) string {
	return at.UTC().Format(time.RFC3339Nano) + "_" + id
}

// ParseTimeCursor reads a FormatTimeCursor cursor.
func ParseTimeCursor(raw string) (time.Time, string, bool) {
	stamp, id, ok := strings.Cut(raw, "_")
	if !ok {
		return time.Time{}, "", false
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}, "", false
	}
	if _, err := parseID(id); err != nil {
		return time.Time{}, "", false
	}
	return at, id, true
}

// StaffUserEntry is one of an account's entries.
type StaffUserEntry struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Visibility   string    `json:"visibility"`
	QuickPreview bool      `json:"quick_preview"`
	CreatedAt    time.Time `json:"created_at"`
	Deleted      bool      `json:"deleted"`
}

// StaffUserDetail is one account's page.
type StaffUserDetail struct {
	StaffUser
	DeletedEntries int64 `json:"deleted_entries"`
	Favorites      int64 `json:"favorites"`
	Collections    int64 `json:"collections"`
	ReportsFiled   int64 `json:"reports_filed"`
	// Status and Links are the account's public profile text.
	Status        string               `json:"status"`
	Links         []catalog.SocialLink `json:"links"`
	RecentEntries []StaffUserEntry     `json:"recent_entries"`
	// Bans are every ban of the account, newest first.
	Bans []StaffBan `json:"bans"`
}

// GetStaffUser reads one account with its entries and bans.
func (s *Service) GetStaffUser(ctx context.Context, by audit.Staff, userID string) (StaffUserDetail, error) {
	userID, err := parseID(userID)
	if err != nil {
		return StaffUserDetail{}, err
	}
	row, err := s.queries.GetStaffUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffUserDetail{}, catalog.ErrNotFound
	}
	if err != nil {
		return StaffUserDetail{}, fmt.Errorf("read user: %w", err)
	}
	entries, err := s.queries.ListStaffUserEntries(ctx, db.ListStaffUserEntriesParams{
		OwnerUserID: pgText(userID), RowLimit: staffUserEntryLimit,
	})
	if err != nil {
		return StaffUserDetail{}, fmt.Errorf("list user entries: %w", err)
	}
	bans, err := s.queries.ListUserBansByUser(ctx, userID)
	if err != nil {
		return StaffUserDetail{}, fmt.Errorf("list user bans: %w", err)
	}
	links, err := catalog.ReadSocialLinks(ctx, s.queries, userID)
	if err != nil {
		return StaffUserDetail{}, err
	}
	now := time.Now()
	detail := StaffUserDetail{
		ID: row.ID, Username: row.Username, CreatedAt: row.CreatedAt.Time,
		DeletedAt: optionalTime(row.DeletedAt), Entries: row.Entries, Comments: row.Comments,
		BanActions: []string{}, Providers: row.Providers, Passkeys: row.Passkeys,
		DeletedEntries: row.DeletedEntries, Favorites: row.Favorites, Collections: row.Collections,
		ReportsFiled:  row.ReportsFiled,
		Status:        row.Status,
		Links:         links,
		RecentEntries: make([]StaffUserEntry, 0, len(entries)),
		Bans:          make([]StaffBan, 0, len(bans)),
	}
	for _, entry := range entries {
		detail.RecentEntries = append(detail.RecentEntries, StaffUserEntry{
			ID: entry.ID, Title: entry.Title, Visibility: entry.Visibility, QuickPreview: entry.QuickPreview,
			CreatedAt: entry.CreatedAt.Time, Deleted: entry.DeletedAt.Valid,
		})
	}
	for _, ban := range bans {
		staffBan := staffBanFrom(ban, row.Username, now)
		detail.Bans = append(detail.Bans, staffBan)
		if staffBan.Active {
			detail.BanActions = append(detail.BanActions, ban.Actions...)
		}
	}
	slices.Sort(detail.BanActions)
	detail.BanActions = slices.Compact(detail.BanActions)
	return detail, nil
}

// StaffLogoutUser signs an account out everywhere: every session is deleted,
// so its tokens stop working at once.
func (s *Service) StaffLogoutUser(ctx context.Context, by audit.Staff, userID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if userID, err = parseID(userID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		user, err := catalog.LockLiveUser(ctx, queries, userID)
		if err != nil {
			return err
		}
		if _, err := queries.DeleteUserSessions(ctx, user.ID); err != nil {
			return fmt.Errorf("end sessions: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "user.logout", TargetType: "user", TargetID: user.ID, Reason: reason,
		})
	})
}

// StaffClearProfile empties an account's status line and social links (text
// someone reported, say); pins stay, as they show only public entries.
func (s *Service) StaffClearProfile(ctx context.Context, by audit.Staff, userID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if userID, err = parseID(userID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		user, err := catalog.LockLiveUser(ctx, queries, userID)
		if err != nil {
			return err
		}
		links, err := catalog.ReadSocialLinks(ctx, queries, user.ID)
		if err != nil {
			return err
		}
		if err := catalog.ClearProfile(ctx, queries, user.ID); err != nil {
			return err
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "user.clear_profile", TargetType: "user", TargetID: user.ID, Reason: reason,
			Before: map[string]any{"status": user.Status, "links": links},
		})
	})
}

// StaffDeleteUser deletes an account (admins, reason required): every
// session ended and every sign-in method deleted (its provider accounts may
// sign up anew at once, unless a ban holds them), profile gone, every entry
// it owns soft-deleted and removed from search, every comment tombstoned
// ("removed by staff"), favorites and collections removed, and the open
// reports of its entries and comments resolved (deleteAccount). Bans stay,
// and the row keeps the username. The daily sweep hard-deletes the row and
// its entries after DeletedAccountHold (accountpurge.go); the deleted
// accounts cleanup can do it sooner.
func (s *Service) StaffDeleteUser(ctx context.Context, by audit.Staff, userID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if reason == "" {
		return invalidStaffRequest("a reason is required")
	}
	if userID, err = parseID(userID); err != nil {
		return err
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		user, err := catalog.LockLiveUser(ctx, c.Queries(), userID)
		if err != nil {
			return err
		}
		deleted, err := s.deletion.Delete(ctx, c, user, accountdeletion.ByStaff)
		if err != nil {
			return err
		}
		var entryReports int64
		for _, entryID := range deleted.EntryIDs {
			resolved, err := c.ResolveEntryReports(ctx, entryID, by.Username)
			if err != nil {
				return err
			}
			entryReports += resolved
		}
		commentReports, err := c.Queries().ResolveUserCommentReports(ctx, db.ResolveUserCommentReportsParams{
			ResolvedBy: by.Username, UserID: user.ID,
		})
		if err != nil {
			return fmt.Errorf("resolve comment reports: %w", err)
		}
		return audit.Write(ctx, c.Queries(), by, audit.Record{
			Action: "user.delete", TargetType: "user", TargetID: user.ID, Reason: reason,
			Before: map[string]any{"deleted": false},
			After: map[string]any{
				"deleted": true, "entries_deleted": len(deleted.EntryIDs), "comments_removed": deleted.Comments,
				"favorites_removed": deleted.Favorites, "collections_removed": deleted.Collections,
				"reports_resolved":        entryReports + commentReports,
				"sign_in_methods_removed": deleted.Methods, "ban_held": deleted.Held,
			},
		})
	})
}

// deletionQueueLimit bounds the deletion queue page.
const deletionQueueLimit = 500

// DeletedAccount is an account waiting for its hard delete.
type DeletedAccount struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	DeletedAt time.Time `json:"deleted_at"`
	// PurgeAt is when the daily sweep hard-deletes it (DeletedAccountHold
	// after the deletion); an account still there after it is overdue (its
	// entries wait for their search removal).
	PurgeAt time.Time `json:"purge_at"`
	// Entries counts the entries it still owns (all soft-deleted).
	Entries int64 `json:"entries"`
	Banned  bool  `json:"banned"`
}

// ListDeletionQueue lists deleted accounts, the oldest deletion (the
// nearest hard delete) first.
func (s *Service) ListDeletionQueue(ctx context.Context, by audit.Staff) ([]DeletedAccount, error) {
	rows, err := s.queries.ListDeletedAccounts(ctx, deletionQueueLimit)
	if err != nil {
		return nil, fmt.Errorf("list deleted accounts: %w", err)
	}
	accounts := make([]DeletedAccount, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, DeletedAccount{
			ID: row.ID, Username: row.Username, DeletedAt: row.DeletedAt.Time,
			PurgeAt: row.DeletedAt.Time.Add(accountdeletion.DeletedAccountHold), Entries: row.Entries, Banned: row.Banned,
		})
	}
	return accounts, nil
}
