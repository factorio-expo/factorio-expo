package catalog

import (
	"context"
	"fmt"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// FeedLimit is how many items a user's feed lists.
const FeedLimit = 30

// FeedItem is one thing that happened to a listed entry of a user: it was
// published (its first revision) or got a new revision.
type FeedItem struct {
	EntryID       string `json:"entry_id" doc:"The entry's public id."`
	Title         string `json:"title" doc:"The entry's title (its root label when it has none)."`
	RevisionID    string `json:"revision_id" doc:"The revision's id."`
	VersionNumber int32  `json:"version_number" doc:"The revision's number."`
	// FirstRevision items carry the entry's description; later ones the note.
	FirstRevision bool      `json:"first_revision" doc:"Whether this is the entry's first revision: the entry was published."`
	Description   string    `json:"description,omitempty" doc:"The entry's description; only on a first revision."`
	Note          string    `json:"note,omitempty" doc:"The revision's note; only on a later revision, and only when it has one."`
	CreatedAt     time.Time `json:"created_at" doc:"When the revision was made."`
}

// UserFeed is a user's recent activity, newest first.
type UserFeed struct {
	Username string     `json:"username" doc:"The user's username as the account spells it."`
	Items    []FeedItem `json:"items" doc:"The last revisions of the user's listed entries, newest first (at most 30)."`
}

// UserFeed lists the newest revisions (the first of an entry is its
// publication) of a user's live, public entries, never quick previews,
// unlisted or private ones, or anonymous ones. An unknown or deleted account
// is ErrNotFound.
func (s *Service) UserFeed(ctx context.Context, username string) (UserFeed, error) {
	user, err := s.findUser(ctx, username)
	if err != nil {
		return UserFeed{}, err
	}
	rows, err := s.queries.ListUserFeedItems(ctx, db.ListUserFeedItemsParams{
		OwnerUserID: user.ID, ItemLimit: FeedLimit,
	})
	if err != nil {
		return UserFeed{}, fmt.Errorf("list user feed: %w", err)
	}
	feed := UserFeed{Username: user.Username, Items: make([]FeedItem, 0, len(rows))}
	for _, row := range rows {
		item := FeedItem{
			EntryID: row.EntryID, Title: row.Title, RevisionID: row.RevisionID,
			VersionNumber: row.VersionNumber, FirstRevision: row.FirstRevision, CreatedAt: row.CreatedAt.Time,
		}
		if row.FirstRevision {
			item.Description = row.Description
		} else {
			item.Note = row.Note
		}
		feed.Items = append(feed.Items, item)
	}
	return feed, nil
}
