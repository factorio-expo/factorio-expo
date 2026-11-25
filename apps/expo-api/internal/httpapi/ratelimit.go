package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"factorio-expo/apps/expo-api/internal/ratelimit"
)

// Rate limits. Anonymous callers are keyed by client IP (IPv6 by /64) and get
// far smaller budgets than signed-in users; uploads, which each start
// Factorio render work, are the strictest. Nothing has a site-wide budget: a
// flood could spend it and lock everyone out. Anonymous writes that cost
// work (uploads, quick previews, revisions, reports) pass Turnstile instead
// (human_check.go), and the render queue caps each uploader's waiting leaves
// (internal/renderqueue).
var (
	ruleAllIP     = ratelimit.Rule{Name: "all-ip", Burst: 300, Every: 200 * time.Millisecond}
	ruleReadAnon  = ratelimit.Rule{Name: "read-ip", Burst: 120, Every: 500 * time.Millisecond}
	ruleReadUser  = ratelimit.Rule{Name: "read-user", Burst: 240, Every: 250 * time.Millisecond}
	rulePreviewIP = ratelimit.Rule{Name: "preview-ip", Burst: 200, Every: 100 * time.Millisecond}
	// ruleRawIP bounds the raw exchange-string routes, which skip ruleAllIP:
	// the blueprint editor fetches them for all its users from one address,
	// and each is one primary-key read (the CDN answers most from cache).
	ruleRawIP      = ratelimit.Rule{Name: "raw-ip", Burst: 600, Every: 50 * time.Millisecond}
	ruleSearchAnon = ratelimit.Rule{Name: "search-ip", Burst: 30, Every: 2 * time.Second}
	// ruleSitemapIP bounds the sitemap listing: the web app pages through all
	// listed entries in one go (5000 per page, by primary key) every few
	// minutes, from its own address.
	ruleSitemapIP  = ratelimit.Rule{Name: "sitemap-ip", Burst: 60, Every: time.Second}
	ruleSearchUser = ratelimit.Rule{Name: "search-user", Burst: 60, Every: time.Second}
	// The blueprint string tools decode a body of up to 8 MiB each, so they
	// get their own budget; the upload form calls inspect once per paste.
	ruleToolsAnon = ratelimit.Rule{Name: "tools-ip", Burst: 30, Every: 2 * time.Second}
	ruleToolsUser = ratelimit.Rule{Name: "tools-user", Burst: 60, Every: time.Second}
	// Imports make the API fetch another site, spending a third party's
	// quota: an anonymous address gets about three an hour with no real burst
	// (and passes Turnstile), a signed-in account about twenty an hour, and
	// any address, whoever is signed in on it, about thirty. The importer
	// adds a budget per site over all callers (internal/importer).
	ruleImportAnonIP = ratelimit.Rule{Name: "import-anon-ip", Burst: 2, Every: 20 * time.Minute}
	ruleImportUser   = ratelimit.Rule{Name: "import-user", Burst: 10, Every: 3 * time.Minute}
	ruleImportUserIP = ratelimit.Rule{Name: "import-user-ip", Burst: 15, Every: 2 * time.Minute}
	ruleUploadAnonIP = ratelimit.Rule{Name: "upload-anon-ip", Burst: 3, Every: 10 * time.Minute}
	ruleUploadUser   = ratelimit.Rule{Name: "upload-user", Burst: 10, Every: 2 * time.Minute}
	ruleUploadUserIP = ratelimit.Rule{Name: "upload-user-ip", Burst: 20, Every: time.Minute}
	// Re-rendering an evicted full preview starts Factorio work too; it also
	// counts against the requester's render queue cap. A request for a
	// stored or queued one is cheap but draws the same.
	ruleRenderIP  = ratelimit.Rule{Name: "render-ip", Burst: 10, Every: time.Minute}
	ruleWriteAnon = ratelimit.Rule{Name: "write-anon-ip", Burst: 10, Every: 6 * time.Second}
	ruleWriteUser = ratelimit.Rule{Name: "write-user", Burst: 30, Every: time.Second}
	// Sign-in steps per address: a provider sign-in or a passkey ceremony is
	// two requests. Nothing here is guessable (no passwords), so this only
	// keeps one address from hammering the providers and the database.
	ruleSignInIP = ratelimit.Rule{Name: "sign-in-ip", Burst: 20, Every: 10 * time.Second}
	// Account creation per address (IPv6 by /64) and per IPv6 /48, charged
	// only for an account actually created (signup_guard.go). There is no
	// site-wide budget for signing in or up: nobody may block everyone.
	ruleRegisterIP     = ratelimit.Rule{Name: "register-ip", Burst: 3, Every: 20 * time.Minute}
	ruleRegisterIP6Net = ratelimit.Rule{Name: "register-ip6-48", Burst: 10, Every: 6 * time.Minute}
	// Comments: a few in a burst, then one every two minutes, per account and
	// per address (several accounts on one address share it). Edits and
	// reports have their own budgets.
	ruleCommentUser = ratelimit.Rule{Name: "comment-user", Burst: 3, Every: 2 * time.Minute}
	ruleCommentIP   = ratelimit.Rule{Name: "comment-ip", Burst: 5, Every: 2 * time.Minute}
	ruleCommentEdit = ratelimit.Rule{Name: "comment-edit-user", Burst: 10, Every: 30 * time.Second}
	ruleReportUser  = ratelimit.Rule{Name: "report-user", Burst: 5, Every: 5 * time.Minute}
	// ruleExportUser: the account data export reads everything the account
	// holds, so a few per hour per account. Drawn only once the fresh
	// sign-in check passed (routes.go), so "confirm it's you" costs nothing.
	ruleExportUser = ratelimit.Rule{Name: "export-user", Burst: 3, Every: 20 * time.Minute}
	// Anonymous entry reports: a few per address, each through Turnstile.
	ruleReportAnonIP = ratelimit.Rule{Name: "report-anon-ip", Burst: 3, Every: 10 * time.Minute}
	// ruleViewIP bounds the page-view beacon; repeats are deduplicated anyway,
	// so it only keeps one address from filling the view counter.
	ruleViewIP = ratelimit.Rule{Name: "view-ip", Burst: 60, Every: 5 * time.Second}
)

