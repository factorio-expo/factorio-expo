package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
)

// The public profile's own parts (docs/api.md#profiles): the status line,
// social links, pinned entries, stats and the user's comments.

// Profile limits.
const (
	// MaxStatusLength is the status line's length in characters.
	MaxStatusLength = 140
	// MaxPinnedEntries is how many entries a profile pins.
	MaxPinnedEntries = 6
	// profileTopTags is how many tags the profile's stats name.
	profileTopTags = 5
)

var (
	// ErrInvalidProfile is a status line or social link the profile refuses;
	// the wrapped message says which.
	ErrInvalidProfile = fault.New(fault.Invalid, "invalid_profile", "invalid profile")
	// ErrPinLimit refuses a pin past MaxPinnedEntries.
	ErrPinLimit = fault.New(fault.Conflict, "pin_limit", fmt.Sprintf("you can pin at most %d entries: unpin one first", MaxPinnedEntries))
	// ErrNotPinnable refuses pinning an entry that is not the caller's own
	// public entry.
	ErrNotPinnable = fault.New(fault.Conflict, "not_pinnable", "only your own public entries can be pinned")
)

// invalidProfile is an ErrInvalidProfile whose text says what to fix.
func invalidProfile(format string, args ...any) error {
	return ErrInvalidProfile.Withf(format, args...)
}

// SocialLink is a profile link: the user's name on a site from a fixed list,
// and the link built from it.
type SocialLink struct {
	Platform string `json:"platform" doc:"The site's name."`
	Handle   string `json:"handle" doc:"The handle on the site."`
	URL      string `json:"url" doc:"The profile's link."`
}

// SocialLinkInput is one link as the user sets it: a site and the name there.
type SocialLinkInput struct {
	Platform string `json:"platform" doc:"The site's name."`
	Handle   string `json:"handle" doc:"The handle on the site."`
}

// socialPlatform is a site a profile may link: how a name there looks, what
// may be typed before it, and its profile URL.
type socialPlatform struct {
	name     string
	label    string
	pattern  *regexp.Regexp
	prefixes []string
	url      string
}

// socialPlatforms are the sites a profile links, in display order. Only a name
// is stored; the URL is always built here, so a profile never links elsewhere.
var socialPlatforms = []socialPlatform{
	{name: "youtube", label: "YouTube", pattern: regexp.MustCompile(`^[A-Za-z0-9._-]{3,30}$`),
		prefixes: []string{"@"}, url: "https://www.youtube.com/@"},
	{name: "twitch", label: "Twitch", pattern: regexp.MustCompile(`^[A-Za-z0-9_]{3,25}$`),
		url: "https://www.twitch.tv/"},
	{name: "github", label: "GitHub",
		pattern:  regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`),
		prefixes: []string{"@"}, url: "https://github.com/"},
	{name: "reddit", label: "Reddit", pattern: regexp.MustCompile(`^[A-Za-z0-9_-]{3,20}$`),
		prefixes: []string{"/u/", "u/"}, url: "https://www.reddit.com/user/"},
	{name: "bluesky", label: "Bluesky",
		pattern: regexp.MustCompile(
			`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`),
		prefixes: []string{"@"}, url: "https://bsky.app/profile/"},
	{name: "steam", label: "Steam", pattern: regexp.MustCompile(`^[A-Za-z0-9_-]{2,32}$`),
		url: "https://steamcommunity.com/id/"},
	{name: "x", label: "X", pattern: regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`),
		prefixes: []string{"@"}, url: "https://x.com/"},
}

func findPlatform(name string) (socialPlatform, int, bool) {
	for i, platform := range socialPlatforms {
		if platform.name == name {
			return platform, i, true
		}
	}
	return socialPlatform{}, 0, false
}

// normalizeHandle trims a name and drops what may be typed before it ("@",
// "u/"); ok is false for anything that is not a plain name on that site.
func (p socialPlatform) normalizeHandle(raw string) (string, bool) {
	handle := strings.TrimSpace(raw)
	for _, prefix := range p.prefixes {
		if trimmed, found := strings.CutPrefix(handle, prefix); found {
			handle = trimmed
			break
		}
	}
	if len(handle) > 253 || !p.pattern.MatchString(handle) {
		return "", false
	}
	return handle, true
}

func socialLink(platform socialPlatform, handle string) SocialLink {
	return SocialLink{Platform: platform.name, Handle: handle, URL: platform.url + handle}
}

