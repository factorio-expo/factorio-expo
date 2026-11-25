package dataexport

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The export's JSON shape (docs/api.md#account-data-export). Nullable times
// are null when unset.

// Format and Version name the document's shape; Version changes when it does.
const (
	Format  = "factorio-expo-account-export"
	Version = 1
)

// Account is the exported account (the export's "account" member).
type Account struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	// Status is the profile's status line ("" for none).
	Status string `json:"status"`
	// SocialLinks are the profile's links: a site and the name there.
	SocialLinks []socialLink `json:"social_links"`
}

type socialLink struct {
	Platform string `json:"platform"`
	Handle   string `json:"handle"`
}

// xpEvent is one row of the account's XP ledger.
type xpEvent struct {
	Reason      string    `json:"reason"`
	Points      int32     `json:"points"`
	EntryID     *string   `json:"entry_id"`
	CommentID   *string   `json:"comment_id"`
	ChallengeID *string   `json:"challenge_id"`
	Board       *string   `json:"board"`
	CreatedAt   time.Time `json:"created_at"`
}

type pinnedEntry struct {
	EntryID  string    `json:"entry_id"`
	Position int32     `json:"position"`
	PinnedAt time.Time `json:"pinned_at"`
}

type identity struct {
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	LinkedAt       time.Time `json:"linked_at"`
	LastUsedAt     time.Time `json:"last_used_at"`
}

// passkey is ids and dates only: never the public key or sign count.
type passkey struct {
	ID         string     `json:"id"`
	AddedAt    time.Time  `json:"added_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type session struct {
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastSignInAt time.Time `json:"last_sign_in_at"`
}

// entry is written without its revisions, which follow one at a time.
type entry struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Visibility   string     `json:"visibility"`
	QuickPreview bool       `json:"quick_preview"`
	Spoiler      bool       `json:"spoiler"`
	Tags         []string   `json:"tags"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	DeletedAt    *time.Time `json:"deleted_at"`
	// SourceURL is the link an imported blueprint came from, Alias the
	// entry's vanity slug; null when it has none.
	SourceURL *string `json:"source_url"`
	Alias     *string `json:"alias"`
}

type revision struct {
	ID          string    `json:"id"`
	Version     int32     `json:"version"`
	GameVersion string    `json:"game_version"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	// ExchangeString is the original string exactly as submitted.
	ExchangeString string `json:"exchange_string"`
}

type comment struct {
	EntryID        string     `json:"entry_id"`
	Number         int32      `json:"number"`
	RevisionNumber int32      `json:"revision_number"`
	Body           string     `json:"body"`
	CreatedAt      time.Time  `json:"created_at"`
	EditedAt       *time.Time `json:"edited_at"`
	RemovedAt      *time.Time `json:"removed_at"`
	// RemovedBy is author, owner or admin; empty while the comment stands.
	RemovedBy string `json:"removed_by"`
}

type favorite struct {
	EntryID   string    `json:"entry_id"`
	CreatedAt time.Time `json:"created_at"`
}

type collection struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Visibility  string            `json:"visibility"`
	CreatedAt   time.Time         `json:"created_at"`
	Entries     []collectionEntry `json:"entries"`
}

type collectionEntry struct {
	EntryID  string    `json:"entry_id"`
	Position int32     `json:"position"`
	AddedAt  time.Time `json:"added_at"`
}

type entryReport struct {
	ID             string     `json:"id"`
	EntryID        string     `json:"entry_id"`
	Reason         string     `json:"reason"`
	Note           string     `json:"note"`
	RevisionNumber int32      `json:"revision_number"`
	CreatedAt      time.Time  `json:"created_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
}

type commentReport struct {
	EntryID       string     `json:"entry_id"`
	CommentNumber int32      `json:"comment_number"`
	Reason        string     `json:"reason"`
	Note          string     `json:"note"`
	CreatedAt     time.Time  `json:"created_at"`
	ResolvedAt    *time.Time `json:"resolved_at"`
}

// optionalTime is a nullable column as a JSON time or null.
func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}

func optionalText(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}
