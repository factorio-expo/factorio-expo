package importer

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// Site is one supported host with its fixed adapter: which links it takes
// (Hosts, matched exactly), where the request goes (Base, never derived from
// the link) and how the answer becomes an exchange string. A request URL is
// Base plus a path the adapter builds from validated id characters only.
type Site struct {
	// Name is the adapter's name (a stable key, also its stub path in e2e).
	Name string
	// Hosts are the link hosts it handles, exact and lower-case; the first is
	// the canonical one the source URL shows.
	Hosts []string
	// Base is the origin (and optional path prefix) requests are sent to.
	Base string
	// plan checks the link's path segments and says what to request.
	plan func(segments []string) (plan, error)
	// redirect is the one extra host this adapter's request may be redirected
	// to (the table's own hosts always may); none for most sites.
	redirect redirectRule
}

// plan is one validated link turned into a request and a reading of its answer.
type plan struct {
	// source is the link's canonical path on the site's own host.
	source string
	// target is the path (and query) appended to Site.Base.
	target string
	// accept is the media type the answer must have.
	accept string
	// extract reads the answer's bytes.
	extract func(body []byte) (extracted, error)
	// details, when set, is a second request (path appended to Site.Base) for
	// a title and description, read by detailsExtract. Best effort: a failure
	// leaves the import without suggestions.
	details        string
	detailsExtract func(body []byte) (title, description string)
	// github marks GitHub's API: it wants a versioned Accept header, and a 403
	// or 429 from it is rate limiting.
	github bool
}

// extracted is what an adapter found: the exchange string and, from sites
// that have them, a title and description to suggest.
type extracted struct {
	exchange, title, description string
}

const (
	mediaText = "text/plain"
	mediaJSON = "application/json"
	// maxGistFiles is how many of a gist's files are looked through.
	maxGistFiles = 5
	// maxPathSegments bounds the repository paths copied into a request.
	maxPathSegments = 12
)

var (
	// pasteID is a pastebin id (8 letters and digits) or a hastebin key.
	pasteID = regexp.MustCompile(`^[A-Za-z0-9]{4,32}$`)
	// hastebinKey is a key with an optional file extension ("abc.txt").
	hastebinKey = regexp.MustCompile(`^([A-Za-z0-9]{4,32})(?:\.[A-Za-z0-9]{1,5})?$`)
	gistUser    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	gistID      = regexp.MustCompile(`^[0-9a-f]{8,40}$`)
	// pathSegment is one segment of a repository path: no slash, nothing a URL
	// parser could read twice (dot-only names are refused apart).
	pathSegment = regexp.MustCompile(`^[A-Za-z0-9._~+@=-]{1,200}$`)
	// idSegment is a site's own id (FactorioBin, Factorio Prints).
	idSegment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	codexID   = regexp.MustCompile(`^[0-9]{1,12}$`)
	docsID    = regexp.MustCompile(`^[A-Za-z0-9_-]{20,100}$`)
	// docsContentHost is where a Google Docs export is served from after its
	// redirect: one generated label under googleusercontent.com, shaped
	// doc-<xx>-<yy>-docstext (seen live 2026-11-15; Google's documentation
	// names no such host), so it is matched narrowly: a single label, no
	// wildcard over googleusercontent.com or google.com.
	docsContentHost = regexp.MustCompile(`^doc-[a-z0-9]+(?:-[a-z0-9]+){1,4}-docs(?:text)?\.googleusercontent\.com$`)
)

func isDocsContentHost(host string) bool { return docsContentHost.MatchString(host) }

func validPathSegments(segments []string, minimum int) bool {
	if len(segments) < minimum || len(segments) > maxPathSegments {
		return false
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." || !pathSegment.MatchString(segment) {
			return false
		}
	}
	return true
}