// normalizeStatus trims a status line and checks it: one line of at most
// MaxStatusLength characters, no control characters.
func normalizeStatus(raw string) (string, error) {
	status := strings.TrimSpace(raw)
	if utf8.RuneCountInString(status) > MaxStatusLength {
		return "", invalidProfile("the status is at most %d characters", MaxStatusLength)
	}
	if !utf8.ValidString(status) || strings.ContainsFunc(status, unicode.IsControl) {
		return "", invalidProfile("the status is one line of text")
	}
	return status, nil
}

// normalizeLinks checks a profile's whole set of links: known sites, each at
// most once, a valid name on each. They come back in display order.
func normalizeLinks(inputs []SocialLinkInput) ([]SocialLink, error) {
	byPosition := make([]*SocialLink, len(socialPlatforms))
	for _, input := range inputs {
		platform, position, ok := findPlatform(input.Platform)
		if !ok {
			return nil, invalidProfile("unknown site %q", input.Platform)
		}
		if strings.TrimSpace(input.Handle) == "" {
			continue
		}
		if byPosition[position] != nil {
			return nil, invalidProfile("one %s link at most", platform.label)
		}
		handle, ok := platform.normalizeHandle(input.Handle)
		if !ok {
			return nil, invalidProfile("%q is not a %s name: type the name, not a link",
				strings.TrimSpace(input.Handle), platform.label)
		}
		link := socialLink(platform, handle)
		byPosition[position] = &link
	}
	links := []SocialLink{}
	for _, link := range byPosition {
		if link != nil {
			links = append(links, *link)
		}
	}
	return links, nil
}

// ReadSocialLinks reads a user's links in display order; a stored site no
// longer offered is left out.
func ReadSocialLinks(ctx context.Context, queries *db.Queries, userID string) ([]SocialLink, error) {
	rows, err := queries.ListSocialLinks(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list social links: %w", err)
	}
	byPosition := make([]*SocialLink, len(socialPlatforms))
	for _, row := range rows {
		platform, position, ok := findPlatform(row.Platform)
		if !ok {
			continue
		}
		link := socialLink(platform, row.Handle)
		byPosition[position] = &link
	}
	links := []SocialLink{}
	for _, link := range byPosition {
		if link != nil {
			links = append(links, *link)
		}
	}
	return links, nil
}

// ProfileUpdate is a change to the caller's own profile; nil leaves a part as
// it is. Links replace the whole set.
type ProfileUpdate struct {
	Status *string
	Links  *[]SocialLinkInput
}

// UpdateProfile changes the caller's status line and social links and
// returns the profile as it now reads.
func (s *Service) UpdateProfile(ctx context.Context, userID string, update ProfileUpdate) (PublicUser, error) {
	userID, err := parseID(userID)
	if err != nil {
		return PublicUser{}, err
	}
	var status string
	if update.Status != nil {
		if status, err = normalizeStatus(*update.Status); err != nil {
			return PublicUser{}, err
		}
	}
	var links []SocialLink
	if update.Links != nil {
		if links, err = normalizeLinks(*update.Links); err != nil {
			return PublicUser{}, err
		}
	}
	var username string
	err = s.inTransaction(ctx, func(queries *db.Queries) error {
		user, err := LockLiveUser(ctx, queries, userID)
		if err != nil {
			return err
		}
		username = user.Username
		if update.Status != nil {
			if err := queries.SetUserStatus(ctx, db.SetUserStatusParams{ID: user.ID, Status: status}); err != nil {
				return fmt.Errorf("set status: %w", err)
			}
		}
		if update.Links != nil {
			return replaceSocialLinks(ctx, queries, user.ID, links)
		}
		return nil
	})
	if err != nil {
		return PublicUser{}, err
	}
	return s.GetPublicUser(ctx, username)
}

func replaceSocialLinks(ctx context.Context, queries *db.Queries, userID string, links []SocialLink) error {
	if _, err := queries.DeleteSocialLinks(ctx, userID); err != nil {
		return fmt.Errorf("delete social links: %w", err)
	}
	for _, link := range links {
		if err := queries.InsertSocialLink(ctx, db.InsertSocialLinkParams{
			UserID: userID, Platform: link.Platform, Handle: link.Handle,
		}); err != nil {
			return fmt.Errorf("insert social link: %w", err)
		}
	}
	return nil
}

