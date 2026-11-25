// Package accountdeletion is the two steps of deleting an account
// (docs/admin.md#account-deletion), shared by the owner (DELETE /api/v1/me) and
// staff (the console): step one, here, makes the account stop working at once;
// step two (purge.go) hard-deletes it with its entries a week later. Everything
// goes through the catalog's unit of work, so the entries are unindexed and
// their cached previews purged like any other delete.
package accountdeletion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// Who deleted an account, as its comment tombstones say (comments.deleted_by).
const (
	ByStaff = "admin"
	ByOwner = "account"
)

// Service deletes accounts.
type Service struct {
	catalog *catalog.Service
	queries *db.Queries
	// banHolds hashes the usernames and identities an active ban keeps
	// refused after the account row is gone.
	banHolds *banhold.Hasher
	// ledger records the ids of hard-deleted accounts; nil records nothing.
	ledger *Ledger
}

// New builds the deletion service over the catalog; banHolds is required.
func New(catalogService *catalog.Service, banHolds *banhold.Hasher) *Service {
	return &Service{catalog: catalogService, queries: catalogService.Queries(), banHolds: banHolds}
}

// WithLedger makes the hard deletes record the account ids in ledger.
func (s *Service) WithLedger(ledger *Ledger) *Service {
	s.ledger = ledger
	return s
}

// Deleted is what deleting an account changed.
type Deleted struct {
	// EntryIDs are the entries it owned, now soft-deleted.
	EntryIDs                                  []string
	Comments, Favorites, Collections, Methods int64
	// Held is whether an active ban left ban holds behind.
	Held bool
}

// DeleteOwn deletes the caller's own account (DELETE /api/v1/me): everything
// staff's delete does, except that its comments are tombstoned as "deleted
// with the account" and open reports stay open for staff. catalog.ErrNotFound
// when the account is gone already.
func (s *Service) DeleteOwn(ctx context.Context, userID string) error {
	if !shortid.Valid(userID) {
		return catalog.ErrNotFound
	}
	var deleted Deleted
	err := s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		user, err := catalog.LockLiveUser(ctx, c.Queries(), userID)
		if err != nil {
			return err
		}
		deleted, err = s.Delete(ctx, c, user, ByOwner)
		return err
	})
	if err == nil {
		slog.Info("account deleted by its owner", "user_id", userID,
			"entries", len(deleted.EntryIDs), "ban_held", deleted.Held)
	}
	return err
}

// Delete soft-deletes a locked live account (catalog.LockLiveUser) inside c: marked deleted,
// every session and sign-in method deleted (after holding the username and
// identities when a ban is active), every owned entry soft-deleted and
// unindexed, every comment tombstoned (by is the tombstone's deleted_by),
// favorites, collections and the profile (status, links, pins) removed. Bans
// and reports stay. Anonymous entries are never touched (they have no owner).
func (s *Service) Delete(ctx context.Context, c *catalog.Unit, user db.User, by string) (Deleted, error) {
	var deleted Deleted
	if s.banHolds == nil {
		return deleted, errors.New("ban holds are not configured")
	}
	queries := c.Queries()
	if _, err := queries.MarkUserDeleted(ctx, user.ID); err != nil {
		return deleted, fmt.Errorf("mark user deleted: %w", err)
	}
	// Signs every device out: a session of a deleted account fails anyway
	// (GetLiveSession), but no row is left to come back.
	if _, err := queries.DeleteUserSessions(ctx, user.ID); err != nil {
		return deleted, fmt.Errorf("end sessions: %w", err)
	}
	banEnd, err := queries.ActiveBanEnd(ctx, user.ID)
	if err != nil {
		return deleted, fmt.Errorf("read ban end: %w", err)
	}
	identities, err := queries.DeleteUserIdentities(ctx, user.ID)
	if err != nil {
		return deleted, fmt.Errorf("delete identities: %w", err)
	}
	passkeys, err := queries.DeleteUserPasskeys(ctx, user.ID)
	if err != nil {
		return deleted, fmt.Errorf("delete passkeys: %w", err)
	}
	deleted.Methods = int64(len(identities)) + passkeys
	if banEnd.Valid {
		// The ban rows go with the account at the hard delete: keep its
		// username and provider accounts refused until the ban ends.
		identityHashes := make([][]byte, 0, len(identities))
		for _, identity := range identities {
			identityHashes = append(identityHashes, s.banHolds.Identity(identity.Provider, identity.ProviderUserID))
		}
		for kind, hashes := range map[string][][]byte{
			banhold.KindUsername: {s.banHolds.Username(user.Username)},
			banhold.KindIdentity: identityHashes,
		} {
			if len(hashes) == 0 {
				continue
			}
			if err := queries.HoldBan(ctx, db.HoldBanParams{Kind: kind, ExpiresAt: banEnd, Hashes: hashes}); err != nil {
				return deleted, fmt.Errorf("hold ban: %w", err)
			}
		}
		deleted.Held = true
	}
	if deleted.EntryIDs, err = queries.ListLiveOwnedEntriesForUpdate(ctx, pgtype.Text{String: user.ID, Valid: true}); err != nil {
		return deleted, fmt.Errorf("lock owned entries: %w", err)
	}
	for _, entryID := range deleted.EntryIDs {
		if err := c.DeleteEntry(ctx, entryID); err != nil {
			return deleted, err
		}
	}
	if deleted.Comments, err = queries.RemoveUserComments(ctx, db.RemoveUserCommentsParams{
		DeletedBy: by, UserID: user.ID,
	}); err != nil {
		return deleted, fmt.Errorf("remove comments: %w", err)
	}
	if deleted.Favorites, err = queries.DeleteUserFavorites(ctx, user.ID); err != nil {
		return deleted, fmt.Errorf("delete favorites: %w", err)
	}
	if deleted.Collections, err = queries.DeleteUserCollections(ctx, user.ID); err != nil {
		return deleted, fmt.Errorf("delete collections: %w", err)
	}
	// The profile goes: status line, social links and pins.
	if err := catalog.ClearProfile(ctx, queries, user.ID); err != nil {
		return deleted, err
	}
	if _, err := queries.DeleteUserPins(ctx, user.ID); err != nil {
		return deleted, fmt.Errorf("delete pins: %w", err)
	}
	return deleted, nil
}
