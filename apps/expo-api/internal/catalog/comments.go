package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// Comment bounds and daily caps (on top of the HTTP rate limits).
const (
	MaxCommentRunes      = 4000
	MaxCommentLines      = 60
	MaxReportNoteRunes   = 500
	DefaultCommentPage   = 50
	MaxCommentPage       = 100
	commentsPerDay       = 50
	commentsPerDayNew    = 5
	commentsPerEntryDay  = 20
	newAccountCommentAge = 24 * time.Hour
)

var (
	// ErrCommentsClosed is returned when an entry takes no comments: it is
	// not public, or it is a quick preview.
	ErrCommentsClosed = fault.New(fault.Conflict, "comments_closed", "comments are only open on public entries")
	// ErrInvalidComment is returned for an empty or oversized comment body.
	ErrInvalidComment = fault.New(fault.Invalid, "invalid_comment", fmt.Sprintf(
		"a comment must be 1-%d characters and at most %d lines", MaxCommentRunes, MaxCommentLines))
	// ErrDuplicateComment is returned when the author posted the same text
	// in the last ten minutes.
	ErrDuplicateComment = fault.New(fault.Conflict, "duplicate_comment", "you already posted this comment")
	// ErrCommentLimit is returned when the author used up a daily comment cap.
	ErrCommentLimit = fault.New(fault.TooMany, "comment_limit", "daily comment limit reached, try again tomorrow")
	// ErrInvalidReport is returned for an unknown reason, an oversized note,
	// or a report of one's own comment.
	ErrInvalidReport = fault.New(fault.Invalid, "invalid_report", fmt.Sprintf(
		"reason must be spam, abuse, off_topic or other, the note at most %d characters, and the comment someone else's",
		MaxReportNoteRunes))
)

// Comment is one entry comment. A removed comment keeps its number and
// author but no body.
type Comment struct {
	Number int32  `json:"number" doc:"The comment's number in the entry's thread (never reused)."`
	Author string `json:"author" doc:"The commenter; absent once removed."`
	// RevisionNumber is the entry's live revision when it was posted.
	RevisionNumber int32      `json:"revision_number" doc:"The entry's live revision when it was posted."`
	Body           string     `json:"body" doc:"The comment text; empty once removed."`
	CreatedAt      time.Time  `json:"created_at" doc:"When it was posted."`
	EditedAt       *time.Time `json:"edited_at,omitempty" doc:"When it was last edited; absent if never."`
	// RemovedBy is author, owner or admin once removed; empty otherwise.
	RemovedBy string `json:"removed_by,omitempty" doc:"author, owner or admin once removed; empty otherwise."`
	// CanEdit and CanRemove are the viewer's rights on this comment.
	CanEdit   bool `json:"can_edit" doc:"Whether the viewer may edit it."`
	CanRemove bool `json:"can_remove" doc:"Whether the viewer may remove it."`
}

// CommentPage is one page of an entry's thread plus what the viewer may do.
type CommentPage struct {
	Comments []Comment `json:"comments" doc:"The comments of this page."`
	// Total counts every comment, removed ones included.
	Total int64 `json:"total" doc:"Every comment in the thread, removed ones included."`
	// FirstNumber and LastNumber bound the thread (0 when empty), so a #N
	// reference off this page can still be linked.
	FirstNumber int32         `json:"first_number" doc:"The lowest number in the thread (0 when empty)."`
	LastNumber  int32         `json:"last_number" doc:"The highest number in the thread (0 when empty)."`
	HasOlder    bool          `json:"has_older" doc:"Whether older comments exist."`
	HasNewer    bool          `json:"has_newer" doc:"Whether newer comments exist."`
	Viewer      CommentViewer `json:"viewer" doc:"What the viewer may do in the thread."`
}

// CommentViewer says whether the viewer may post, and why not.
type CommentViewer struct {
	CanComment bool `json:"can_comment" doc:"Whether the viewer may post."`
	// Blocked is closed (entry not public), signed_out or banned when
	// CanComment is false.
	Blocked string `json:"blocked,omitempty" doc:"Why the viewer cannot comment: closed (entry not public), signed_out or banned."`
	Ban     *Ban   `json:"ban,omitempty" doc:"The viewer's active ban, if it blocks commenting."`
	// CanModerate is whether the viewer may remove anyone's comment: the
	// entry owner, or the edit-token holder of an anonymous entry.
	CanModerate bool `json:"can_moderate" doc:"Whether the viewer may remove anyone's comment."`
}