// ClearProfile empties a profile's status and links (staff, account deletion).
func ClearProfile(ctx context.Context, queries *db.Queries, userID string) error {
	if err := queries.SetUserStatus(ctx, db.SetUserStatusParams{ID: userID, Status: ""}); err != nil {
		return fmt.Errorf("clear status: %w", err)
	}
	if _, err := queries.DeleteSocialLinks(ctx, userID); err != nil {
		return fmt.Errorf("delete social links: %w", err)
	}
	return nil
}

// ProfileTag is one of the tags a profile's entries carry most.
type ProfileTag struct {
	Slug    string `json:"slug" doc:"The tag's slug."`
	Name    string `json:"name" doc:"The tag's display name."`
	Icon    string `json:"icon" doc:"The tag's game icon as a rich-text token; empty for none."`
	Entries int64  `json:"entries" doc:"How many of the user's entries carry it."`
}

// ProfileStats are a profile's numbers, over its listed entries only.
type ProfileStats struct {
	Entries     int64 `json:"entries" doc:"How many public entries the user has."`
	Views       int64 `json:"views" doc:"The page views of the user's public entries."`
	Favorites   int64 `json:"favorites" doc:"How many favorites the user's entries got."`
	Collections int64 `json:"collections" doc:"How many public collections the user has."`
	Comments    int64 `json:"comments" doc:"How many comments the user posted."`
	// TopTags are the tags its entries carry most, most used first.
	TopTags []ProfileTag `json:"top_tags" doc:"The tags the user's entries carry most, most used first."`
}

func readProfileStats(ctx context.Context, queries *db.Queries, userID string) (ProfileStats, error) {
	row, err := queries.GetProfileStats(ctx, userID)
	if err != nil {
		return ProfileStats{}, fmt.Errorf("read profile stats: %w", err)
	}
	stats := ProfileStats{Entries: row.Entries, Views: row.Views, Favorites: row.Favorites, TopTags: []ProfileTag{}}
	if stats.Collections, err = queries.CountUserPublicCollections(ctx, userID); err != nil {
		return ProfileStats{}, fmt.Errorf("count public collections: %w", err)
	}
	if stats.Comments, err = queries.CountUserComments(ctx, userID); err != nil {
		return ProfileStats{}, fmt.Errorf("count comments: %w", err)
	}
	tags, err := queries.ListProfileTopTags(ctx, db.ListProfileTopTagsParams{UserID: userID, RowLimit: profileTopTags})
	if err != nil {
		return ProfileStats{}, fmt.Errorf("list top tags: %w", err)
	}
	for _, tag := range tags {
		stats.TopTags = append(stats.TopTags, ProfileTag{Slug: tag.Slug, Name: tag.DisplayName, Icon: tag.Icon, Entries: tag.Entries})
	}
	return stats, nil
}

// ListPinnedEntries returns the cards of a user's pinned entries in their
// order: only those still listed.
func (s *Service) ListPinnedEntries(ctx context.Context, username, viewerUserID string) ([]EntrySummary, error) {
	user, err := s.findUser(ctx, username)
	if err != nil {
		return nil, err
	}
	ids, err := s.queries.ListPinnedEntryIDs(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list pinned entries: %w", err)
	}
	return s.entryCards(ctx, ids, viewerUserID)
}

// SetPins replaces the caller's pinned entries with entryIDs, in that order:
// pin, unpin and reorder in one. Each must be the caller's own listed entry.
func (s *Service) SetPins(ctx context.Context, userID string, entryIDs []string) error {
	userID, err := parseID(userID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(entryIDs))
	seen := make(map[string]bool, len(entryIDs))
	for _, entryID := range entryIDs {
		id, err := parseID(entryID)
		if err != nil {
			return ErrNotPinnable
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > MaxPinnedEntries {
		return ErrPinLimit
	}
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		if _, err := LockLiveUser(ctx, queries, userID); err != nil {
			return err
		}
		return writePins(ctx, queries, userID, ids)
	})
}

// PinEntry adds an entry at the end of the caller's pins (pinned already:
// nothing changes).
func (s *Service) PinEntry(ctx context.Context, userID, entryID string) error {
	return s.changePins(ctx, userID, entryID, func(pins []string, id string) ([]string, error) {
		if slices.Contains(pins, id) {
			return pins, nil
		}
		if len(pins) >= MaxPinnedEntries {
			return nil, ErrPinLimit
		}
		return append(pins, id), nil
	})
}

// UnpinEntry removes an entry from the caller's pins.
func (s *Service) UnpinEntry(ctx context.Context, userID, entryID string) error {
	return s.changePins(ctx, userID, entryID, func(pins []string, id string) ([]string, error) {
		kept := pins[:0]
		for _, pinned := range pins {
			if pinned != id {
				kept = append(kept, pinned)
			}
		}
		return kept, nil
	})
}

