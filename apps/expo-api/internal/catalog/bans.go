package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
)

// BanAction is one thing a ban can block (user_bans.actions, ip_bans.action).
type BanAction string

const (
	BanComment BanAction = "comment"
	BanUpload  BanAction = "upload"
	BanReport  BanAction = "report"
)

// Ban is an active ban as the banned caller sees it.
type Ban struct {
	Action BanAction `json:"action" doc:"What the ban blocks (upload, comment, report or favorite)."`
	// ReasonCode is spam, abuse, illegal_content or other.
	ReasonCode string `json:"reason_code" doc:"Why: spam, abuse, illegal_content or other."`
	// ExpiresAt is nil for a permanent ban.
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When the ban ends; absent for a permanent ban."`
}

// BanError is returned when an active ban blocks the action.
type BanError struct{ Ban Ban }

func (e *BanError) Error() string {
	message := fmt.Sprintf("You are banned from %s", banVerb(e.Ban.Action))
	if reason := banReason(e.Ban.ReasonCode); reason != "" {
		message += " for " + reason
	}
	if e.Ban.ExpiresAt != nil {
		message += " until " + e.Ban.ExpiresAt.UTC().Format("2 January 2006, 15:04 UTC")
	}
	return message + "."
}

// banReason words a reason code for the banned person ("" for other).
func banReason(code string) string {
	switch code {
	case "spam":
		return "spam"
	case "abuse":
		return "abusive behaviour"
	case "illegal_content":
		return "illegal content"
	}
	return ""
}

// Fault answers it as banned (403).
func (e *BanError) Fault() *fault.Error {
	return fault.New(fault.Forbidden, "banned", e.Error())
}

func banVerb(action BanAction) string {
	switch action {
	case BanComment:
		return "commenting"
	case BanUpload:
		return "uploading"
	case BanReport:
		return "reporting"
	}
	return string(action)
}

// Requester is who is asking, for ban checks: the signed-in user (empty when
// anonymous) and the keyed hash of their address (nil when unknown).
type Requester struct {
	UserID string
	IPHash []byte
}

// checkBan returns a *BanError when an active ban blocks action for the
// requester's account or address. A banned account also bans the address it
// tried from (for every action its ban covers) for the IP ban duration, so
// signing out or a fresh account does not get around it there. Every action
// a ban covers checks it at the start of its catalog operation (comments,
// reports, and the uploads through allowUpload), outside the transaction the
// ban error would roll back.
func (s *Service) checkBan(ctx context.Context, requester Requester, action BanAction) error {
	ban, actions, err := s.accountBan(ctx, requester.UserID, action)
	if err != nil {
		return err
	}
	if ban != nil {
		if requester.IPHash != nil {
			if err := s.banIP(ctx, requester.IPHash, *ban, actions); err != nil {
				return err
			}
		}
		return &BanError{Ban: *ban}
	}
	if requester.IPHash == nil {
		return nil
	}
	row, err := s.queries.GetActiveIPBan(ctx, db.GetActiveIPBanParams{IpHash: requester.IPHash, Action: string(action)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read address ban: %w", err)
	}
	return &BanError{Ban: banFrom(action, row.ReasonCode, row.ExpiresAt)}
}

// allowUpload refuses an upload (new entry, revision, quick preview or
// publish: publishing makes an upload lasting) from a user or address banned
// from uploading.
func (s *Service) allowUpload(ctx context.Context, userID string, ipHash []byte) error {
	return s.checkBan(ctx, Requester{UserID: userID, IPHash: ipHash}, BanUpload)
}

// accountBan returns the account's ban blocking action (nil when none) and
// every action that ban covers.
func (s *Service) accountBan(ctx context.Context, userID string, action BanAction) (*Ban, []string, error) {
	if userID == "" {
		return nil, nil, nil
	}
	row, err := s.queries.GetActiveUserBan(ctx, db.GetActiveUserBanParams{UserID: userID, Action: string(action)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read user ban: %w", err)
	}
	ban := banFrom(action, row.ReasonCode, row.ExpiresAt)
	return &ban, row.Actions, nil
}

// banIP bans an address for the IP ban duration, or until the user ban ends
// if that is sooner.
func (s *Service) banIP(ctx context.Context, ipHash []byte, ban Ban, actions []string) error {
	expires := time.Now().Add(s.ipBanDuration)
	if ban.ExpiresAt != nil && ban.ExpiresAt.Before(expires) {
		expires = *ban.ExpiresAt
	}
	if err := s.queries.BanIP(ctx, db.BanIPParams{
		IpHash: ipHash, ReasonCode: ban.ReasonCode, Actions: actions,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return fmt.Errorf("ban address: %w", err)
	}
	return nil
}

func banFrom(action BanAction, reasonCode string, expiresAt pgtype.Timestamptz) Ban {
	ban := Ban{Action: action, ReasonCode: reasonCode}
	if expiresAt.Valid {
		expires := expiresAt.Time
		ban.ExpiresAt = &expires
	}
	return ban
}

// PruneBanData deletes expired address bans and ban holds and returns how
// many; the API runs it hourly, and staff can from the cleanups.
func (s *Service) PruneBanData(ctx context.Context) (int64, error) {
	pruned, err := s.queries.PruneExpiredIPBans(ctx)
	if err != nil {
		return 0, fmt.Errorf("prune expired address bans: %w", err)
	}
	holds, err := s.queries.PruneExpiredBanHolds(ctx)
	if err != nil {
		return pruned, fmt.Errorf("prune expired ban holds: %w", err)
	}
	return pruned + holds, nil
}