// DefaultSites are the supported sites with their real origins. factorio.school
// (its API answers 410 Gone) and Dropbox (links need a key) have no adapter.
func DefaultSites() []Site {
	return []Site{
		{Name: "pastebin", Hosts: []string{"pastebin.com", "www.pastebin.com"}, Base: "https://pastebin.com", plan: planPastebin},
		{Name: "hastebin", Hosts: []string{"hastebin.com"}, Base: "https://hastebin.com", plan: planHastebin},
		{Name: "gist", Hosts: []string{"gist.github.com"}, Base: "https://api.github.com", plan: planGist},
		{Name: "githubraw", Hosts: []string{"raw.githubusercontent.com"}, Base: "https://raw.githubusercontent.com", plan: planGitHubRaw},
		{Name: "gitlab", Hosts: []string{"gitlab.com"}, Base: "https://gitlab.com", plan: planGitLab},
		{Name: "factoriobin", Hosts: []string{"factoriobin.com", "www.factoriobin.com"}, Base: "https://factoriobin.com", plan: planFactorioBin,
			// blueprint.txt redirects to the post's file on the CDN.
			redirect: redirectRule{scheme: "https", match: func(host string) bool { return host == "cdn.factoriobin.com" }}},
		{Name: "googledocs", Hosts: []string{"docs.google.com"}, Base: "https://docs.google.com", plan: planGoogleDocs,
			redirect: redirectRule{scheme: "https", match: isDocsContentHost}},
		// The host spelling "facorio" is the real one (checked 2026-11-14).
		{Name: "factorioprints", Hosts: []string{"factorioprints.com", "www.factorioprints.com"},
			Base: "https://facorio-blueprints.firebaseio.com", plan: planFactorioPrints},
		{Name: "factoriocodex", Hosts: []string{"www.factoriocodex.com", "factoriocodex.com"},
			Base: "https://www.factoriocodex.com", plan: planFactorioCodex},
	}
}

// RebaseSites points every site at base + "/" + name, for a local or test
// stand-in server. Production never calls it.
func RebaseSites(sites []Site, base string) []Site {
	rebased := slices.Clone(sites)
	for i := range rebased {
		rebased[i].Base = strings.TrimRight(base, "/") + "/" + rebased[i].Name
	}
	return rebased
}

func textPlan(source, target string) plan {
	return plan{source: source, target: target, accept: mediaText, extract: extractText}
}

func planPastebin(segments []string) (plan, error) {
	if len(segments) == 2 && segments[0] == "raw" {
		segments = segments[1:]
	}
	if len(segments) != 1 || !pasteID.MatchString(segments[0]) {
		return plan{}, ErrUnsupportedSite
	}
	id := segments[0]
	return textPlan("/"+id, "/raw/"+id), nil
}

func planHastebin(segments []string) (plan, error) {
	if len(segments) == 2 && segments[0] == "raw" {
		segments = segments[1:]
	}
	if len(segments) != 1 {
		return plan{}, ErrUnsupportedSite
	}
	match := hastebinKey.FindStringSubmatch(segments[0])
	if match == nil {
		return plan{}, ErrUnsupportedSite
	}
	return textPlan("/"+match[1], "/raw/"+match[1]), nil
}

func planGist(segments []string) (plan, error) {
	var id, source string
	switch {
	case len(segments) == 1 && gistID.MatchString(segments[0]):
		id, source = segments[0], "/"+segments[0]
	case len(segments) == 2 && gistUser.MatchString(segments[0]) && gistID.MatchString(segments[1]):
		id, source = segments[1], "/"+segments[0]+"/"+segments[1]
	default:
		return plan{}, ErrUnsupportedSite
	}
	return plan{source: source, target: "/gists/" + id, accept: mediaJSON, extract: extractGist, github: true}, nil
}

func planGitHubRaw(segments []string) (plan, error) {
	// user / repo / ref / file (at least).
	if !validPathSegments(segments, 4) {
		return plan{}, ErrUnsupportedSite
	}
	path := "/" + strings.Join(segments, "/")
	return textPlan(path, path), nil
}

func planGitLab(segments []string) (plan, error) {
	if !validPathSegments(segments, 5) {
		return plan{}, ErrUnsupportedSite
	}
	// namespace/project/-/raw|blob/ref/file: a blob link reads as its raw one.
	marker := slices.Index(segments, "-")
	if marker < 2 || marker+3 >= len(segments) || (segments[marker+1] != "raw" && segments[marker+1] != "blob") {
		return plan{}, ErrUnsupportedSite
	}
	raw := slices.Clone(segments)
	raw[marker+1] = "raw"
	return textPlan("/"+strings.Join(segments, "/"), "/"+strings.Join(raw, "/")), nil
}

func planFactorioBin(segments []string) (plan, error) {
	// /post/<id>[/blueprint.txt]
	if n := len(segments); n == 3 && segments[2] == "blueprint.txt" {
		segments = segments[:2]
	}
	if len(segments) != 2 || segments[0] != "post" || !idSegment.MatchString(segments[1]) {
		return plan{}, ErrUnsupportedSite
	}
	path := "/post/" + segments[1]
	built := textPlan(path, path+"/blueprint.txt")
	built.details, built.detailsExtract = path+"/info.json", extractFactorioBinInfo
	return built, nil
}