// CommentPageRequest picks a page: after a number (oldest first, the
// default) or before one; Limit is clamped to MaxCommentPage.
type CommentPageRequest struct {
	After  int32
	Before int32
	Limit  int32
}

// CommentReport is one user's report of a comment.
type CommentReport struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

var reportReasons = []string{"spam", "abuse", "off_topic", "other"}

// ListComments returns one page of an entry's thread. An entry that takes no
// comments answers an empty, closed thread.
func (s *Service) ListComments(
	ctx context.Context, entryID string, auth Authorization, request CommentPageRequest,
) (CommentPage, error) {
	entry, err := s.entries().Viewable(ctx, entryID, auth.UserID)
	if err != nil {
		return CommentPage{}, err
	}
	page := CommentPage{Comments: []Comment{}}
	// Only listed entries take comments.
	if !entryListed(entry, time.Now()) {
		page.Viewer.Blocked = "closed"
		return page, nil
	}
	moderator := authorize(entry, auth) == nil
	page.Viewer.CanModerate = moderator
	// Only the account's ban: reading a thread never looks up the address.
	switch ban, _, err := s.accountBan(ctx, auth.UserID, BanComment); {
	case err != nil:
		return CommentPage{}, err
	case auth.UserID == "":
		page.Viewer.Blocked = "signed_out"
	case ban != nil:
		page.Viewer.Blocked, page.Viewer.Ban = "banned", ban
	default:
		page.Viewer.CanComment = true
	}

	bounds, err := s.queries.GetCommentBounds(ctx, entry.ID)
	if err != nil {
		return CommentPage{}, fmt.Errorf("read comment bounds: %w", err)
	}
	page.Total, page.FirstNumber, page.LastNumber = bounds.Total, bounds.FirstNumber, bounds.LastNumber

	limit := request.Limit
	if limit <= 0 {
		limit = DefaultCommentPage
	}
	limit = min(limit, MaxCommentPage)
	var rows []db.ListCommentsAfterRow
	if request.Before > 0 {
		before, err := s.queries.ListCommentsBefore(ctx, db.ListCommentsBeforeParams{
			EntryID: entry.ID, Before: request.Before, Limit: limit,
		})
		if err != nil {
			return CommentPage{}, fmt.Errorf("list comments: %w", err)
		}
		for _, row := range slices.Backward(before) {
			rows = append(rows, db.ListCommentsAfterRow(row))
		}
	} else {
		rows, err = s.queries.ListCommentsAfter(ctx, db.ListCommentsAfterParams{
			EntryID: entry.ID, After: max(request.After, 0), Limit: limit,
		})
		if err != nil {
			return CommentPage{}, fmt.Errorf("list comments: %w", err)
		}
	}
	for _, row := range rows {
		page.Comments = append(page.Comments,
			commentView(db.Comment{
				ID: row.ID, EntryID: row.EntryID, Number: row.Number, AuthorUserID: row.AuthorUserID,
				RevisionNumber: row.RevisionNumber, Body: row.Body, CreatedAt: row.CreatedAt,
				EditedAt: row.EditedAt, DeletedAt: row.DeletedAt, DeletedBy: row.DeletedBy,
			}, row.AuthorUsername, auth.UserID, moderator, page.Viewer.Ban != nil))
	}
	if len(rows) > 0 {
		page.HasOlder = rows[0].Number > bounds.FirstNumber
		page.HasNewer = rows[len(rows)-1].Number < bounds.LastNumber
	} else if bounds.Total > 0 {
		// Past either end: offer the way back.
		page.HasOlder = request.Before == 0
		page.HasNewer = request.Before > 0
	}
	return page, nil
}

// commentView is the public shape of a row for one viewer. A banned author
// may still remove their comments but no longer edit them.
func commentView(row db.Comment, author, viewerID string, moderator, banned bool) Comment {
	comment := Comment{
		Number: row.Number, Author: author, RevisionNumber: row.RevisionNumber,
		Body: row.Body, CreatedAt: row.CreatedAt.Time, RemovedBy: row.DeletedBy.String,
	}
	if row.EditedAt.Valid {
		edited := row.EditedAt.Time
		comment.EditedAt = &edited
	}
	if !row.DeletedAt.Valid {
		own := viewerID != "" && viewerID == row.AuthorUserID
		comment.CanEdit = own && !banned
		comment.CanRemove = own || moderator
	}
	return comment
}

