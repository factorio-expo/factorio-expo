package staffops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The report queues and what staff do with a report (docs/admin.md,
// Moderation). Reports never hide anything by themselves; staff decide.

// reportQueueGroups is how many reported entries or comments one queue page
// shows, those reported longest ago first.
const reportQueueGroups = 50

// Reporter is who filed a report: whether it was anonymous, and the
// username of a signed-in reporter.
type Reporter struct {
	Anonymous bool   `json:"anonymous"`
	Username  string `json:"username,omitempty"`
}

// ReportedEntry is a reported entry as it is now.
type ReportedEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	// Owner is the owner's username; empty for an anonymous entry.
	Owner        string `json:"owner"`
	QuickPreview bool   `json:"quick_preview"`
	Deleted      bool   `json:"deleted"`
	// Gone is deleted or expired: the entry reads as missing to visitors.
	Gone            bool  `json:"gone"`
	CurrentRevision int32 `json:"current_revision"`
}

// StaffEntryReport is one open report of an entry, with the entry as it was
// reported.
type StaffEntryReport struct {
	ID                  string    `json:"id"`
	Reason              string    `json:"reason"`
	Note                string    `json:"note"`
	RevisionNumber      int32     `json:"revision_number"`
	TitleSnapshot       string    `json:"title_snapshot"`
	DescriptionSnapshot string    `json:"description_snapshot"`
	CreatedAt           time.Time `json:"created_at"`
	Reporter            Reporter  `json:"reporter"`
}

// EntryReportGroup is a reported entry and its open reports, oldest first.
type EntryReportGroup struct {
	Entry   ReportedEntry      `json:"entry"`
	Reports []StaffEntryReport `json:"reports"`
	// Takedown: one of them says the entry is someone else's work ("stolen"),
	// which needs a decision soon.
	Takedown bool `json:"takedown"`
}

// ListEntryReports is the entry report queue: open reports grouped per
// entry, takedowns first, then the entry reported longest ago.
func (s *Service) ListEntryReports(ctx context.Context, by audit.Staff) ([]EntryReportGroup, error) {
	rows, err := s.queries.ListOpenEntryReports(ctx, reportQueueGroups)
	if err != nil {
		return nil, fmt.Errorf("list entry reports: %w", err)
	}
	groups := []EntryReportGroup{}
	for _, row := range rows {
		if len(groups) == 0 || groups[len(groups)-1].Entry.ID != row.EntryID {
			groups = append(groups, EntryReportGroup{Entry: ReportedEntry{
				ID: row.EntryID, Title: row.Title, Description: row.Description, Visibility: row.Visibility,
				Owner: row.OwnerUsername, QuickPreview: row.QuickPreview, Deleted: row.Deleted, Gone: row.Gone,
				CurrentRevision: row.CurrentRevisionNumber,
			}})
		}
		reporter := Reporter{Anonymous: row.ReporterUsername == ""}
		reporter.Username = row.ReporterUsername
		group := &groups[len(groups)-1]
		group.Takedown = group.Takedown || row.Reason == "stolen"
		group.Reports = append(group.Reports, StaffEntryReport{
			ID: row.ID, Reason: row.Reason, Note: row.Note, RevisionNumber: row.RevisionNumber,
			TitleSnapshot: row.TitleSnapshot, DescriptionSnapshot: row.DescriptionSnapshot,
			CreatedAt: row.CreatedAt.Time, Reporter: reporter,
		})
	}
	return groups, nil
}

// ResolveEntryReports resolves every open report of an entry, with nothing
// else done to it. ErrNotFound when it has none open.
func (s *Service) ResolveEntryReports(ctx context.Context, by audit.Staff, entryID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if entryID, err = parseID(entryID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		resolved, err := queries.ResolveEntryReports(ctx, db.ResolveEntryReportsParams{
			EntryID: entryID, ResolvedBy: pgText(by.Username),
		})
		if err != nil {
			return fmt.Errorf("resolve entry reports: %w", err)
		}
		if resolved == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "entry_reports.resolve", TargetType: "entry", TargetID: entryID, Reason: reason,
			After: map[string]any{"resolved": resolved},
		})
	})
}

