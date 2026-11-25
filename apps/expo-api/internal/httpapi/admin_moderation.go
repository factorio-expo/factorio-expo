package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// Staff moderation: the report queues, staff removals and bans.

// staffWriteBodyLimit bounds a staff write's JSON body.
const staffWriteBodyLimit = 16 << 10

// staffReason is the body of a staff write that only takes a reason. The
// domain checks the reason (a delete needs one) and answers its own code.
type staffReason struct {
	Reason string `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

// CommentPath names a comment by its public id.
type CommentPath struct {
	CommentID string `path:"commentID" doc:"The comment's id."`
}

// EntryReportPath names an entry report by its id.
type EntryReportPath struct {
	ReportID string `path:"reportID" doc:"The entry report's id."`
}

// ReporterBanBody bans a report's reporter. The domain checks each field
// and answers its own code, so none is required here.
type ReporterBanBody struct {
	ReasonCode string `json:"reason_code,omitempty" doc:"Why: spam, abuse, illegal_content or other."`
	Days       int    `json:"days,omitempty" doc:"How long a signed-in reporter's account ban lasts, in days."`
	Reason     string `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

func (b ReporterBanBody) ban() staffops.ReporterBan {
	return staffops.ReporterBan{ReasonCode: b.ReasonCode, Days: b.Days, Reason: b.Reason}
}

type badgesResponse struct {
	// ReportedEntries and ReportedComments have open reports.
	ReportedEntries  int64 `json:"reported_entries"`
	ReportedComments int64 `json:"reported_comments"`
	// ProblemReports are open problem reports.
	ProblemReports int64 `json:"problem_reports"`
	// FailedJobs failed for good (preview, analysis and search).
	FailedJobs int64 `json:"failed_jobs"`
}

type badgesOutput struct {
	Body badgesResponse
}

// badges are the sidebar's counts, read on every console page.
func (s *adminServer) badges(ctx context.Context, _ *struct{}) (*badgesOutput, error) {
	counts, err := s.queries.StaffBadges(ctx)
	if err != nil {
		slog.Error("staff badges", "error", err)
		return nil, fault.New(fault.Internal, "internal", "could not load the counts")
	}
	return &badgesOutput{Body: badgesResponse{
		ReportedEntries: counts.ReportedEntries, ReportedComments: counts.ReportedComments,
		ProblemReports: counts.ProblemReports,
		FailedJobs:     counts.FailedJobs,
	}}, nil
}

type entryReportsResponse struct {
	Groups []staffops.EntryReportGroup `json:"groups"`
}

type entryReportsOutput struct {
	Body entryReportsResponse
}

func (s *adminServer) listEntryReports(ctx context.Context, _ *struct{}) (*entryReportsOutput, error) {
	groups, err := s.ops.ListEntryReports(ctx, staffUser(ctx))
	if err != nil {
		return nil, err
	}
	return &entryReportsOutput{Body: entryReportsResponse{Groups: groups}}, nil
}

type resolveEntryReportsInput struct {
	EntryPath
	Body staffReason
}

func (s *adminServer) resolveEntryReports(ctx context.Context, in *resolveEntryReportsInput) (*struct{}, error) {
	return nil, s.ops.ResolveEntryReports(ctx, staffUser(ctx), in.EntryID, in.Body.Reason)
}

type banEntryReporterInput struct {
	EntryReportPath
	Body ReporterBanBody
}

func (s *adminServer) banEntryReporter(ctx context.Context, in *banEntryReporterInput) (*struct{}, error) {
	return nil, s.ops.BanEntryReporter(ctx, staffUser(ctx), in.ReportID, in.Body.ban())
}

type deleteEntryInput struct {
	EntryPath
	Body staffReason
}

func (s *adminServer) deleteEntry(ctx context.Context, in *deleteEntryInput) (*struct{}, error) {
	return nil, s.ops.StaffDeleteEntry(ctx, staffUser(ctx), in.EntryID, in.Body.Reason)
}

type commentReportsResponse struct {
	Groups []staffops.CommentReportGroup `json:"groups"`
}

type commentReportsOutput struct {
	Body commentReportsResponse
}

func (s *adminServer) listCommentReports(ctx context.Context, _ *struct{}) (*commentReportsOutput, error) {
	groups, err := s.ops.ListCommentReports(ctx, staffUser(ctx))
	if err != nil {
		return nil, err
	}
	return &commentReportsOutput{Body: commentReportsResponse{Groups: groups}}, nil
}

type resolveCommentReportsInput struct {
	CommentPath
	Body staffReason
}

func (s *adminServer) resolveCommentReports(ctx context.Context, in *resolveCommentReportsInput) (*struct{}, error) {
	return nil, s.ops.ResolveCommentReports(ctx, staffUser(ctx), in.CommentID, in.Body.Reason)
}

type removeCommentInput struct {
	CommentPath
	Body staffReason
}

func (s *adminServer) removeComment(ctx context.Context, in *removeCommentInput) (*struct{}, error) {
	return nil, s.ops.StaffRemoveComment(ctx, staffUser(ctx), in.CommentID, in.Body.Reason)
}

// registerModeration registers this file's operations.
func (s *adminServer) registerModeration(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getBadges", Method: http.MethodGet, Path: "/admin/badges",
		Tags:    []string{"Overview"},
		Summary: "Sidebar counts",
	}), s.badges)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listEntryReports", Method: http.MethodGet, Path: "/admin/reports/entries",
		Tags:        []string{"Entry reports"},
		Summary:     "Open entry reports",
		Description: "The open entry report queue, grouped per entry, takedowns first.",
	}), s.listEntryReports)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "resolveEntryReports", Method: http.MethodPost, Path: "/admin/reports/entries/{entryID}/resolve",
		Tags:         []string{"Entry reports"},
		Summary:      "Resolve an entry's reports",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.resolveEntryReports)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "banEntryReporter", Method: http.MethodPost, Path: "/admin/entry-reports/{reportID}/ban-reporter",
		Tags:         []string{"Entry reports"},
		Summary:      "Ban an entry report's reporter",
		Description:  "Bans the reporter from reporting and resolves that report.",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.banEntryReporter)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteEntry", Method: http.MethodPost, Path: "/admin/entries/{entryID}/delete",
		Tags:         []string{"Entry reports"},
		Summary:      "Delete an entry as staff",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.deleteEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listCommentReports", Method: http.MethodGet, Path: "/admin/reports/comments",
		Tags:    []string{"Comment reports"},
		Summary: "Open comment reports",
	}), s.listCommentReports)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "resolveCommentReports", Method: http.MethodPost, Path: "/admin/reports/comments/{commentID}/resolve",
		Tags:         []string{"Comment reports"},
		Summary:      "Resolve a comment's reports",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.resolveCommentReports)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removeComment", Method: http.MethodPost, Path: "/admin/comments/{commentID}/remove",
		Tags:         []string{"Comment reports"},
		Summary:      "Remove a comment as staff",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.removeComment)
}
