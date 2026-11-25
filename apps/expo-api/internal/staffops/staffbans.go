package staffops

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// Staff bans (docs/admin.md): from commenting, uploading or reporting, for
// a number of days or permanently. A user ban becomes short address bans by itself (checkBan); staff
// see and lift address bans by their hash only.

// MaxBanNoteRunes bounds a ban's staff note.
const MaxBanNoteRunes = 1000

// banListLimit caps each ban list.
const banListLimit = 200

var banReasons = []string{"spam", "abuse", "illegal_content", "other"}

func validBanReason(code string) bool { return slices.Contains(banReasons, code) }

func newBanID() string { return shortid.New() }

// StaffBan is a user ban as staff see it.
type StaffBan struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Actions    []string  `json:"actions"`
	ReasonCode string    `json:"reason_code"`
	Note       string    `json:"note"`
	BannedBy   string    `json:"banned_by"`
	CreatedAt  time.Time `json:"created_at"`
	// ExpiresAt is nil for a permanent ban.
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	RevokedBy string     `json:"revoked_by,omitempty"`
	Active    bool       `json:"active"`
}

func staffBanFrom(ban db.UserBan, username string, now time.Time) StaffBan {
	return StaffBan{
		ID: ban.ID, Username: username, Actions: ban.Actions, ReasonCode: ban.ReasonCode, Note: ban.Note,
		BannedBy: ban.BannedBy, CreatedAt: ban.CreatedAt.Time,
		ExpiresAt: optionalTime(ban.ExpiresAt), RevokedAt: optionalTime(ban.RevokedAt),
		RevokedBy: ban.RevokedBy.String, Active: catalog.BanActive(ban, now),
	}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

// ListBans lists user bans, newest first: the active ones or those that
// ended, of every user or one (username, any case).
func (s *Service) ListBans(ctx context.Context, username string, active bool) ([]StaffBan, error) {
	rows, err := s.queries.ListUserBans(ctx, db.ListUserBansParams{
		Username: strings.TrimSpace(username), Active: active, RowLimit: banListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list user bans: %w", err)
	}
	now := time.Now()
	bans := make([]StaffBan, 0, len(rows))
	for _, row := range rows {
		bans = append(bans, staffBanFrom(db.UserBan{
			ID: row.ID, UserID: row.UserID, Actions: row.Actions, ReasonCode: row.ReasonCode, Note: row.Note,
			BannedBy: row.BannedBy, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
			RevokedAt: row.RevokedAt, RevokedBy: row.RevokedBy,
		}, row.Username, now))
	}
	return bans, nil
}

// NewBan is a staff member's ban of an account.
type NewBan struct {
	Username string `json:"username"`
	// Actions are comment, upload and/or report.
	Actions    []string `json:"actions"`
	ReasonCode string   `json:"reason_code"`
	// Note is the staff note kept on the ban (and in the audit log).
	Note string `json:"note"`
	// ExpiresAt is when the ban ends, unless Permanent.
	ExpiresAt *time.Time `json:"expires_at"`
	Permanent bool       `json:"permanent"`
}

// banEnd reads an end from an expiry and a permanent flag: exactly one of
// them is set.
func banEnd(expiresAt *time.Time, permanent bool) (*time.Time, error) {
	if permanent == (expiresAt != nil) {
		return nil, invalidStaffRequest("give either an end (expires_at) or permanent")
	}
	return expiresAt, nil
}

// CreateBan bans an account from the given actions.
func (s *Service) CreateBan(ctx context.Context, by audit.Staff, request NewBan) (StaffBan, error) {
	actions, err := cleanBanActions(request.Actions)
	if err != nil {
		return StaffBan{}, err
	}
	if !validBanReason(request.ReasonCode) {
		return StaffBan{}, invalidStaffRequest("reason_code must be spam, abuse, illegal_content or other")
	}
	note := strings.TrimSpace(request.Note)
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > MaxBanNoteRunes {
		return StaffBan{}, invalidStaffRequest("the note is at most %d characters", MaxBanNoteRunes)
	}
	expiresAt, err := banEnd(request.ExpiresAt, request.Permanent)
	if err != nil {
		return StaffBan{}, err
	}
	now := time.Now()
	if err := checkBanEnd(expiresAt, now); err != nil {
		return StaffBan{}, err
	}
	var ban StaffBan
	err = s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		user, err := queries.GetUserByUsername(ctx, strings.TrimSpace(request.Username))
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		row, err := queries.CreateUserBan(ctx, db.CreateUserBanParams{
			ID: newBanID(), UserID: user.ID, Actions: actions, ReasonCode: request.ReasonCode,
			Note: note, BannedBy: by.Username, ExpiresAt: timestamptz(expiresAt),
		})
		if err != nil {
			return fmt.Errorf("create user ban: %w", err)
		}
		ban = staffBanFrom(row, user.Username, now)
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "ban.create", TargetType: "user", TargetID: user.ID, Reason: note,
			After: map[string]any{
				"ban": row.ID, "actions": actions, "reason_code": row.ReasonCode, "expires_at": expiresAt,
			},
		})
	})
	return ban, err
}

// BanEnd is a new end for a ban: a time, or permanent (admins only).
type BanEnd struct {
	ExpiresAt *time.Time `json:"expires_at"`
	Permanent bool       `json:"permanent"`
	Reason    string     `json:"reason"`
}