// StaffDeleteEntry soft-deletes an entry as staff, like its
// owner's delete: restorable, and removed from search. Its open reports are
// resolved with it.
func (s *Service) StaffDeleteEntry(ctx context.Context, by audit.Staff, entryID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if reason == "" {
		return invalidStaffRequest("a reason is required")
	}
	if entryID, err = parseID(entryID); err != nil {
		return err
	}
	return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
		entry, err := c.LockEntry(ctx, entryID)
		if err != nil {
			return err
		}
		if entry.DeletedAt.Valid {
			return catalog.ErrNotFound
		}
		if err := c.DeleteEntry(ctx, entry.ID); err != nil {
			return err
		}
		resolved, err := c.ResolveEntryReports(ctx, entry.ID, by.Username)
		if err != nil {
			return err
		}
		return audit.Write(ctx, c.Queries(), by, audit.Record{
			Action: "entry.delete", TargetType: "entry", TargetID: entry.ID, Reason: reason,
			Before: map[string]any{"deleted": false, "visibility": entry.Visibility},
			After:  map[string]any{"deleted": true, "reports_resolved": resolved},
		})
	})
}

// ReporterBan is a ban of the person behind one entry report, for report
// floods: an anonymous reporter's address is banned from reporting for the
// address ban duration (hours), a signed-in reporter's account for Days.
type ReporterBan struct {
	// ReasonCode is spam, abuse, illegal_content or other.
	ReasonCode string `json:"reason_code"`
	// Days is how long a signed-in reporter's account ban lasts.
	Days   int    `json:"days"`
	Reason string `json:"reason"`
}

// BanEntryReporter bans the reporter of an open entry report from reporting
// and resolves that report.
func (s *Service) BanEntryReporter(ctx context.Context, by audit.Staff, reportID string, request ReporterBan) error {
	reason, err := audit.CleanReason(request.Reason)
	if err != nil {
		return err
	}
	if reportID, err = parseID(reportID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		report, err := queries.GetOpenEntryReportForUpdate(ctx, reportID)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock entry report: %w", err)
		}
		after, err := s.banReporter(ctx, queries, by, report.ReporterUserID, report.ReporterIpHash, request, reason)
		if err != nil {
			return err
		}
		if err := queries.ResolveEntryReport(ctx, db.ResolveEntryReportParams{
			ID: report.ID, ResolvedBy: pgText(by.Username),
		}); err != nil {
			return fmt.Errorf("resolve entry report: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "entry_report.ban_reporter", TargetType: "entry_report", TargetID: report.ID,
			Reason: reason, After: after,
		})
	})
}

