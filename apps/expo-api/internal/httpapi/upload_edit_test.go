package httpapi_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/importer"
	"factorio-expo/apps/expo-api/internal/ratelimit"
)

// stubSites answers every adapter's request with body as plain text (the way
// the e2e stack's stand-in server does), and records what was asked.
func stubImporter(t *testing.T, body string) (*importer.Fetcher, *[]string) {
	t.Helper()
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	fetcher, err := importer.New(importer.Config{
		Sites: importer.RebaseSites(importer.DefaultSites(), server.URL), MaxExchangeBytes: 8 << 20,
		Timeout: 2 * time.Second, AllowPrivateNetworks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fetcher, &requests
}

func problemCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v: %s", err, recorder.Body)
	}
	return problem.Code
}

func resolveRequest(handler http.Handler, sessionToken, link string, consent bool, turnstileToken string) *httptest.ResponseRecorder {
	return resolveRequestFrom(handler, "", sessionToken, link, consent, turnstileToken)
}

// resolveRequestFrom is resolveRequest from a given client address ("" keeps the default).
func resolveRequestFrom(handler http.Handler, remote, sessionToken, link string, consent bool, turnstileToken string) *httptest.ResponseRecorder {
	body := map[string]any{"link": link, "consent": consent}
	if turnstileToken != "" {
		body["turnstile_token"] = turnstileToken
	}
	encoded, _ := json.Marshal(body)
	request := authedRequest(http.MethodPost, "/api/v1/imports/resolve", sessionToken, encoded)
	if remote != "" {
		request.RemoteAddr = remote
	}
	return serve(handler, request)
}

func TestResolveImport(t *testing.T) {
	exchange := strings.TrimSpace(fixtureBlueprint(t, "bp-starter-smelter.txt"))
	fetcher, requests := stubImporter(t, exchange)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) { deps.Importer = fetcher })
	resolve := func(link string, consent bool) *httptest.ResponseRecorder {
		return resolveRequest(handler, "", link, consent, "")
	}

	// Without the consent statement nothing is fetched.
	denied := resolve("https://pastebin.com/aBcD1234", false)
	if denied.Code != http.StatusUnprocessableEntity || problemCode(t, denied) != "import_consent_required" {
		t.Fatalf("no consent = %d, %s", denied.Code, denied.Body)
	}
	if len(*requests) != 0 {
		t.Fatalf("fetched without consent: %v", *requests)
	}

	// A link to a host outside the table, and one that is not a link.
	for link, code := range map[string]string{
		"https://evil.test/aBcD1234":              "import_unsupported_site",
		"https://pastebin.com.evil.test/aBcD1234": "import_unsupported_site",
		"http://pastebin.com/aBcD1234":            "import_invalid_link",
		"https://127.0.0.1/x":                     "import_invalid_link",
		"0eNqrVkrKKU0tKMrMTS1WsqqOrQUAJxkFKQ==":   "import_invalid_link",
	} {
		refused := resolve(link, true)
		if refused.Code != http.StatusUnprocessableEntity || problemCode(t, refused) != code {
			t.Errorf("%s = %d %s, want 422 %s", link, refused.Code, refused.Body, code)
		}
	}
	if len(*requests) != 0 {
		t.Fatalf("fetched for refused links: %v", *requests)
	}

	ok := resolve("https://pastebin.com/aBcD1234", true)
	var imported httpapi.ImportedBlueprint
	if err := json.Unmarshal(ok.Body.Bytes(), &imported); ok.Code != http.StatusOK || err != nil {
		t.Fatalf("resolve = %d %v, %s", ok.Code, err, ok.Body)
	}
	if imported.Blueprint != exchange || imported.SourceURL != "https://pastebin.com/aBcD1234" {
		t.Fatalf("imported = %+v", imported)
	}
	if imported.SourceToken == "" {
		t.Fatal("no source token")
	}
	// The answer carries what inspectBlueprint says about the string.
	inspected := postJSON(t, handler, "/api/v1/blueprints/inspect", map[string]any{"blueprint": exchange})
	var want catalog.Inspection
	if err := json.Unmarshal(inspected.Body.Bytes(), &want); inspected.Code != http.StatusOK || err != nil {
		t.Fatalf("inspect = %d %v", inspected.Code, err)
	}
	if imported.Inspection.Kind == "" || imported.Inspection.EntityCount != want.EntityCount ||
		imported.Inspection.BlueprintCount != want.BlueprintCount || imported.Inspection.Label != want.Label {
		t.Fatalf("inspection = %+v, want %+v", imported.Inspection, want)
	}
	if len(*requests) != 1 || (*requests)[0] != "/pastebin/raw/aBcD1234" {
		t.Fatalf("requests = %v", *requests)
	}
}