// changePins rewrites the caller's pins (those still listed, in order) with
// edit, under the user's row lock.
func (s *Service) changePins(
	ctx context.Context, userID, entryID string, edit func(pins []string, id string) ([]string, error),
) error {
	userID, err := parseID(userID)
	if err != nil {
		return err
	}
	id, err := parseID(entryID)
	if err != nil {
		return err
	}
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		if _, err := LockLiveUser(ctx, queries, userID); err != nil {
			return err
		}
		pins, err := queries.ListPinnedEntryIDs(ctx, userID)
		if err != nil {
			return fmt.Errorf("list pins: %w", err)
		}
		next, err := edit(pins, id)
		if err != nil {
			return err
		}
		return writePins(ctx, queries, userID, next)
	})
}

// writePins stores ids as the user's pins in order, dropping the rows of pins
// no longer listed. Every id must be the user's own listed entry.
func writePins(ctx context.Context, queries *db.Queries, userID string, ids []string) error {
	pinnable, err := queries.ListPinnableAmong(ctx, db.ListPinnableAmongParams{UserID: userID, Ids: ids})
	if err != nil {
		return fmt.Errorf("check pinnable entries: %w", err)
	}
	if len(pinnable) != len(ids) {
		return ErrNotPinnable
	}
	if _, err := queries.DeleteUserPins(ctx, userID); err != nil {
		return fmt.Errorf("delete pins: %w", err)
	}
	for position, id := range ids {
		if err := queries.InsertPin(ctx, db.InsertPinParams{
			UserID: userID, EntryID: id, Position: int32(position),
		}); err != nil {
			return fmt.Errorf("insert pin: %w", err)
		}
	}
	return nil
}

// pinnedEntryIDs is the set of ids among entryIDs the viewer pinned; empty for
// an anonymous viewer.
func pinnedEntryIDs(ctx context.Context, queries *db.Queries, viewerUserID string, entryIDs []string) (map[string]bool, error) {
	pinned := map[string]bool{}
	if viewerUserID == "" || len(entryIDs) == 0 {
		return pinned, nil
	}
	ids, err := queries.ListPinnedAmong(ctx, db.ListPinnedAmongParams{UserID: viewerUserID, Ids: entryIDs})
	if err != nil {
		return nil, fmt.Errorf("list pinned entries: %w", err)
	}
	for _, id := range ids {
		pinned[id] = true
	}
	return pinned, nil
}

// ProfileComment is one of a user's comments as their profile lists it.
type ProfileComment struct {
	EntryID    string     `json:"entry_id" doc:"The entry it is on."`
	EntryTitle string     `json:"entry_title" doc:"The entry's title."`
	Number     int32      `json:"number" doc:"Its number in the entry's thread."`
	Body       string     `json:"body" doc:"The comment text."`
	CreatedAt  time.Time  `json:"created_at" doc:"When it was posted."`
	EditedAt   *time.Time `json:"edited_at,omitempty" doc:"When it was last edited; absent if never."`
}

// ListUserComments returns one page of a user's comments on listed entries,
// newest first (removed ones left out), and their total.
func (s *Service) ListUserComments(
	ctx context.Context, username string, limit, offset int32,
) ([]ProfileComment, int64, error) {
	user, err := s.findUser(ctx, username)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.queries.ListUserComments(ctx, db.ListUserCommentsParams{
		AuthorUserID: user.ID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list user comments: %w", err)
	}
	total, err := s.queries.CountUserComments(ctx, user.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("count user comments: %w", err)
	}
	comments := make([]ProfileComment, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, ProfileComment{
			EntryID: row.EntryID, EntryTitle: row.EntryTitle, Number: row.Number, Body: row.Body,
			CreatedAt: row.CreatedAt.Time, EditedAt: optionalTime(row.EditedAt),
		})
	}
	return comments, total, nil
}

// LockLiveUser locks an account row for deletion or staff changes; ErrNotFound
// when it is missing or deleted.
func LockLiveUser(ctx context.Context, queries *db.Queries, userID string) (db.User, error) {
	user, err := queries.GetUserForStaffUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && user.DeletedAt.Valid) {
		return db.User{}, ErrNotFound
	}
	if err != nil {
		return db.User{}, fmt.Errorf("lock user: %w", err)
	}
	return user, nil
}
