package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/settings"
)

// Emergency switches (docs/admin.md#settings): staff can pause uploads,
// sign-ups and comments from the console without a redeploy. A paused action
// answers 503 with a coded error the forms explain; reads, sign-in and
// everything else keep working. The checks run after authentication (the
// anonymous switch needs the viewer) and before the handler reads its body.

// Error codes of a paused action (writeCodedError).
const (
	uploadsPausedCode          = "uploads_paused"
	anonymousUploadsPausedCode = "anonymous_uploads_paused"
	signupsPausedCode          = "signups_paused"
	commentsPausedCode         = "comments_paused"
)

// pausedRetryAfter is the Retry-After a paused action answers with, seconds.
const pausedRetryAfter = "60"

func writePaused(w http.ResponseWriter, code, message string) {
	w.Header().Set("Retry-After", pausedRetryAfter)
	writeCodedError(w, http.StatusServiceUnavailable, code, message)
}

// uploadsOpen guards every route that adds an upload: new entries, new
// revisions, quick previews and publishing.
func (s *server) uploadsOpen(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := s.catalog.Settings(r.Context())
		switch {
		case !values.Bool(settings.AllowUploads):
			writePaused(w, uploadsPausedCode, "uploads are paused right now, try again later")
		case viewerID(r) == "" && !values.Bool(settings.AllowAnonymousUploads):
			writePaused(w, anonymousUploadsPausedCode,
				"uploads without an account are paused right now; sign in to upload, or try again later")
		default:
			next(w, r)
		}
	}
}

// signupsOpen guards the start of a sign-up (the passkey begin, a provider
// sign-in that finds no account). Later steps of a sign-up already under way
// are not guarded, so pausing never strands someone halfway.
func (s *server) signupsOpen(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.catalog.Settings(r.Context()).Bool(settings.AllowSignups) {
			writePaused(w, signupsPausedCode, "new sign-ups are paused right now, try again later")
			return
		}
		next(w, r)
	}
}

// commentsOpen guards posting a comment.
func (s *server) commentsOpen(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.catalog.Settings(r.Context()).Bool(settings.AllowComments) {
			writePaused(w, commentsPausedCode, "comments are paused right now, try again later")
			return
		}
		next(w, r)
	}
}

// siteStatusResponse is what visitors may know about the switches: the
// banner and which actions are open. The site's pages and forms read it
// (GET /api/v1/site) to explain a pause before anyone submits.
type siteStatusResponse struct {
	Banner           string `json:"banner" doc:"The site banner; empty when none is set."`
	Uploads          bool   `json:"uploads" doc:"Whether uploads are open."`
	AnonymousUploads bool   `json:"anonymous_uploads" doc:"Whether uploads without an account are open."`
	Signups          bool   `json:"signups" doc:"Whether new sign-ups are open."`
	Comments         bool   `json:"comments" doc:"Whether comments are open."`
}

type siteStatusOutput struct {
	Body siteStatusResponse
}

// siteStatus answers GET /api/v1/site.
func (s *server) siteStatus(ctx context.Context, _ *struct{}) (*siteStatusOutput, error) {
	values := s.catalog.Settings(ctx)
	return &siteStatusOutput{Body: siteStatusResponse{
		Banner:           values.String(settings.SiteBanner),
		Uploads:          values.Bool(settings.AllowUploads),
		AnonymousUploads: values.Bool(settings.AllowAnonymousUploads),
		Signups:          values.Bool(settings.AllowSignups),
		Comments:         values.Bool(settings.AllowComments),
	}}, nil
}

func (s *server) registerSite(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getSiteStatus", Method: http.MethodGet, Path: "/site",
		Tags:    []string{"Site"},
		Summary: "The site banner and which actions are open",
		Description: "What visitors may know about the emergency switches: the banner and whether uploads, " +
			"anonymous uploads, sign-ups and comments are open. The forms read it to explain a pause.",
	}, accessPublic, limitRead), s.siteStatus)
}