// Text that merely starts with the exchange string's version marker is not
// returned: it must decode as a blueprint, and nothing of it is reflected.
func TestResolveImportIsNoTextProxy(t *testing.T) {
	const secret = "0 SECRET-internal-config-value: hunter2"
	fetcher, requests := stubImporter(t, secret)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) { deps.Importer = fetcher })
	refused := resolveRequest(handler, "", "https://pastebin.com/aBcD1234", true, "")
	if refused.Code != http.StatusUnprocessableEntity || problemCode(t, refused) != "import_no_blueprint" {
		t.Fatalf("not a blueprint = %d, %s", refused.Code, refused.Body)
	}
	if len(*requests) != 1 {
		t.Fatalf("requests = %v", *requests)
	}
	if strings.Contains(refused.Body.String(), "SECRET") || strings.Contains(refused.Body.String(), "hunter2") {
		t.Fatalf("fetched text reflected: %s", refused.Body)
	}
	// A string that is valid base64 and zlib but not a blueprint is refused too.
	other, _ := stubImporter(t, "0eNqrVkrKKU0tKMrMTS1WsqqOrQUAJxkFKQ==")
	handler, _ = newTestAPIWith(t, func(deps *httpapi.Dependencies) { deps.Importer = other })
	if r := resolveRequest(handler, "", "https://pastebin.com/aBcD1234", true, ""); r.Code != http.StatusUnprocessableEntity ||
		problemCode(t, r) != "import_no_blueprint" {
		t.Fatalf("undecodable payload = %d, %s", r.Code, r.Body)
	}
}

func TestResolveImportIsRateLimited(t *testing.T) {
	truncateSocialFixtures(t)
	exchange := strings.TrimSpace(fixtureBlueprint(t, "bp-starter-smelter.txt"))
	fetcher, _ := stubImporter(t, exchange)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Importer = fetcher
		deps.Limiter = ratelimit.New(1000)
	})
	count := func(remote, sessionToken string, attempts int) (allowed int, last *httptest.ResponseRecorder) {
		for range attempts {
			last = resolveRequestFrom(handler, remote, sessionToken, "https://pastebin.com/aBcD1234", true, "")
			if last.Code == http.StatusOK {
				allowed++
			}
		}
		return allowed, last
	}

	// An anonymous address is held to a couple, whatever it asks.
	allowed, last := count("", "", 12)
	if allowed != 2 || last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Fatalf("anonymous: allowed = %d, last = %d: an address gets 2 imports in a burst", allowed, last.Code)
	}

	// Signed in: an account gets 10, from any address; one address gets 15
	// across all the accounts on it.
	// (Accounts are made on a stack without limits: sign-up has its own.)
	plain, _ := newTestAPI(t)
	first, _ := registerTestUser(t, plain, "import-limit-alice")
	second, _ := registerTestUser(t, plain, "import-limit-bob")
	third, _ := registerTestUser(t, plain, "import-limit-carol")
	fourth, _ := registerTestUser(t, plain, "import-limit-dave")
	if allowed, last = count("198.51.100.7:1234", first, 14); allowed != 10 || last.Code != http.StatusTooManyRequests {
		t.Fatalf("account: allowed = %d, last = %d: an account gets 10 imports in a burst", allowed, last.Code)
	}
	if allowed, last = count("203.0.113.9:1234", second, 10); allowed != 10 || last.Code != http.StatusOK {
		t.Fatalf("another account on another address: allowed = %d, last = %d", allowed, last.Code)
	}
	// Three accounts on one address: 15 in all (carol's own ten are not the limit).
	if allowed, last = count("192.0.2.77:1234", third, 10); allowed != 10 {
		t.Fatalf("third account: allowed = %d", allowed)
	}
	if allowed, last = count("192.0.2.77:1234", fourth, 10); allowed != 5 || last.Code != http.StatusTooManyRequests {
		t.Fatalf("address: allowed = %d, last = %d: the address's 15 are spent across accounts", allowed, last.Code)
	}
}

