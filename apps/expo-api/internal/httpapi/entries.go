package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/storage"
)

const (
	maxSubmissionBytes = 8 << 20
	defaultEntryPage   = 20
	maxEntryPage       = 100
)

// submissionRequest is an upload. It carries no render target: the game
// version inside an exchange string only records which game exported it, so
// the API picks the pinned content set to render against.
type submissionRequest struct {
	Blueprint   string   `json:"blueprint" minLength:"1" doc:"The exchange string to store, as the game exports it."`
	Title       *string  `json:"title,omitempty" doc:"The entry's title; absent takes the blueprint's own."`
	Description *string  `json:"description,omitempty" doc:"The entry's description."`
	Tags        []string `json:"tags,omitempty" doc:"The entry's tags, from the curated list."`
	Visibility  string   `json:"visibility,omitempty" doc:"public, unlisted or private; absent is the default."`
	// Spoiler marks the entry spoiler/NSFW.
	Spoiler bool `json:"spoiler,omitempty" doc:"Marks the entry spoiler/NSFW."`
	// ExpiresInDays is the upload's lifetime (1, 7, 14 or 30); 0 or absent never expires.
	ExpiresInDays int `json:"expires_in_days,omitempty" doc:"The lifetime in days (1, 7, 14 or 30); absent never expires."`
	// TurnstileToken is the form's human check, required when anonymous and
	// Turnstile is on (human_check.go).
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The human check's token, required when anonymous."`
	// SourceURL and ImportConsent come with a blueprint resolved from a link
	// (imports.go).
	SourceURL     string `json:"source_url,omitempty" doc:"The canonical link of an imported blueprint (from resolveImport): the entry then shows where it came from. Only a link to a supported site is accepted, and only with the source_token of that import."`
	SourceToken   string `json:"source_token,omitempty" doc:"Required with source_url: the token resolveImport gave for this exact string and link (good for about 30 minutes). Without a valid one the upload is refused with import_source_unverified."`
	ImportConsent bool   `json:"import_consent,omitempty" doc:"Required with source_url: the uploader made the blueprint or has the right to share it here."`
	// Alias is the vanity alias (catalog/alias.go).
	Alias string `json:"alias,omitempty" doc:"The vanity alias (lowercase letters, digits and hyphens, 3 to 60 characters), served at /@{username}/{alias}. Signed-in uploaders only."`
}

// submissionInput is an upload: an exchange string and its metadata.
type submissionInput struct {
	Body submissionRequest
}

// createEntry stores a submitted exchange string as a new catalog entry; the
// catalog wakes the preview dispatcher for its leaves.
func (s *server) createEntry(ctx context.Context, in *submissionInput) (*entryOutput, error) {
	request := in.Body
	sourceURL, err := s.importSource(request)
	if err != nil {
		return nil, err
	}
	entry, err := s.catalog.CreateEntry(ctx, catalog.Submission{
		Exchange:      request.Blueprint,
		Title:         request.Title,
		Description:   request.Description,
		Tags:          request.Tags,
		OwnerUserID:   viewerFrom(ctx),
		Visibility:    request.Visibility,
		Spoiler:       request.Spoiler,
		ExpiresInDays: request.ExpiresInDays,
		SourceURL:     sourceURL,
		Alias:         request.Alias,
		IPHash:        s.ipHash(requestFrom(ctx)),
	})
	if err != nil {
		return nil, err
	}
	return &entryOutput{Body: entry}, nil
}

// viewerID is the signed-in caller, or "" for anonymous requests.
func viewerID(r *http.Request) string {
	return viewerFrom(r.Context())
}

// viewerFrom is the signed-in caller of an operation, or "".
func viewerFrom(ctx context.Context) string {
	userID, _ := userIDFromContext(ctx)
	return userID
}

// EntryPath is the entry an operation is about.
type EntryPath struct {
	EntryID string `path:"entryID" doc:"The entry's public id (12 characters)."`
}

type entryOutput struct {
	Body catalog.Entry
}