// extractFactorioBinInfo reads a post's info.json: the root node's name and
// description, or the post's title when the node has no name.
func extractFactorioBinInfo(body []byte) (string, string) {
	var info struct {
		Post struct {
			Title string `json:"title"`
		} `json:"post"`
		Node struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"node"`
	}
	if json.Unmarshal(body, &info) != nil {
		return "", ""
	}
	title := suggestion(info.Node.Name)
	if title == "" {
		title = suggestion(info.Post.Title)
	}
	return title, suggestion(info.Node.Description)
}

func planGoogleDocs(segments []string) (plan, error) {
	// /document/d/<id>[/edit, /view, ...]
	if len(segments) < 3 || segments[0] != "document" || segments[1] != "d" || !docsID.MatchString(segments[2]) {
		return plan{}, ErrUnsupportedSite
	}
	id := segments[2]
	return textPlan("/document/d/"+id, "/document/d/"+id+"/export?format=txt"), nil
}

func planFactorioPrints(segments []string) (plan, error) {
	if len(segments) != 2 || segments[0] != "view" || !idSegment.MatchString(segments[1]) {
		return plan{}, ErrUnsupportedSite
	}
	id := segments[1]
	return plan{source: "/view/" + id, target: "/blueprints/" + id + ".json", accept: mediaJSON,
		extract: extractPrints}, nil
}

func planFactorioCodex(segments []string) (plan, error) {
	if len(segments) != 2 || segments[0] != "blueprints" || !codexID.MatchString(segments[1]) {
		return plan{}, ErrUnsupportedSite
	}
	id := segments[1]
	return plan{source: "/blueprints/" + id, target: "/api/v1/blueprints/" + id, accept: mediaJSON,
		extract: extractCodex}, nil
}

// extractCodex reads a Factorio Codex blueprint: its title and description and
// the string of its first version, which is the latest revision.
func extractCodex(body []byte) (extracted, error) {
	var record struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Versions    []struct {
			Blueprint string `json:"blueprint_string"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(body, &record); err != nil || len(record.Versions) == 0 {
		return extracted{}, ErrNoBlueprint
	}
	exchange, ok := exchangeIn(record.Versions[0].Blueprint)
	if !ok {
		return extracted{}, ErrNoBlueprint
	}
	return extracted{exchange: exchange, title: suggestion(record.Title), description: suggestion(record.Description)}, nil
}

// extractText reads a plain text page: the exchange string with any line
// breaks the paste added taken out.
func extractText(body []byte) (extracted, error) {
	exchange, ok := exchangeIn(string(body))
	if !ok {
		return extracted{}, ErrNoBlueprint
	}
	return extracted{exchange: exchange}, nil
}

// exchangeIn is text's exchange string: all whitespace removed (a paste may
// wrap its lines), then it must start with the version marker "0".
func exchangeIn(text string) (string, bool) {
	text = strings.TrimPrefix(text, string(rune(0xFEFF)))
	text = strings.Join(strings.Fields(text), "")
	if len(text) < 2 || text[0] != '0' {
		return "", false
	}
	return text, true
}

// extractGist takes the first of the gist's first few files (by name) whose
// content is an exchange string; its description is the suggested title.
func extractGist(body []byte) (extracted, error) {
	var gist struct {
		Description string `json:"description"`
		Files       map[string]struct {
			Content   string `json:"content"`
			Truncated bool   `json:"truncated"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &gist); err != nil {
		return extracted{}, ErrNoBlueprint
	}
	names := make([]string, 0, len(gist.Files))
	for name := range gist.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names[:min(len(names), maxGistFiles)] {
		file := gist.Files[name]
		if file.Truncated {
			continue
		}
		if exchange, ok := exchangeIn(file.Content); ok {
			return extracted{exchange: exchange, title: suggestion(gist.Description)}, nil
		}
	}
	return extracted{}, ErrNoBlueprint
}

// extractPrints reads a Factorio Prints record.
func extractPrints(body []byte) (extracted, error) {
	var record *struct {
		Title       string `json:"title"`
		Description string `json:"descriptionMarkdown"`
		Blueprint   string `json:"blueprintString"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record == nil {
		return extracted{}, ErrNoBlueprint
	}
	exchange, ok := exchangeIn(record.Blueprint)
	if !ok {
		return extracted{}, ErrNoBlueprint
	}
	return extracted{exchange: exchange, title: suggestion(record.Title), description: suggestion(record.Description)}, nil
}

// suggestion is imported text cleaned for use as a suggested title or
// description: valid UTF-8, trimmed.
func suggestion(text string) string {
	return strings.TrimSpace(strings.ToValidUTF8(text, ""))
}