func TestResolveImportNeedsTheHumanCheckWhenAnonymous(t *testing.T) {
	truncateSocialFixtures(t)
	plain, _ := newTestAPI(t)
	session, _ := registerTestUser(t, plain, "import-human-alice")
	exchange := strings.TrimSpace(fixtureBlueprint(t, "bp-starter-smelter.txt"))
	fetcher, requests := stubImporter(t, exchange)
	var down atomic.Bool
	verifier := fakeTurnstile(t, &down)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Importer = fetcher
		deps.Turnstile = verifier
	})
	link := "https://pastebin.com/aBcD1234"

	expectCode(t, "no token", resolveRequest(handler, "", link, true, ""), http.StatusForbidden, "turnstile_failed")
	expectCode(t, "another form's token", resolveRequest(handler, "", link, true, "pass:upload"), http.StatusForbidden, "turnstile_failed")
	expectCode(t, "a token that fails", resolveRequest(handler, "", link, true, "fail"), http.StatusForbidden, "turnstile_failed")
	if len(*requests) != 0 {
		t.Fatalf("fetched without a passed human check: %v", *requests)
	}
	// A link no adapter takes is refused before a token is spent or checked.
	expectCode(t, "bad link, no token", resolveRequest(handler, "", "https://evil.test/x", true, ""),
		http.StatusUnprocessableEntity, "import_unsupported_site")
	if r := resolveRequest(handler, "", link, true, "pass:import"); r.Code != http.StatusOK {
		t.Fatalf("anonymous with a passed check = %d, %s", r.Code, r.Body)
	}
	down.Store(true)
	expectCode(t, "check unavailable", resolveRequest(handler, "", link, true, "pass:import"),
		http.StatusServiceUnavailable, "turnstile_unavailable")
	down.Store(false)
	if r := resolveRequest(handler, session, link, true, ""); r.Code != http.StatusOK {
		t.Fatalf("signed in without a token = %d, %s", r.Code, r.Body)
	}
}

func TestUploadWithASourceNeedsConsentALinkAndItsToken(t *testing.T) {
	exchange := strings.TrimSpace(fixtureBlueprint(t, "bp-starter-smelter.txt"))
	fetcher, _ := stubImporter(t, exchange)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) { deps.Importer = fetcher })
	upload := func(extra map[string]any) *httptest.ResponseRecorder {
		body := map[string]any{"blueprint": exchange}
		maps.Copy(body, extra)
		return postJSON(t, handler, "/api/v1/entries", body)
	}
	resolve := func(link string) httpapi.ImportedBlueprint {
		t.Helper()
		r := resolveRequest(handler, "", link, true, "")
		var imported httpapi.ImportedBlueprint
		if err := json.Unmarshal(r.Body.Bytes(), &imported); r.Code != http.StatusOK || err != nil {
			t.Fatalf("resolve %s = %d %v, %s", link, r.Code, err, r.Body)
		}
		return imported
	}
	const link = "https://pastebin.com/aBcD1234"
	imported := resolve(link)

	if r := upload(map[string]any{"source_url": link, "source_token": imported.SourceToken}); r.Code != http.StatusUnprocessableEntity || problemCode(t, r) != "import_consent_required" {
		t.Fatalf("no consent = %d, %s", r.Code, r.Body)
	}
	for _, source := range []string{"https://evil.test/x", "https://pastebin.com.evil.test/aBcD1234", "javascript:alert(1)", "http://pastebin.com/aBcD1234"} {
		if r := upload(map[string]any{"source_url": source, "import_consent": true, "source_token": imported.SourceToken}); r.Code != http.StatusUnprocessableEntity {
			t.Errorf("source %q = %d, %s", source, r.Code, r.Body)
		}
	}

	// A claimed source without the token of this link and this string is refused.
	otherLink := resolve("https://pastebin.com/zZzZ9999")
	altered := strings.Replace(exchange, "e", "f", 1)
	for name, extra := range map[string]map[string]any{
		"no token":            {"source_url": link, "import_consent": true},
		"garbage token":       {"source_url": link, "import_consent": true, "source_token": "AAAA"},
		"a token of another":  {"source_url": link, "import_consent": true, "source_token": otherLink.SourceToken},
		"another string":      {"blueprint": altered, "source_url": link, "import_consent": true, "source_token": imported.SourceToken},
		"token, other source": {"source_url": "https://pastebin.com/zZzZ9999", "import_consent": true, "source_token": imported.SourceToken},
	} {
		r := upload(extra)
		if r.Code != http.StatusUnprocessableEntity || problemCode(t, r) != "import_source_unverified" {
			t.Errorf("%s = %d, %s, want 422 import_source_unverified", name, r.Code, r.Body)
		}
	}

	// The stored source is the canonical form, whatever was pasted around it.
	created := upload(map[string]any{"source_url": "https://www.pastebin.com/raw/aBcD1234?x=1#frag", "import_consent": true, "source_token": imported.SourceToken})
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); created.Code != http.StatusCreated || err != nil {
		t.Fatalf("create = %d %v, %s", created.Code, err, created.Body)
	}
	if entry.SourceURL != "https://pastebin.com/aBcD1234" {
		t.Fatalf("source = %q", entry.SourceURL)
	}
	read := revisionCall(t, handler, http.MethodGet, "/api/v1/entries/"+entry.ID, "", nil)
	var reloaded catalog.Entry
	if err := json.Unmarshal(read.Body.Bytes(), &reloaded); err != nil || reloaded.SourceURL != entry.SourceURL {
		t.Fatalf("read source = %q, %v", reloaded.SourceURL, err)
	}
	// An upload without a source needs none of this.
	if r := upload(nil); r.Code != http.StatusCreated {
		t.Fatalf("plain upload = %d, %s", r.Code, r.Body)
	}
}

