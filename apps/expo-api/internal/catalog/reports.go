package catalog

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// MaxEntryReportNoteRunes bounds an entry report's note.
const MaxEntryReportNoteRunes = 1000

// entryReportReasons are the reasons an entry can be reported for. "stolen"
// (someone else's work posted without permission) needs a note pointing at
// the original.
var entryReportReasons = []string{"stolen", "offensive", "spam", "misleading", "other"}

// ErrInvalidEntryReport is returned for an unknown reason, a missing or
// oversized note, or a report of an entry the caller may edit.
var ErrInvalidEntryReport = fault.New(fault.Invalid, "invalid_entry_report", fmt.Sprintf(
	"reason must be stolen, offensive, spam, misleading or other; the note is required for stolen and at most %d characters; and the entry must not be your own",
	MaxEntryReportNoteRunes))

// EntryReport is one report of an entry.
type EntryReport struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
	// RevisionID is the revision the reporter was looking at; empty is the
	// live one.
	RevisionID string `json:"revision_id"`
}

// ReportEntry files a report of an entry the caller can see for the admins.
// Anyone may report, signed in or not: an anonymous report is keyed by the
// address hash. Its editors (owner, edit-token holder) and quick previews
// cannot be reported, and a reporter's second report while the first is open
// changes nothing. Reports never hide the entry by themselves.
func (s *Service) ReportEntry(
	ctx context.Context, entryID string, auth Authorization, ipHash []byte, report EntryReport,
) error {
	note := strings.TrimSpace(strings.ReplaceAll(report.Note, "\r\n", "\n"))
	if !slices.Contains(entryReportReasons, report.Reason) || !utf8.ValidString(note) ||
		utf8.RuneCountInString(note) > MaxEntryReportNoteRunes || (report.Reason == "stolen" && note == "") {
		return ErrInvalidEntryReport
	}
	if err := s.checkBan(ctx, Requester{UserID: auth.UserID, IPHash: ipHash}, BanReport); err != nil {
		return err
	}
	entry, err := s.entries().Viewable(ctx, entryID, auth.UserID)
	if err != nil {
		return err
	}
	if entry.QuickPreview {
		return ErrQuickPreview
	}
	if authorize(entry, auth) == nil {
		return ErrInvalidEntryReport
	}
	revision, err := viewedRevision(ctx, s.queries, entry, report.RevisionID)
	if err != nil {
		return err
	}
	params := db.CreateEntryReportParams{
		ID: shortid.New(), EntryID: entry.ID, Reason: report.Reason, Note: note,
		RevisionNumber: revision.VersionNumber,
		TitleSnapshot:  entry.Title, DescriptionSnapshot: entry.Description,
	}
	if auth.UserID != "" {
		params.ReporterUserID = pgtype.Text{String: auth.UserID, Valid: true}
	} else {
		params.ReporterIpHash = ipHash
	}
	if _, err := s.queries.CreateEntryReport(ctx, params); err != nil {
		return fmt.Errorf("create entry report: %w", err)
	}
	return nil
}