// NewComment is a comment to post. RevisionID is the revision the author was
// looking at (any of the entry's); empty means the live one.
type NewComment struct {
	Body       string `json:"body"`
	RevisionID string `json:"revision_id,omitempty"`
}

// CreateComment posts a comment as the signed-in caller.
func (s *Service) CreateComment(
	ctx context.Context, entryID string, auth Authorization, ipHash []byte, input NewComment,
) (Comment, error) {
	author, err := parseID(auth.UserID)
	if auth.UserID == "" || err != nil {
		return Comment{}, ErrForbidden
	}
	body, err := normalizedCommentBody(input.Body)
	if err != nil {
		return Comment{}, err
	}
	if err := s.checkBan(ctx, Requester{UserID: author, IPHash: ipHash}, BanComment); err != nil {
		return Comment{}, err
	}
	var comment Comment
	err = s.inTransaction(ctx, func(queries *db.Queries) error {
		entry, err := (entryAccess{queries: queries}).Commentable(ctx, entryID, author)
		if err != nil {
			return err
		}
		user, err := queries.GetUserByID(ctx, author)
		if err != nil {
			return notFoundOr(err, "read comment author")
		}
		if err := commentCaps(ctx, queries, user, entry.ID, body); err != nil {
			return err
		}
		revision, err := viewedRevision(ctx, queries, entry, input.RevisionID)
		if err != nil {
			return err
		}
		number, err := queries.NextCommentNumber(ctx, entry.ID)
		if err != nil {
			return fmt.Errorf("next comment number: %w", err)
		}
		row, err := queries.CreateComment(ctx, db.CreateCommentParams{
			ID: shortid.New(), EntryID: entry.ID, Number: number, AuthorUserID: author,
			RevisionNumber: revision.VersionNumber, Body: body,
		})
		if err != nil {
			return fmt.Errorf("create comment: %w", err)
		}
		if _, err := recordXP(ctx, queries, xpCommented{authorID: author, commentID: row.ID}); err != nil {
			return err
		}
		comment = commentView(row, user.Username, author, authorize(entry, auth) == nil, false)
		return nil
	})
	return comment, err
}

// viewedRevision is the revision a new comment or report refers to: the one named (it
// must belong to the entry), or the live one.
func viewedRevision(ctx context.Context, queries *db.Queries, entry db.Entry, revisionID string) (db.Revision, error) {
	id := entry.CurrentRevisionID.String
	if revisionID != "" {
		var err error
		if id, err = parseID(revisionID); err != nil {
			return db.Revision{}, ErrNotFound
		}
	}
	revision, err := queries.GetRevision(ctx, id)
	if err != nil {
		return db.Revision{}, notFoundOr(err, "read viewed revision")
	}
	if revision.EntryID != entry.ID {
		return db.Revision{}, ErrNotFound
	}
	return revision, nil
}

// commentCaps enforces the daily caps (lower for accounts younger than a
// day) and refuses a repeat of a recent comment.
func commentCaps(ctx context.Context, queries *db.Queries, user db.User, entryID, body string) error {
	activity, err := queries.GetUserCommentActivity(ctx, db.GetUserCommentActivityParams{
		AuthorUserID: user.ID, EntryID: entryID, Body: body,
	})
	if err != nil {
		return fmt.Errorf("read comment activity: %w", err)
	}
	if activity.Duplicate {
		return ErrDuplicateComment
	}
	daily := int64(commentsPerDay)
	if time.Since(user.CreatedAt.Time) < newAccountCommentAge {
		daily = commentsPerDayNew
	}
	if activity.DayTotal >= daily || activity.DayOnEntry >= commentsPerEntryDay {
		return ErrCommentLimit
	}
	return nil
}

// EditComment replaces the body of the caller's own comment; it is marked
// edited. Edits are not limited in time.
func (s *Service) EditComment(
	ctx context.Context, entryID string, number int32, auth Authorization, ipHash []byte, body string,
) (Comment, error) {
	if auth.UserID == "" {
		return Comment{}, ErrForbidden
	}
	body, err := normalizedCommentBody(body)
	if err != nil {
		return Comment{}, err
	}
	if err := s.checkBan(ctx, Requester{UserID: auth.UserID, IPHash: ipHash}, BanComment); err != nil {
		return Comment{}, err
	}
	var comment Comment
	err = s.inTransaction(ctx, func(queries *db.Queries) error {
		entry, row, err := lockComment(ctx, queries, entryID, number, auth.UserID)
		if err != nil {
			return err
		}
		if row.AuthorUserID != auth.UserID {
			return ErrForbidden
		}
		if row.DeletedAt.Valid {
			return ErrNotFound
		}
		if row.Body != body {
			if row, err = queries.UpdateCommentBody(ctx, db.UpdateCommentBodyParams{ID: row.ID, Body: body}); err != nil {
				return fmt.Errorf("update comment: %w", err)
			}
		}
		user, err := queries.GetUserByID(ctx, auth.UserID)
		if err != nil {
			return notFoundOr(err, "read comment author")
		}
		comment = commentView(row, user.Username, auth.UserID, authorize(entry, auth) == nil, false)
		return nil
	})
	return comment, err
}