// limitClass picks the budget a route draws from.
type limitClass int

const (
	limitRead limitClass = iota
	limitPreview
	limitRaw
	limitSearch
	limitSitemap
	limitTools
	limitUpload
	limitImport
	limitRender
	limitWrite
	limitSignIn
	limitView
	limitComment
	limitCommentEdit
	limitReport
	limitExport
	// limitNone draws from no route budget (only the global per-IP one).
	limitNone
)

// check is one bucket to draw from.
type check struct {
	rule ratelimit.Rule
	key  string
}

// allow draws from every bucket in order and writes a 429 on the first empty
// one. A nil limiter (tests) allows everything.
func (s *server) allow(w http.ResponseWriter, checks ...check) bool {
	if s.limiter == nil {
		return true
	}
	for _, c := range checks {
		if ok, wait := s.limiter.Allow(c.rule, c.key); !ok {
			tooManyRequests(w, wait)
			return false
		}
	}
	return true
}

// tooManyRequests writes a 429 saying when to retry.
func tooManyRequests(w http.ResponseWriter, wait time.Duration) {
	seconds := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	httpError(w, "too many requests, retry in "+strconv.Itoa(seconds)+"s", http.StatusTooManyRequests)
}

func (s *server) clientKey(r *http.Request) string {
	return clientKey(s.clientIPs.clientIP(r))
}

// withGlobalLimit is the per-IP safety net in front of every route.
func (s *server) withGlobalLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if !s.allow(w, check{ruleAllIP, s.clientKey(r)}) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limited applies a route's budget. It must run inside the auth middleware so
// signed-in callers are keyed by account.
func (s *server) limited(class limitClass, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allow(w, s.checksFor(class, r)...) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) checksFor(class limitClass, r *http.Request) []check {
	ip := s.clientKey(r)
	userID, signedIn := userIDFromContext(r.Context())
	switch class {
	case limitPreview:
		return []check{{rulePreviewIP, ip}}
	case limitRaw:
		return []check{{ruleRawIP, ip}}
	case limitSearch:
		if signedIn {
			return []check{{ruleSearchUser, userID}}
		}
		return []check{{ruleSearchAnon, ip}}
	case limitSitemap:
		return []check{{ruleSitemapIP, ip}}
	case limitTools:
		if signedIn {
			return []check{{ruleToolsUser, userID}}
		}
		return []check{{ruleToolsAnon, ip}}
	case limitUpload:
		if signedIn {
			return []check{{ruleUploadUserIP, ip}, {ruleUploadUser, userID}}
		}
		return []check{{ruleUploadAnonIP, ip}}
	case limitImport:
		if signedIn {
			return []check{{ruleImportUserIP, ip}, {ruleImportUser, userID}}
		}
		return []check{{ruleImportAnonIP, ip}}
	case limitRender:
		return []check{{ruleRenderIP, ip}}
	case limitWrite:
		if signedIn {
			return []check{{ruleWriteUser, userID}}
		}
		return []check{{ruleWriteAnon, ip}}
	case limitSignIn:
		return []check{{ruleSignInIP, ip}}
	case limitView:
		return []check{{ruleViewIP, ip}}
	case limitComment:
		return []check{{ruleCommentIP, ip}, {ruleCommentUser, userID}}
	case limitCommentEdit:
		return []check{{ruleCommentEdit, userID}}
	case limitExport:
		return []check{{ruleExportUser, userID}}
	case limitReport:
		if signedIn {
			return []check{{ruleReportUser, userID}}
		}
		return []check{{ruleReportAnonIP, ip}}
	default:
		if signedIn {
			return []check{{ruleReadUser, userID}}
		}
		return []check{{ruleReadAnon, ip}}
	}
}
