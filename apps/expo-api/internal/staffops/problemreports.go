package staffops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// StaffProblemReport is a problem report as staff see it.
type StaffProblemReport struct {
	ID          string            `json:"id"`
	Body        string            `json:"body"`
	Email       string            `json:"email"`
	Page        string            `json:"page"`
	EntryID     string            `json:"entry_id"`
	BlueprintID string            `json:"blueprint_id"`
	Context     map[string]string `json:"context"`
	UserAgent   string            `json:"user_agent"`
	CreatedAt   time.Time         `json:"created_at"`
	Status      string            `json:"status"`
	StaffNote   string            `json:"staff_note"`
	ResolvedAt  *time.Time        `json:"resolved_at"`
	ResolvedBy  string            `json:"resolved_by"`
	Reporter    Reporter          `json:"reporter"`
}

// ProblemReportPage is the console's problem report list.
type ProblemReportPage struct {
	Reports []StaffProblemReport `json:"reports"`
	Counts  struct {
		Open     int64 `json:"open"`
		Resolved int64 `json:"resolved"`
	} `json:"counts"`
}

// ListProblemReports lists the open reports (oldest first) or the resolved
// ones still kept (latest first).
func (s *Service) ListProblemReports(ctx context.Context, by audit.Staff, open bool) (ProblemReportPage, error) {
	rows, err := s.queries.ListProblemReports(ctx, db.ListProblemReportsParams{
		Open: open, RowLimit: problemReportListLimit,
	})
	if err != nil {
		return ProblemReportPage{}, fmt.Errorf("list problem reports: %w", err)
	}
	counts, err := s.queries.CountProblemReports(ctx)
	if err != nil {
		return ProblemReportPage{}, fmt.Errorf("count problem reports: %w", err)
	}
	page := ProblemReportPage{Reports: make([]StaffProblemReport, 0, len(rows))}
	page.Counts.Open, page.Counts.Resolved = counts.Open, counts.Resolved
	for _, row := range rows {
		context := map[string]string{}
		if err := json.Unmarshal(row.Context, &context); err != nil {
			return ProblemReportPage{}, fmt.Errorf("decode problem context of %s: %w", row.ID, err)
		}
		page.Reports = append(page.Reports, StaffProblemReport{
			ID: row.ID, Body: row.Body, Email: row.Email.String, Page: row.PagePath,
			EntryID: row.EntryID.String, BlueprintID: row.BlueprintID.String, Context: context,
			UserAgent: row.UserAgent, CreatedAt: row.CreatedAt.Time, Status: row.Status, StaffNote: row.StaffNote,
			ResolvedAt: optionalTime(row.ResolvedAt), ResolvedBy: row.ResolvedBy.String,
			Reporter: Reporter{Anonymous: row.ReporterUsername == "", Username: row.ReporterUsername},
		})
	}
	return page, nil
}

// ResolveProblemReport closes an open report with a status (fixed,
// wont_fix, duplicate, spam) and a staff note; its contact address and
// address hash are dropped.
func (s *Service) ResolveProblemReport(ctx context.Context, by audit.Staff, reportID, status, note string) error {
	if !slices.Contains(catalog.ProblemReportStatuses, status) {
		return invalidStaffRequest("status must be fixed, wont_fix, duplicate or spam")
	}
	note, err := cleanStaffNote(note)
	if err != nil {
		return err
	}
	if reportID, err = parseID(reportID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		report, err := openProblemReport(ctx, queries, reportID)
		if err != nil {
			return err
		}
		if err := queries.ResolveProblemReport(ctx, db.ResolveProblemReportParams{
			ID: report.ID, Status: status, StaffNote: note, ResolvedBy: pgText(by.Username),
		}); err != nil {
			return fmt.Errorf("resolve problem report: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "problem_report.resolve", TargetType: "problem_report", TargetID: report.ID,
			After: map[string]any{"status": status},
		})
	})
}

// SetProblemReportNote changes a report's staff note, open or resolved.
func (s *Service) SetProblemReportNote(ctx context.Context, by audit.Staff, reportID, note string) error {
	note, err := cleanStaffNote(note)
	if err != nil {
		return err
	}
	if reportID, err = parseID(reportID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		changed, err := queries.SetProblemReportNote(ctx, db.SetProblemReportNoteParams{ID: reportID, StaffNote: note})
		if err != nil {
			return fmt.Errorf("set problem report note: %w", err)
		}
		if changed == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "problem_report.note", TargetType: "problem_report", TargetID: reportID,
		})
	})
}

// BanProblemReporter bans an open report's reporter from reporting (as
// BanEntryReporter) and closes the report as spam.
func (s *Service) BanProblemReporter(ctx context.Context, by audit.Staff, reportID string, request ReporterBan) error {
	reason, err := audit.CleanReason(request.Reason)
	if err != nil {
		return err
	}
	if reportID, err = parseID(reportID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		report, err := openProblemReport(ctx, queries, reportID)
		if err != nil {
			return err
		}
		after, err := s.banReporter(ctx, queries, by, report.ReporterUserID, report.ReporterIpHash, request, reason)
		if err != nil {
			return err
		}
		if err := queries.ResolveProblemReport(ctx, db.ResolveProblemReportParams{
			ID: report.ID, Status: "spam", StaffNote: report.StaffNote, ResolvedBy: pgText(by.Username),
		}); err != nil {
			return fmt.Errorf("resolve problem report: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "problem_report.ban_reporter", TargetType: "problem_report", TargetID: report.ID,
			Reason: reason, After: after,
		})
	})
}

func openProblemReport(ctx context.Context, queries *db.Queries, reportID string) (db.ProblemReport, error) {
	report, err := queries.GetProblemReportForUpdate(ctx, reportID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && report.ResolvedAt.Valid) {
		return db.ProblemReport{}, catalog.ErrNotFound
	}
	if err != nil {
		return db.ProblemReport{}, fmt.Errorf("lock problem report: %w", err)
	}
	return report, nil
}

// maxStaffNoteRunes bounds a problem report's staff note.
const maxStaffNoteRunes = 1000

func cleanStaffNote(note string) (string, error) {
	note = strings.TrimSpace(strings.ReplaceAll(note, "\r\n", "\n"))
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > maxStaffNoteRunes {
		return "", invalidStaffRequest("the note is at most %d characters", maxStaffNoteRunes)
	}
	return note, nil
}
