package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// permissionsOutput is whether the caller may change the entry; it is never cached.
type permissionsOutput struct {
	CacheControl string `header:"Cache-Control" doc:"no-store: a caller's rights are never cached."`
	Body         entryPermissions
}

type entryPermissions struct {
	CanEdit bool `json:"can_edit" doc:"Whether the caller may change the entry (its owner, or the edit token's holder)."`
}

func (s *server) getEntryPermissions(ctx context.Context, in *EntryAccessInput) (*permissionsOutput, error) {
	canEdit, err := s.catalog.CanEditEntry(ctx, in.EntryID, in.authorization(ctx))
	if err != nil {
		return nil, err
	}
	return &permissionsOutput{CacheControl: "no-store", Body: entryPermissions{CanEdit: canEdit}}, nil
}

type entryMetadataInput struct {
	EntryPath
	EditTokenInput
	Body catalog.EntryMetadataUpdate
}

func (s *server) updateEntryMetadata(ctx context.Context, in *entryMetadataInput) (*entryOutput, error) {
	auth := catalog.Authorization{UserID: viewerFrom(ctx), EditToken: in.editToken()}
	entry, err := s.catalog.UpdateEntryMetadata(ctx, in.EntryID, auth, in.Body)
	if err != nil {
		return nil, err
	}
	return &entryOutput{Body: entry}, nil
}

// registerEntryMetadata registers this file's operations.
func (s *server) registerEntryMetadata(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateEntryMetadata", Method: http.MethodPatch, Path: "/entries/{entryID}",
		Tags:    []string{"Entries"},
		Summary: "Change an entry's title, description, tags, visibility or spoiler flag",
		Description: "The owner, or anyone with the edit token of an anonymous entry. Absent fields " +
			"keep their value.",
		MaxBodyBytes: 64 << 10,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
	}, accessOptional, limitWrite), s.updateEntryMetadata)
}