// banReporter bans the person behind a report from reporting: a signed-in
// reporter's account for request.Days, an anonymous reporter's address hash
// for the address ban duration. It answers the audit row's after fields.
func (s *Service) banReporter(
	ctx context.Context, queries *db.Queries, by audit.Staff, userID pgtype.Text, ipHash []byte,
	request ReporterBan, reason string,
) (map[string]any, error) {
	if !validBanReason(request.ReasonCode) {
		return nil, invalidStaffRequest("reason_code must be spam, abuse, illegal_content or other")
	}
	after := map[string]any{"actions": []string{string(catalog.BanReport)}, "reason_code": request.ReasonCode}
	if userID.Valid {
		expires, err := banEndInDays(request.Days)
		if err != nil {
			return nil, err
		}
		ban, err := queries.CreateUserBan(ctx, db.CreateUserBanParams{
			ID: newBanID(), UserID: userID.String, Actions: []string{string(catalog.BanReport)},
			ReasonCode: request.ReasonCode, Note: reason, BannedBy: by.Username,
			ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("ban reporter: %w", err)
		}
		after["ban"], after["expires_at"] = ban.ID, expires
		return after, nil
	}
	if ipHash == nil {
		return nil, invalidStaffRequest("the reporter is not known any more")
	}
	expires := time.Now().Add(s.catalog.IPBanDuration())
	if err := queries.BanIP(ctx, db.BanIPParams{
		IpHash: ipHash, ReasonCode: request.ReasonCode,
		Actions: []string{string(catalog.BanReport)}, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("ban reporter address: %w", err)
	}
	after["address"], after["expires_at"] = true, expires
	return after, nil
}

// ReportedComment is a reported comment as it is now.
type ReportedComment struct {
	ID         string `json:"id"`
	EntryID    string `json:"entry_id"`
	EntryTitle string `json:"entry_title"`
	Number     int32  `json:"number"`
	Author     string `json:"author"`
	// Body is empty once removed; RemovedBy then says by whom.
	Body      string    `json:"body"`
	RemovedBy string    `json:"removed_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// StaffCommentReport is one open report of a comment, with its body as
// reported.
type StaffCommentReport struct {
	Reason       string    `json:"reason"`
	Note         string    `json:"note"`
	BodySnapshot string    `json:"body_snapshot"`
	CreatedAt    time.Time `json:"created_at"`
	Reporter     Reporter  `json:"reporter"`
}

// CommentReportGroup is a reported comment and its open reports.
type CommentReportGroup struct {
	Comment ReportedComment      `json:"comment"`
	Reports []StaffCommentReport `json:"reports"`
}

// ListCommentReports is the comment report queue, grouped per comment, the
// comment reported longest ago first.
func (s *Service) ListCommentReports(ctx context.Context, by audit.Staff) ([]CommentReportGroup, error) {
	rows, err := s.queries.ListOpenCommentReports(ctx, reportQueueGroups)
	if err != nil {
		return nil, fmt.Errorf("list comment reports: %w", err)
	}
	groups := []CommentReportGroup{}
	for _, row := range rows {
		if len(groups) == 0 || groups[len(groups)-1].Comment.ID != row.CommentID {
			groups = append(groups, CommentReportGroup{Comment: ReportedComment{
				ID: row.CommentID, EntryID: row.EntryID, EntryTitle: row.EntryTitle, Number: row.Number,
				Author: row.AuthorUsername, Body: row.Body, RemovedBy: row.DeletedBy.String,
				CreatedAt: row.CommentCreatedAt.Time,
			}})
		}
		reporter := Reporter{}
		reporter.Username = row.ReporterUsername
		group := &groups[len(groups)-1]
		group.Reports = append(group.Reports, StaffCommentReport{
			Reason: row.Reason, Note: row.Note, BodySnapshot: row.BodySnapshot,
			CreatedAt: row.CreatedAt.Time, Reporter: reporter,
		})
	}
	return groups, nil
}

// ResolveCommentReports resolves every open report of a comment and leaves
// the comment as it is. ErrNotFound when it has none open.
func (s *Service) ResolveCommentReports(ctx context.Context, by audit.Staff, commentID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if commentID, err = parseID(commentID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		resolved, err := queries.ResolveCommentReports(ctx, db.ResolveCommentReportsParams{
			CommentID: commentID, ResolvedBy: pgText(by.Username),
		})
		if err != nil {
			return fmt.Errorf("resolve comment reports: %w", err)
		}
		if resolved == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "comment_reports.resolve", TargetType: "comment", TargetID: commentID, Reason: reason,
			After: map[string]any{"resolved": resolved},
		})
	})
}

// StaffRemoveComment leaves a "removed by staff" tombstone of a comment
// and resolves its open reports. The body is not kept, not
// even in the audit log.
func (s *Service) StaffRemoveComment(ctx context.Context, by audit.Staff, commentID, reason string) error {
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	if commentID, err = parseID(commentID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		comment, err := queries.GetCommentByIDForUpdate(ctx, commentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock comment: %w", err)
		}
		if comment.DeletedAt.Valid {
			return catalog.ErrNotFound
		}
		if _, err := queries.RemoveComment(ctx, db.RemoveCommentParams{
			ID: comment.ID, DeletedBy: pgText("admin"),
		}); err != nil {
			return fmt.Errorf("remove comment: %w", err)
		}
		resolved, err := queries.ResolveCommentReports(ctx, db.ResolveCommentReportsParams{
			CommentID: comment.ID, ResolvedBy: pgText(by.Username),
		})
		if err != nil {
			return fmt.Errorf("resolve comment reports: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "comment.remove", TargetType: "comment", TargetID: comment.ID, Reason: reason,
			Before: map[string]any{"entry_id": comment.EntryID, "number": comment.Number, "removed": false},
			After:  map[string]any{"removed": true, "reports_resolved": resolved},
		})
	})
}
