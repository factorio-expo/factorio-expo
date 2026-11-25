package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// quickPreviewRequest is a quick preview: one blueprint string, no metadata.
type quickPreviewRequest struct {
	Blueprint string `json:"blueprint" minLength:"1" doc:"The exchange string of one blueprint (a book is refused)."`
	// TurnstileToken: required when anonymous and Turnstile is on.
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The human check's token, required when anonymous."`
}

// quickPreviewInput is a quick preview's body.
type quickPreviewInput struct {
	Body quickPreviewRequest
}

// createQuickPreview stores one blueprint as an expiring, unlisted entry. It
// shares the upload rate limit.
func (s *server) createQuickPreview(ctx context.Context, in *quickPreviewInput) (*entryOutput, error) {
	entry, err := s.catalog.CreateQuickPreview(ctx, in.Body.Blueprint, viewerFrom(ctx), s.ipHash(requestFrom(ctx)))
	if err != nil {
		return nil, err
	}
	return &entryOutput{Body: entry}, nil
}

// publishBody is what publishing sets (catalog.EntryPublish). Every field is
// optional here: the catalog decides what an absent value keeps or refuses,
// with its own codes.
type publishBody struct {
	Title       string    `json:"title,omitempty" doc:"The entry's title."`
	Description string    `json:"description,omitempty" doc:"The entry's description."`
	Tags        *[]string `json:"tags,omitempty" doc:"Replaces the tag set; absent keeps the entry's tags."`
	Visibility  string    `json:"visibility,omitempty" doc:"public, unlisted or private."`
	// ExpiresInDays is the lifetime from now (1, 7, 14 or 30); 0 never expires.
	ExpiresInDays *int `json:"expires_in_days,omitempty" doc:"The lifetime from now in days; absent keeps the entry's expiry."`
	// Spoiler marks the entry spoiler/NSFW; absent keeps its flag.
	Spoiler *bool `json:"spoiler,omitempty" doc:"Marks the entry spoiler/NSFW; absent keeps its flag."`
}

// entryPublishInput is the entry to publish, with the edit token that claims
// an anonymous one, and what publishing sets.
type entryPublishInput struct {
	EntryAccessInput
	Body publishBody
}

// publishEntry gives a finished quick preview or an anonymous entry to the
// signed-in caller (the edit token claims an anonymous one).
func (s *server) publishEntry(ctx context.Context, in *entryPublishInput) (*entryOutput, error) {
	request := catalog.EntryPublish{
		Title:         in.Body.Title,
		Description:   in.Body.Description,
		Tags:          in.Body.Tags,
		Visibility:    in.Body.Visibility,
		ExpiresInDays: in.Body.ExpiresInDays,
		Spoiler:       in.Body.Spoiler,
	}
	// Publishing makes an upload lasting, so an upload ban covers it.
	entry, err := s.catalog.PublishEntry(
		ctx, in.EntryID, in.authorization(ctx), s.ipHash(requestFrom(ctx)), request,
	)
	if err != nil {
		return nil, err
	}
	return &entryOutput{Body: entry}, nil
}

// registerPreviews registers the quick preview and publish operations.
func (s *server) registerPreviews(api huma.API) {
	registerHumanChecked(api, s, s.operation(huma.Operation{
		OperationID: "createQuickPreview", Method: http.MethodPost, Path: "/previews",
		Tags:          []string{"Previews"},
		Summary:       "Store one blueprint as an expiring, unlisted entry",
		Description:   "A quick preview has no metadata and lapses unless it is published. Books are refused.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxSubmissionBytes,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity,
			http.StatusServiceUnavailable},
	}, accessOptional, limitUpload, s.uploadsGate()), turnstileActionQuickPreview, s.createQuickPreview)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "publishEntry", Method: http.MethodPost, Path: "/entries/{entryID}/publish",
		Tags:         []string{"Previews"},
		Summary:      "Publish a finished quick preview or an anonymous entry",
		Description:  "Gives the entry to the signed-in caller and sets its metadata. The edit token claims an anonymous entry.",
		MaxBodyBytes: 64 << 10,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
			http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, accessRequired, limitWrite, s.uploadsGate()), s.publishEntry)
}
