package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// Problem reports (docs/admin.md#problem-reports): the site's bug button.
// Anyone may send one, signed in or not (an anonymous one keyed by the
// address hash, like entry reports), with an optional contact address kept
// only until the report is resolved. Staff review them in the console.

const (
	// MaxProblemReportRunes bounds a problem report's text.
	MaxProblemReportRunes = 4000
	// maxProblemPageRunes bounds the reported page's path.
	maxProblemPageRunes = 500
	// maxProblemContextKeys and maxProblemContextRunes bound the page context.
	maxProblemContextKeys  = 16
	maxProblemContextRunes = 200
	// maxUserAgentRunes is where a reporter's user agent is cut.
	maxUserAgentRunes = 500
	// maxEmailBytes is the longest address RFC 5321 allows.
	maxEmailBytes = 254
	// ProblemReportRetention is how long a resolved report is kept.
	ProblemReportRetention = 7 * 24 * time.Hour
	// ProblemEmailRetention is how long an open report keeps its contact
	// address.
	ProblemEmailRetention = 30 * 24 * time.Hour
)

// ProblemReportStatuses are how staff close a report.
var ProblemReportStatuses = []string{"fixed", "wont_fix", "duplicate", "spam"}

// ErrInvalidProblemReport is a problem report with an empty or oversized
// text, a malformed address, page, id or context.
var ErrInvalidProblemReport = fault.New(fault.Invalid, "invalid_problem_report", fmt.Sprintf(
	"describe the problem in at most %d characters; the email, if given, must be one address", MaxProblemReportRunes))

var contextKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ProblemReport is what the bug button sends.
type ProblemReport struct {
	Body string `json:"body"`
	// Email is an opt-in contact address for this report only.
	Email string `json:"email"`
	// Page is the page's path; a query or fragment is dropped.
	Page        string `json:"page"`
	EntryID     string `json:"entry_id"`
	BlueprintID string `json:"blueprint_id"`
	// Context is what the page knew (game version, phase states, viewport).
	Context map[string]string `json:"context"`
}

// ReportProblem stores a problem report. A report ban blocks it, like the
// other reports.
func (s *Service) ReportProblem(
	ctx context.Context, auth Authorization, ipHash []byte, userAgent string, report ProblemReport,
) error {
	params, err := problemReportParams(report)
	if err != nil {
		return err
	}
	if err := s.checkBan(ctx, Requester{UserID: auth.UserID, IPHash: ipHash}, BanReport); err != nil {
		return err
	}
	params.ID = shortid.New()
	params.UserAgent = cutRunes(strings.ToValidUTF8(userAgent, ""), maxUserAgentRunes)
	if auth.UserID != "" {
		params.ReporterUserID = pgText(auth.UserID)
	} else {
		params.ReporterIpHash = ipHash
	}
	if _, err := s.queries.CreateProblemReport(ctx, params); err != nil {
		return fmt.Errorf("create problem report: %w", err)
	}
	return nil
}

func problemReportParams(report ProblemReport) (db.CreateProblemReportParams, error) {
	body := strings.TrimSpace(strings.ReplaceAll(report.Body, "\r\n", "\n"))
	if body == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > MaxProblemReportRunes {
		return db.CreateProblemReportParams{}, ErrInvalidProblemReport
	}
	params := db.CreateProblemReportParams{Body: body}
	if email := strings.TrimSpace(report.Email); email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || address.Name != "" || len(email) > maxEmailBytes {
			return db.CreateProblemReportParams{}, ErrInvalidProblemReport
		}
		params.Email = pgText(email)
	}
	page, _, _ := strings.Cut(report.Page, "?")
	page, _, _ = strings.Cut(page, "#")
	if page != "" && (!strings.HasPrefix(page, "/") || !printable(page) ||
		utf8.RuneCountInString(page) > maxProblemPageRunes) {
		return db.CreateProblemReportParams{}, ErrInvalidProblemReport
	}
	params.PagePath = page
	for _, id := range []struct {
		value  string
		target *pgtype.Text
	}{{report.EntryID, &params.EntryID}, {report.BlueprintID, &params.BlueprintID}} {
		if id.value == "" {
			continue
		}
		if _, err := parseID(id.value); err != nil {
			return db.CreateProblemReportParams{}, ErrInvalidProblemReport
		}
		*id.target = pgText(id.value)
	}
	if len(report.Context) > maxProblemContextKeys {
		return db.CreateProblemReportParams{}, ErrInvalidProblemReport
	}
	context := map[string]string{}
	for key, value := range report.Context {
		value = strings.TrimSpace(value)
		if !contextKey.MatchString(key) || !printable(value) || utf8.RuneCountInString(value) > maxProblemContextRunes {
			return db.CreateProblemReportParams{}, ErrInvalidProblemReport
		}
		if value != "" {
			context[key] = value
		}
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return db.CreateProblemReportParams{}, fmt.Errorf("encode problem context: %w", err)
	}
	params.Context = encoded
	return params, nil
}

// printable: valid UTF-8 without control characters.
func printable(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// cutRunes keeps the first n runes of value.
func cutRunes(value string, n int) string {
	if utf8.RuneCountInString(value) <= n {
		return value
	}
	return string([]rune(value)[:n])
}

// PruneProblemReports deletes reports resolved more than
// ProblemReportRetention ago and drops the contact address of open ones
// sent more than ProblemEmailRetention ago; the API runs it hourly.
func (s *Service) PruneProblemReports(ctx context.Context) (deleted, cleared int64, err error) {
	now := time.Now()
	deleted, err = s.queries.DeleteResolvedProblemReports(ctx, pgtype.Timestamptz{
		Time: now.Add(-ProblemReportRetention), Valid: true,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("delete resolved problem reports: %w", err)
	}
	cleared, err = s.queries.ClearStaleProblemReportEmails(ctx, pgtype.Timestamptz{
		Time: now.Add(-ProblemEmailRetention), Valid: true,
	})
	if err != nil {
		return deleted, 0, fmt.Errorf("clear problem report emails: %w", err)
	}
	return deleted, cleared, nil
}
