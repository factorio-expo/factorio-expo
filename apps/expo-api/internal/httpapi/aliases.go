package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// AliasPath is a vanity alias: /@{username}/{slug}.
type AliasPath struct {
	ProfileUserPath
	Slug string `path:"slug" doc:"The alias, as in /@{username}/{slug}; matched in lower case."`
}

type aliasInput struct {
	AliasPath
}

// aliasTarget is the entry an alias names.
type aliasTarget struct {
	EntryID string `json:"entry_id" doc:"The entry's public id: the page is the entry's, canonical at /e/{entry_id}."`
}

type aliasOutput struct {
	Body aliasTarget
}

// resolveAlias names the entry behind /@{username}/{slug}; a missing or
// invisible entry and a replaced alias are all 404.
func (s *server) resolveAlias(ctx context.Context, in *aliasInput) (*aliasOutput, error) {
	id, err := s.catalog.ResolveAlias(ctx, in.Username, in.Slug, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &aliasOutput{Body: aliasTarget{EntryID: id}}, nil
}

// registerAliases registers this file's operations.
func (s *server) registerAliases(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "resolveAlias", Method: http.MethodGet, Path: "/users/{username}/aliases/{slug}",
		Tags:    []string{"Users"},
		Summary: "The entry behind a vanity alias",
		Description: "A signed-in owner can give an entry an alias, served at /@{username}/{slug}. " +
			"Changing the alias makes the old one answer 404.",
		Errors: []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.resolveAlias)
}
