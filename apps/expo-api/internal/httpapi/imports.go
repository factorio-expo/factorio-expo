package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/importer"
)

// maxImportLinkBytes bounds the pasted link (importer.maxLinkLength) and the
// Turnstile token next to it.
const maxImportLinkBytes = 8 << 10

// errImportConsent is an import without the uploader's statement that they may
// share the blueprint.
var errImportConsent = fault.New(fault.Unprocessable, "import_consent_required",
	"confirm that you made this blueprint or have the right to share it here")

// errImportSourceUnverified is an upload that claims a source link without the
// token a resolve of that link gave for this very string.
var errImportSourceUnverified = fault.New(fault.Unprocessable, "import_source_unverified",
	"the blueprint does not match the link it claims to come from: import it again from the link")

// importRequest is POST /api/v1/imports/resolve.
type importRequest struct {
	Link           string `json:"link" minLength:"1" doc:"A link to a blueprint on a supported site (pastebin, hastebin, GitHub gists and raw files, GitLab raw files, FactorioBin, Google Docs, Factorio Prints)."`
	Consent        bool   `json:"consent" doc:"Must be true: the uploader made the blueprint or has the right to share it here."`
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The human check's token (action import), required when anonymous."`
}

type importInput struct {
	Body importRequest
}

// ImportedBlueprint is what a link held.
type ImportedBlueprint struct {
	Blueprint   string             `json:"blueprint" doc:"The exchange string, to upload like a pasted one."`
	SourceURL   string             `json:"source_url" doc:"The link's canonical form: send it as source_url with the upload, so the entry shows where it came from."`
	SourceToken string             `json:"source_token" doc:"Proof that this string came from source_url: send it as source_token with the upload. It is good for about 30 minutes and only for this exact string and link."`
	Title       string             `json:"title,omitempty" doc:"The title the site had for it, to suggest; absent when it has none."`
	Description string             `json:"description,omitempty" doc:"The description the site had for it, to suggest; absent when it has none."`
	Inspection  catalog.Inspection `json:"inspection" doc:"What inspectBlueprint answers for the string: the fetched text is a blueprint, kind, label, counts, suggested tags."`
}

type importOutput struct {
	Body ImportedBlueprint
}

// resolveImport fetches the exchange string behind a link. The API makes the
// request (internal/importer), never the browser; nothing is stored. What was
// fetched must parse as a blueprint (the inspect check) before anything is
// returned, so this is no general text proxy, and the answer carries the
// inspection and the token the upload presents.
func (s *server) resolveImport(ctx context.Context, in *importInput) (*importOutput, error) {
	if !in.Body.Consent {
		return nil, errImportConsent
	}
	// A link that is no supported one is refused before the human check spends
	// a token or anything is fetched.
	if _, err := s.importer.SourceURL(in.Body.Link); err != nil {
		return nil, err
	}
	if err := s.anonymousHumanError(ctx, in.Body.TurnstileToken, turnstileActionImport); err != nil {
		return nil, err
	}
	result, err := s.importer.Fetch(ctx, in.Body.Link)
	if err != nil {
		return nil, err
	}
	inspection, err := s.catalog.InspectBlueprint(ctx, result.Exchange)
	if err != nil {
		// Whatever the page held is not echoed back, in the error or the log.
		slog.Info("imported text is not a blueprint", "source", result.SourceURL)
		return nil, importer.ErrNoBlueprint
	}
	return &importOutput{Body: ImportedBlueprint{
		Blueprint: result.Exchange, SourceURL: result.SourceURL,
		SourceToken: s.importTokens.sign(result.SourceURL, result.Exchange),
		Title:       result.Title, Description: result.Description, Inspection: inspection,
	}}, nil
}

// importSource is an upload's canonical source link, "" for a plain upload. It
// is checked against the supported sites (without fetching anything), needs
// the uploader's consent, and needs the token a resolve of that link signed
// for this exact string, so an entry can only claim a source it was really
// imported from.
func (s *server) importSource(request submissionRequest) (string, error) {
	if request.SourceURL == "" {
		return "", nil
	}
	if !request.ImportConsent {
		return "", errImportConsent
	}
	source, err := s.importer.SourceURL(request.SourceURL)
	if err != nil {
		return "", err
	}
	if !s.importTokens.valid(request.SourceToken, source, request.Blueprint) {
		return "", errImportSourceUnverified
	}
	return source, nil
}

// registerImports registers this file's operations.
func (s *server) registerImports(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "resolveImport", Method: http.MethodPost, Path: "/imports/resolve",
		Tags:    []string{"Entries"},
		Summary: "Fetch the blueprint behind a link to another site",
		Description: "Only links to the supported sites are fetched, by the API, from a fixed address per site " +
			"(never the pasted URL), and only a blueprint is answered: the text must parse like an upload " +
			"(import_no_blueprint otherwise). Answers the exchange string with its inspection and a source " +
			"token, storing nothing: upload the string as usual with source_url, source_token and " +
			"import_consent. Anonymous callers pass the human check and get a much smaller rate limit; every " +
			"site also has an hourly fetch budget over all callers (503 import_site_busy).",
		MaxBodyBytes: maxImportLinkBytes,
		Errors:       []int{http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, accessOptional, limitImport, s.uploadsGate()), s.resolveImport)
}