func TestRevisionNoteThroughTheAPI(t *testing.T) {
	handler, _ := newTestAPI(t)
	blueprint := fixtureBlueprint(t, "bp-starter-smelter.txt")
	created := revisionCall(t, handler, http.MethodPost, "/api/v1/entries", "", map[string]string{"blueprint": blueprint})
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/entries/" + entry.ID

	added := revisionCall(t, handler, http.MethodPost, base+"/revisions", entry.EditToken,
		map[string]string{"blueprint": blueprint, "note": " Moved the inserters "})
	var revised catalog.Entry
	if err := json.Unmarshal(added.Body.Bytes(), &revised); added.Code != http.StatusCreated || err != nil || revised.Revision.Note != "Moved the inserters" {
		t.Fatalf("revision = %d %v, %s", added.Code, err, added.Body)
	}
	tooLong := revisionCall(t, handler, http.MethodPost, base+"/revisions", entry.EditToken,
		map[string]string{"blueprint": blueprint, "note": strings.Repeat("x", 281)})
	if tooLong.Code != http.StatusBadRequest || problemCode(t, tooLong) != "invalid_revision_note" {
		t.Fatalf("long note = %d, %s", tooLong.Code, tooLong.Body)
	}
	history := revisionCall(t, handler, http.MethodGet, base+"/history", "", nil)
	var steps struct {
		Steps []catalog.HistoryStep `json:"steps"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &steps); err != nil || len(steps.Steps) != 2 ||
		steps.Steps[0].Note != "" || steps.Steps[1].Note != "Moved the inserters" {
		t.Fatalf("history = %+v, %v", steps, err)
	}
}

func TestAliasThroughTheAPI(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	token, _ := registerTestUser(t, handler, "alias-owner")
	otherToken, _ := registerTestUser(t, handler, "alias-other")
	blueprint := fixtureBlueprint(t, "bp-starter-smelter.txt")

	create := func(token string, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		return serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", token, raw))
	}
	made := create(token, map[string]any{"blueprint": blueprint, "alias": "My-Smelter"})
	var entry catalog.Entry
	if err := json.Unmarshal(made.Body.Bytes(), &entry); made.Code != http.StatusCreated || err != nil || entry.Alias != "my-smelter" {
		t.Fatalf("create = %d %v, %s", made.Code, err, made.Body)
	}
	if anonymous := create("", map[string]any{"blueprint": blueprint, "alias": "nope-nope"}); anonymous.Code != http.StatusForbidden || problemCode(t, anonymous) != "alias_needs_account" {
		t.Fatalf("anonymous alias = %d, %s", anonymous.Code, anonymous.Body)
	}

	resolve := func(token, user, slug string) *httptest.ResponseRecorder {
		return serve(handler, authedRequest(http.MethodGet, "/api/v1/users/"+user+"/aliases/"+slug, token, nil))
	}
	found := resolve("", "ALIAS-owner", "my-smelter")
	var target struct {
		EntryID string `json:"entry_id"`
	}
	if err := json.Unmarshal(found.Body.Bytes(), &target); found.Code != http.StatusOK || err != nil || target.EntryID != entry.ID {
		t.Fatalf("resolve = %d %v, %s", found.Code, err, found.Body)
	}
	if other := resolve("", "alias-other", "my-smelter"); other.Code != http.StatusNotFound {
		t.Fatalf("another user's namespace = %d", other.Code)
	}

	patch := func(token string, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		return serve(handler, authedRequest(http.MethodPatch, "/api/v1/entries/"+entry.ID, token, raw))
	}
	if forbidden := patch(otherToken, map[string]any{"alias": "stolen-it"}); forbidden.Code != http.StatusForbidden {
		t.Fatalf("someone else's edit = %d", forbidden.Code)
	}
	if bad := patch(token, map[string]any{"alias": "not valid!"}); bad.Code != http.StatusBadRequest || problemCode(t, bad) != "invalid_alias" {
		t.Fatalf("bad alias = %d, %s", bad.Code, bad.Body)
	}
	if changed := patch(token, map[string]any{"alias": "copper-line"}); changed.Code != http.StatusOK {
		t.Fatalf("change = %d, %s", changed.Code, changed.Body)
	}
	if old := resolve("", "alias-owner", "my-smelter"); old.Code != http.StatusNotFound {
		t.Fatalf("old alias = %d, want 404", old.Code)
	}
	if now := resolve("", "alias-owner", "copper-line"); now.Code != http.StatusOK {
		t.Fatalf("new alias = %d", now.Code)
	}

	// Private: only the owner resolves it.
	if private := patch(token, map[string]any{"visibility": "private"}); private.Code != http.StatusOK {
		t.Fatalf("make private = %d, %s", private.Code, private.Body)
	}
	if hidden := resolve("", "alias-owner", "copper-line"); hidden.Code != http.StatusNotFound {
		t.Fatalf("private alias for the public = %d", hidden.Code)
	}
	if mine := resolve(token, "alias-owner", "copper-line"); mine.Code != http.StatusOK {
		t.Fatalf("private alias for its owner = %d", mine.Code)
	}
}

func TestUserFeedThroughTheAPI(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	token, _ := registerTestUser(t, handler, "feed-owner")
	blueprint := fixtureBlueprint(t, "bp-starter-smelter.txt")
	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		return serve(handler, authedRequest(http.MethodPost, path, token, raw))
	}
	made := post("/api/v1/entries", map[string]any{"blueprint": blueprint, "title": "Feed me", "description": "First"})
	var entry catalog.Entry
	if err := json.Unmarshal(made.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if r := post("/api/v1/entries/"+entry.ID+"/revisions", map[string]any{"blueprint": blueprint, "note": "Second"}); r.Code != http.StatusCreated {
		t.Fatalf("revision = %d, %s", r.Code, r.Body)
	}
	post("/api/v1/entries", map[string]any{"blueprint": blueprint, "visibility": "private"})

	feed := serve(handler, authedRequest(http.MethodGet, "/api/v1/users/FEED-owner/feed", "", nil))
	var body catalog.UserFeed
	if err := json.Unmarshal(feed.Body.Bytes(), &body); feed.Code != http.StatusOK || err != nil {
		t.Fatalf("feed = %d %v, %s", feed.Code, err, feed.Body)
	}
	if body.Username != "feed-owner" || len(body.Items) != 2 || body.Items[0].Note != "Second" ||
		body.Items[1].Description != "First" || !body.Items[1].FirstRevision {
		t.Fatalf("feed = %+v", body)
	}
	if missing := serve(handler, authedRequest(http.MethodGet, "/api/v1/users/nobody-here/feed", "", nil)); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown user = %d", missing.Code)
	}
}