func (s *server) getEntry(ctx context.Context, in *EntryPath) (*entryOutput, error) {
	entry, err := s.catalog.GetEntry(ctx, in.EntryID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &entryOutput{Body: entry}, nil
}

type leafInput struct {
	EntryPath
	BlueprintID string `path:"blueprintID" doc:"The blueprint's id."`
	IfNoneMatch string `header:"If-None-Match" doc:"The ETag of a copy the caller holds; a match answers 304."`
}

type leafOutput struct {
	ETag string `header:"ETag"`
	Body catalog.LeafDetail
}

func (s *server) getLeafDetail(ctx context.Context, in *leafInput) (*leafOutput, error) {
	detail, err := s.catalog.GetLeafDetail(ctx, in.EntryID, in.BlueprintID, viewerFrom(ctx), in.IfNoneMatch)
	if err != nil {
		return nil, err
	}
	if detail.NotModified {
		return nil, huma.ErrorWithHeaders(huma.Status304NotModified(), http.Header{"ETag": {detail.ETag}})
	}
	return &leafOutput{ETag: detail.ETag, Body: detail}, nil
}

// EntryAccessInput is an entry an operation changes or checks, with the edit
// token an anonymous entry's uploader presents.
type EntryAccessInput struct {
	EntryPath
	EditTokenInput
}

// authorization is the proof the caller presents: the signed-in account or
// the edit token.
func (in EntryAccessInput) authorization(ctx context.Context) catalog.Authorization {
	return catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
}

func (s *server) deleteEntry(ctx context.Context, in *EntryAccessInput) (*struct{}, error) {
	return nil, s.catalog.DeleteEntry(ctx, in.EntryID, in.authorization(ctx))
}

// previewCacheAge is how long browsers and CDNs may keep a shareable
// preview. A blueprint's image never changes, so this only bounds how long a
// cached copy outlives the entry turning private or being deleted.
const previewCacheAge = 24 * time.Hour

// getPreviewImage serves a blueprint's full preview or its thumbnail from the
// API's file storage, with the same visibility rules as entry reads.
type previewInput struct {
	BlueprintID string `path:"blueprintID" doc:"The blueprint's id."`
}

// getPreviewImage serves a blueprint's full preview or its thumbnail from the
// API's file storage, with the same visibility rules as entry reads.
// http.ServeContent answers conditional and range requests.
func (s *server) getPreviewImage(thumbnail bool) func(context.Context, *previewInput) (*huma.StreamResponse, error) {
	return func(ctx context.Context, in *previewInput) (*huma.StreamResponse, error) {
		file, err := s.catalog.PreviewSource(ctx, in.BlueprintID, viewerFrom(ctx))
		if err != nil {
			return nil, err
		}
		// A small preview's full URL serves its thumbnail file.
		object, servesThumbnail, err := s.previewFiles.Open(
			ctx, file.SourceSHA256, file.Width, file.Height, thumbnail,
		)
		if errors.Is(err, storage.ErrNotFound) && !thumbnail {
			// The storage budget evicted the full file; POST .../preview/render
			// renders it again. Never cached, so it shows up once it is back
			// (set here: an error's headers would only add to the default).
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				_, w := humago.Unwrap(hctx)
				w.Header().Set("Cache-Control", "no-store")
				writeErrorBody(w, http.StatusNotFound, statusCode(http.StatusNotFound),
					"full preview is not stored, request a render", nil)
			}}, nil
		}
		if err != nil {
			slog.Error("open preview file", "blueprint_id", in.BlueprintID,
				"thumbnail", thumbnail, "error", err)
			return nil, huma.Error500InternalServerError("preview image is unavailable")
		}

		etag := file.SourceSHA256
		if servesThumbnail {
			etag += "-thumb"
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer object.Close()
			r, w := humago.Unwrap(hctx)
			w.Header().Set("Content-Type", previewimage.MediaType)
			w.Header().Set("ETag", `"`+etag+`"`)
			// Shareable previews may sit in shared caches (CDNs); a private
			// entry's image must only ever reach its owner's browser.
			if file.Shareable {
				w.Header().Set("Cache-Control",
					fmt.Sprintf("public, max-age=%d", int(previewCacheAge.Seconds())))
			} else {
				w.Header().Set("Cache-Control", "private, no-store")
			}
			http.ServeContent(w, r, "", object.Info().ModTime, object)
		}}, nil
	}
}

// registerEntryWrites registers the entry create and delete operations and the
// permissions check (entry_metadata.go).
func (s *server) registerEntryWrites(api huma.API) {
	registerHumanChecked(api, s, s.operation(huma.Operation{
		OperationID: "createEntry", Method: http.MethodPost, Path: "/entries",
		Tags:    []string{"Entries"},
		Summary: "Upload an exchange string as a new entry",
		Description: "Anonymous uploads need the upload gate and, when Turnstile is on, a human check. " +
			"A source_url (an imported blueprint) needs import_consent and the source_token of its import.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxSubmissionBytes,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity,
			http.StatusServiceUnavailable},
	}, accessOptional, limitUpload, s.uploadsGate()), turnstileActionUpload, s.createEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getEntryPermissions", Method: http.MethodGet, Path: "/entries/{entryID}/permissions",
		Tags:        []string{"Entries"},
		Summary:     "Whether the caller may change the entry",
		Description: "The owner, or anyone with the edit token of an anonymous entry.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getEntryPermissions)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteEntry", Method: http.MethodDelete, Path: "/entries/{entryID}",
		Tags:        []string{"Entries"},
		Summary:     "Delete an entry",
		Description: "The owner, or anyone with the edit token of an anonymous entry.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, accessOptional, limitWrite), s.deleteEntry)
}

// registerEntryReads registers this file's operations.
func (s *server) registerEntryReads(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getEntry", Method: http.MethodGet, Path: "/entries/{entryID}",
		Tags:    []string{readAPI, "Entries"},
		Summary: "An entry with its live revision tree",
		Description: "The entry's metadata and the tree of books, blueprints and planners of its live " +
			"revision, with preview links. Analysis values are on the leaf endpoint.",
		Errors: []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getLeafDetail", Method: http.MethodGet,
		Path:    "/entries/{entryID}/blueprints/{blueprintID}",
		Tags:    []string{readAPI, "Entries"},
		Summary: "One blueprint with its analysis",
		Description: "A leaf of the entry's live revision: its own exchange string, the entities and " +
			"recipes it uses, the mods it needs and its production analysis (rates, electricity, build " +
			"cost, footprint, space platform stats). Supports ETag and If-None-Match.",
		Errors: []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getLeafDetail)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getPreviewImage", Method: http.MethodGet,
		Path:    "/blueprints/{blueprintID}/preview.webp",
		Tags:    []string{readAPI, "Previews"},
		Summary: "The full-size preview image",
		Description: "A lossy WebP (quality 80) rendered by the real game. 404 with a re-render hint once the " +
			"storage budget evicted the full file; use the thumbnail then. Cached for a day.",
		Responses: imageResponse(),
		Errors:    []int{http.StatusNotFound},
	}, accessOptional, limitPreview), s.getPreviewImage(false))
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getThumbnailImage", Method: http.MethodGet,
		Path:        "/blueprints/{blueprintID}/thumbnail.webp",
		Tags:        []string{readAPI, "Previews"},
		Summary:     "The preview thumbnail",
		Description: "A lossy WebP within 800 px. Never evicted.",
		Responses:   imageResponse(),
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitPreview), s.getPreviewImage(true))
}