// ChangeBanEnd moves an active ban's end.
func (s *Service) ChangeBanEnd(ctx context.Context, by audit.Staff, banID string, request BanEnd) error {
	reason, err := audit.CleanReason(request.Reason)
	if err != nil {
		return err
	}
	expiresAt, err := banEnd(request.ExpiresAt, request.Permanent)
	if err != nil {
		return err
	}
	return s.changeActiveBan(ctx, by, banID, func(queries *db.Queries, row db.UserBan, now time.Time) (audit.Record, error) {
		if err := checkBanEnd(expiresAt, now); err != nil {
			return audit.Record{}, err
		}
		if _, err := queries.SetUserBanEnd(ctx, db.SetUserBanEndParams{
			ID: row.ID, ExpiresAt: timestamptz(expiresAt),
		}); err != nil {
			return audit.Record{}, fmt.Errorf("change ban end: %w", err)
		}
		return audit.Record{
			Action: "ban.change_end", TargetType: "ban", TargetID: row.ID, Reason: reason,
			Before: map[string]any{"expires_at": optionalTime(row.ExpiresAt)},
			After:  map[string]any{"expires_at": expiresAt},
		}, nil
	})
}

// RevokeBan ends an active ban now. Address bans the account caused stay until they expire
// (hours) or are lifted.
func (s *Service) RevokeBan(ctx context.Context, by audit.Staff, banID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	return s.changeActiveBan(ctx, by, banID, func(queries *db.Queries, row db.UserBan, _ time.Time) (audit.Record, error) {
		if _, err := queries.RevokeUserBan(ctx, db.RevokeUserBanParams{
			ID: row.ID, RevokedBy: pgText(by.Username),
		}); err != nil {
			return audit.Record{}, fmt.Errorf("revoke ban: %w", err)
		}
		return audit.Record{
			Action: "ban.revoke", TargetType: "ban", TargetID: row.ID, Reason: reason,
			Before: map[string]any{"active": true}, After: map[string]any{"active": false},
		}, nil
	})
}

// changeActiveBan locks an active ban, applies change and records it.
func (s *Service) changeActiveBan(
	ctx context.Context, by audit.Staff, banID string,
	change func(*db.Queries, db.UserBan, time.Time) (audit.Record, error),
) error {
	banID, err := parseID(banID)
	if err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		row, err := queries.GetUserBanForUpdate(ctx, banID)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock ban: %w", err)
		}
		now := time.Now()
		if !catalog.BanActive(row, now) {
			return catalog.ErrNotFound
		}
		record, err := change(queries, row, now)
		if err != nil {
			return err
		}
		return audit.Write(ctx, queries, by, record)
	})
}

// cleanBanActions checks, de-duplicates and orders a ban's actions.
func cleanBanActions(actions []string) ([]string, error) {
	cleaned := []string{}
	for _, action := range actions {
		switch catalog.BanAction(action) {
		case catalog.BanComment, catalog.BanUpload, catalog.BanReport:
		default:
			return nil, invalidStaffRequest("actions must be comment, upload or report")
		}
		if !slices.Contains(cleaned, action) {
			cleaned = append(cleaned, action)
		}
	}
	if len(cleaned) == 0 {
		return nil, invalidStaffRequest("a ban needs at least one action")
	}
	slices.Sort(cleaned)
	return cleaned, nil
}

// checkBanEnd checks a ban's end is in the future (nil is permanent).
func checkBanEnd(expiresAt *time.Time, now time.Time) error {
	if expiresAt != nil && !expiresAt.After(now) {
		return invalidStaffRequest("the ban must end in the future")
	}
	return nil
}

// banEndInDays is the end of a days-long ban.
func banEndInDays(days int) (time.Time, error) {
	if days < 1 || days > 3650 {
		return time.Time{}, invalidStaffRequest("days must be 1 or more")
	}
	return time.Now().Add(time.Duration(days) * 24 * time.Hour), nil
}

func timestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

// StaffIPBan is an active address ban: the address only as its keyed hash.
type StaffIPBan struct {
	// Hash is the hex HMAC of the address.
	Hash       string    `json:"hash"`
	Action     string    `json:"action"`
	ReasonCode string    `json:"reason_code"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ListIPBans lists the active address bans, the longest-running first.
func (s *Service) ListIPBans(ctx context.Context) ([]StaffIPBan, error) {
	rows, err := s.queries.ListActiveIPBans(ctx, banListLimit)
	if err != nil {
		return nil, fmt.Errorf("list address bans: %w", err)
	}
	bans := make([]StaffIPBan, 0, len(rows))
	for _, row := range rows {
		bans = append(bans, StaffIPBan{
			Hash: hex.EncodeToString(row.IpHash), Action: row.Action, ReasonCode: row.ReasonCode,
			CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		})
	}
	return bans, nil
}

// LiftIPBan ends an address ban early: for one action, or every action when
// action is empty. Either role may.
func (s *Service) LiftIPBan(ctx context.Context, by audit.Staff, hash, action, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	ipHash, err := hex.DecodeString(hash)
	if err != nil || len(ipHash) != 32 {
		return catalog.ErrNotFound
	}
	if action != "" && !slices.Contains([]catalog.BanAction{catalog.BanComment, catalog.BanUpload, catalog.BanReport}, catalog.BanAction(action)) {
		return invalidStaffRequest("action must be comment, upload or report")
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		lifted, err := queries.LiftIPBan(ctx, db.LiftIPBanParams{IpHash: ipHash, Action: action})
		if err != nil {
			return fmt.Errorf("lift address ban: %w", err)
		}
		if lifted == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "ip_ban.lift", TargetType: "ip_ban", TargetID: hex.EncodeToString(ipHash), Reason: reason,
			After: map[string]any{"action": action, "lifted": lifted},
		})
	})
}