// RemoveComment leaves a tombstone of a comment. Its author or the entry's
// moderator (owner, or edit-token holder of an anonymous entry) may remove
// it; removing a removed comment is a no-op.
func (s *Service) RemoveComment(ctx context.Context, entryID string, number int32, auth Authorization) error {
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		entry, row, err := lockComment(ctx, queries, entryID, number, auth.UserID)
		if err != nil {
			return err
		}
		var by string
		switch {
		case auth.UserID != "" && row.AuthorUserID == auth.UserID:
			by = "author"
		case authorize(entry, auth) == nil:
			by = "owner"
		default:
			return ErrForbidden
		}
		if _, err := queries.RemoveComment(ctx, db.RemoveCommentParams{
			ID: row.ID, DeletedBy: pgtype.Text{String: by, Valid: true},
		}); err != nil {
			return fmt.Errorf("remove comment: %w", err)
		}
		return nil
	})
}

// ReportComment files the caller's report of someone else's live comment for
// the admins. Reporting the same comment again changes nothing.
func (s *Service) ReportComment(
	ctx context.Context, entryID string, number int32, auth Authorization, ipHash []byte, report CommentReport,
) error {
	if auth.UserID == "" {
		return ErrForbidden
	}
	note := strings.TrimSpace(report.Note)
	if !slices.Contains(reportReasons, report.Reason) || utf8.RuneCountInString(note) > MaxReportNoteRunes {
		return ErrInvalidReport
	}
	if err := s.checkBan(ctx, Requester{UserID: auth.UserID, IPHash: ipHash}, BanReport); err != nil {
		return err
	}
	return s.inTransaction(ctx, func(queries *db.Queries) error {
		_, row, err := lockComment(ctx, queries, entryID, number, auth.UserID)
		if err != nil {
			return err
		}
		if row.DeletedAt.Valid {
			return ErrNotFound
		}
		if row.AuthorUserID == auth.UserID {
			return ErrInvalidReport
		}
		if _, err := queries.CreateCommentReport(ctx, db.CreateCommentReportParams{
			CommentID: row.ID, ReporterUserID: auth.UserID, Reason: report.Reason,
			Note: note, BodySnapshot: row.Body,
		}); err != nil {
			return fmt.Errorf("create comment report: %w", err)
		}
		return nil
	})
}

// lockComment locks a commentable entry and then one of its comments.
func lockComment(
	ctx context.Context, queries *db.Queries, entryID string, number int32, viewerID string,
) (db.Entry, db.Comment, error) {
	entry, err := (entryAccess{queries: queries}).Commentable(ctx, entryID, viewerID)
	if err != nil {
		return db.Entry{}, db.Comment{}, err
	}
	row, err := queries.GetCommentForUpdate(ctx, db.GetCommentForUpdateParams{EntryID: entry.ID, Number: number})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Entry{}, db.Comment{}, ErrNotFound
	}
	if err != nil {
		return db.Entry{}, db.Comment{}, fmt.Errorf("read comment: %w", err)
	}
	return entry, row, nil
}

// normalizedCommentBody trims the body and normalizes line endings, then
// checks its bounds.
func normalizedCommentBody(body string) (string, error) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > MaxCommentRunes ||
		strings.Count(body, "\n") >= MaxCommentLines {
		return "", ErrInvalidComment
	}
	return body, nil
}

// purgeComments deletes an entry's thread once it stops taking comments (is
// no longer listed); entry is the row after the change.
func purgeComments(ctx context.Context, queries *db.Queries, entry db.Entry) error {
	if entryListed(entry, time.Now()) {
		return nil
	}
	if _, err := queries.DeleteEntryComments(ctx, entry.ID); err != nil {
		return fmt.Errorf("delete entry comments: %w", err)
	}
	return nil
}
