package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Anonymous writes that cost work pass Cloudflare Turnstile when it is on:
// an upload, an import from a link, a new revision made with an edit token,
// a quick preview, an entry report and a problem report. Signed-in callers
// never see it: their account is the accountability. Each such operation is
// registered with registerHumanChecked, which runs the check after the rate
// limit and after Huma parsed and validated the request, so a malformed
// request spends no token; the forms validate the exchange string (inspect)
// before they submit. The import runs its check itself (imports.go): a link
// to no supported site is refused before the check spends a token.

// Turnstile actions of the anonymous write forms (signup_guard.go has the
// sign-up ones).
const (
	turnstileActionUpload       = "upload"
	turnstileActionRevision     = "revision"
	turnstileActionQuickPreview = "quick-preview"
	turnstileActionReport       = "report"
	turnstileActionProblem      = "problem-report"
	turnstileActionImport       = "import"
)

// humanCheckedInput is the input of an operation behind the human check: the
// form's Turnstile token (empty for a signed-in caller).
type humanCheckedInput[I any] interface {
	*I
	turnstileToken() string
}

// registerHumanChecked registers op with its handler behind the human check
// of action for an anonymous caller, and documents the check's refusals (403
// turnstile_failed, 503 turnstile_unavailable).
func registerHumanChecked[I any, O any, P humanCheckedInput[I]](
	api huma.API, s *server, op huma.Operation, action string, handler func(context.Context, *I) (*O, error),
) {
	op.Errors = append(op.Errors, http.StatusForbidden, http.StatusServiceUnavailable)
	huma.Register(api, op, func(ctx context.Context, in *I) (*O, error) {
		if err := s.anonymousHumanError(ctx, P(in).turnstileToken(), action); err != nil {
			return nil, err
		}
		return handler(ctx, in)
	})
}

// anonymousHumanError is nil for a signed-in caller or a passed check, else
// the refusal as an error.
func (s *server) anonymousHumanError(ctx context.Context, token, action string) error {
	if viewerFrom(ctx) != "" {
		return nil
	}
	return s.verifyHuman(requestFrom(ctx), token, action)
}

func (in *submissionInput) turnstileToken() string         { return in.Body.TurnstileToken }
func (in *revisionSubmissionInput) turnstileToken() string { return in.Body.TurnstileToken }
func (in *quickPreviewInput) turnstileToken() string       { return in.Body.TurnstileToken }
func (in *reportEntryInput) turnstileToken() string        { return in.Body.TurnstileToken }
func (in *reportProblemInput) turnstileToken() string      { return in.Body.TurnstileToken }
