package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// reportEntryBody is an entry report and, when anonymous, its human check.
type reportEntryBody struct {
	Reason     string `json:"reason,omitempty" enum:"stolen,offensive,spam,misleading,other" doc:"Why the entry is reported. A stolen report needs a note pointing at the original."`
	Note       string `json:"note,omitempty" doc:"What the admins should look at; required for a stolen report, at most 1000 characters."`
	RevisionID string `json:"revision_id,omitempty" doc:"The revision the reporter was looking at; empty is the live one."`
	// TurnstileToken passes the human check; anonymous callers only.
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The Cloudflare Turnstile token; required from anonymous callers when the check is on."`
}

type reportEntryInput struct {
	EntryPath
	EditTokenInput
	Body reportEntryBody
}

// reportProblemBody is a problem report and, when anonymous, its human check.
type reportProblemBody struct {
	Body        string            `json:"body,omitempty" doc:"What went wrong, at most 4000 characters; required."`
	Email       string            `json:"email,omitempty" doc:"An opt-in contact address for this report only."`
	Page        string            `json:"page,omitempty" doc:"The page's path; a query or fragment is dropped."`
	EntryID     string            `json:"entry_id,omitempty" doc:"The entry the page showed, if any."`
	BlueprintID string            `json:"blueprint_id,omitempty" doc:"The blueprint the page showed, if any."`
	Context     map[string]string `json:"context,omitempty" doc:"What the page knew: game version, phase states, viewport."`
	// TurnstileToken passes the human check; anonymous callers only.
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The Cloudflare Turnstile token; required from anonymous callers when the check is on."`
}

type reportProblemInput struct {
	UserAgent string `header:"User-Agent" doc:"The reporter's user agent, stored with the report."`
	Body      reportProblemBody
}

// reportProblem stores a problem report from the site's bug button, signed
// in or not; the user agent comes from the request.
func (s *server) reportProblem(ctx context.Context, in *reportProblemInput) (*struct{}, error) {
	return nil, s.catalog.ReportProblem(ctx, catalog.Authorization{UserID: viewerFrom(ctx)},
		s.ipHash(requestFrom(ctx)), in.UserAgent, catalog.ProblemReport{
			Body: in.Body.Body, Email: in.Body.Email, Page: in.Body.Page,
			EntryID: in.Body.EntryID, BlueprintID: in.Body.BlueprintID, Context: in.Body.Context,
		})
}

// reportEntry files a report of an entry for the admins, signed in or not.
func (s *server) reportEntry(ctx context.Context, in *reportEntryInput) (*struct{}, error) {
	return nil, s.catalog.ReportEntry(ctx, in.EntryID,
		catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()},
		s.ipHash(requestFrom(ctx)), catalog.EntryReport{
			Reason: in.Body.Reason, Note: in.Body.Note, RevisionID: in.Body.RevisionID,
		})
}

func (s *server) registerReports(api huma.API) {
	registerHumanChecked(api, s, s.operation(huma.Operation{
		OperationID: "reportEntry", Method: http.MethodPost, Path: "/entries/{entryID}/report",
		Tags:         []string{"Reports"},
		Summary:      "Report an entry",
		Description:  "Anyone who can see an entry may report it, signed in or not. Anonymous reports pass the human check. Reports never hide the entry by themselves.",
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		MaxBodyBytes: 16 << 10,
	}, accessOptional, limitReport), turnstileActionReport, s.reportEntry)
	registerHumanChecked(api, s, s.operation(huma.Operation{
		OperationID: "reportProblem", Method: http.MethodPost, Path: "/problem-reports",
		Tags:         []string{"Reports"},
		Summary:      "Report a problem with the site",
		Description:  "The bug button: anyone may send one, signed in or not, under the same budget and ban as reports. Anonymous reports pass the human check.",
		Errors:       []int{http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		MaxBodyBytes: 32 << 10,
	}, accessOptional, limitReport), turnstileActionProblem, s.reportProblem)
}
