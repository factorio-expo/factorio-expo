package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// A gate is a check an operation passes after its access and rate limit
// (s.operation's trailing arguments, run in the order given): a pause switch,
// a fresh sign-in, a second budget. It is net/http middleware, so it answers
// its refusals itself; errors lists the statuses it adds to the document.
type gate struct {
	wrap   func(http.Handler) http.Handler
	errors []int
}

// handlerGate adapts a `next http.HandlerFunc` wrapper (switches.go).
func handlerGate(wrap func(http.HandlerFunc) http.HandlerFunc, errors ...int) gate {
	return gate{
		wrap: func(next http.Handler) http.Handler {
			return http.HandlerFunc(wrap(next.ServeHTTP))
		},
		errors: errors,
	}
}

// uploadsGate: 503 while uploads (or anonymous ones) are paused.
func (s *server) uploadsGate() gate { return handlerGate(s.uploadsOpen, http.StatusServiceUnavailable) }

// commentsGate: 503 while comments are paused.
func (s *server) commentsGate() gate {
	return handlerGate(s.commentsOpen, http.StatusServiceUnavailable)
}

// signupsGate: 503 while new sign-ups are paused.
func (s *server) signupsGate() gate { return handlerGate(s.signupsOpen, http.StatusServiceUnavailable) }

// freshGate: 403 reauth_required unless the session signed in recently.
func (s *server) freshGate() gate {
	return gate{wrap: s.requireFreshSignIn, errors: []int{http.StatusForbidden}}
}

// registrationGate: 429 while the caller's address may create no more
// accounts (signup_guard.go); the handler charges the budget itself.
func (s *server) registrationGate() gate {
	return gate{wrap: s.withRegistrationBudget, errors: []int{http.StatusTooManyRequests}}
}

// budgetGate draws a second rate-limit class after the gates before it.
func (s *server) budgetGate(class limitClass) gate {
	return gate{wrap: func(next http.Handler) http.Handler { return s.limited(class, next) }}
}

type requestContextKey struct{}

// withRequestContext puts the request in the operation's context, for the
// handlers that need what only it has: the client address (rate limits, IP
// hashes, the human check) and the response writer's deadlines. Typed
// header, query and cookie fields are preferred for everything else.
func withRequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestContextKey{}, r)))
	})
}

// requestFrom is the request of an operation (withRequestContext).
func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey{}).(*http.Request)
	return r
}

// SetCookies is the Set-Cookie headers of a response: embed it in an output
// that sets or clears cookies, and fill it with cookie.String() values.
type SetCookies struct {
	SetCookie []string `header:"Set-Cookie" doc:"The cookies the response sets or clears."`
}

// Add sets or clears cookie.
func (c *SetCookies) Add(cookies ...*http.Cookie) {
	for _, cookie := range cookies {
		c.SetCookie = append(c.SetCookie, cookie.String())
	}
}

// withCookies attaches the Set-Cookie headers to an error response, for the
// refusals that still clear a ceremony's cookie.
func withCookies(err error, cookies SetCookies) error {
	if err == nil || len(cookies.SetCookie) == 0 {
		return err
	}
	return huma.ErrorWithHeaders(err, http.Header{"Set-Cookie": cookies.SetCookie})
}

// Redirect is the output of an operation that answers 302.
type Redirect struct {
	Location string `header:"Location" doc:"Where the browser goes next."`
	SetCookies
}

// redirectOperation documents op as answering 302 (Redirect).
func redirectOperation(op huma.Operation, description string) huma.Operation {
	op.DefaultStatus = http.StatusFound
	op.Responses = map[string]*huma.Response{"302": {Description: description}}
	return op
}

// downloadResponse documents a 200 file download of one media type.
func downloadResponse(contentType, description string) map[string]*huma.Response {
	return map[string]*huma.Response{"200": {
		Description: description,
		Content: map[string]*huma.MediaType{
			contentType: {Schema: &huma.Schema{Type: huma.TypeString}},
		},
	}}
}

// downloadStream answers 200 with write's bytes as contentType; a non-empty
// filename makes it an attachment. setup, when set, runs first with the
// response writer (deadlines). An error from write after the status is sent
// cuts the connection, so a download never ends as a truncated file.
func downloadStream(contentType, filename string, setup func(http.ResponseWriter), write func(io.Writer) error) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		_, w := humago.Unwrap(hctx)
		if setup != nil {
			setup(w)
		}
		header := w.Header()
		header.Set("Content-Type", contentType)
		if filename != "" {
			header.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		}
		if err := write(w); err != nil {
			panic(http.ErrAbortHandler)
		}
	}}
}

// textStream answers 200 with text as UTF-8 plain text.
func textStream(text string) *huma.StreamResponse {
	return downloadStream("text/plain; charset=utf-8", "", nil, func(w io.Writer) error {
		_, err := io.WriteString(w, text)
		return err
	})
}
