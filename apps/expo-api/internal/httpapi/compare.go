package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/fault"
)

// errCompareSame is a comparison of a blueprint with itself.
var errCompareSame = fault.New(fault.Invalid, "invalid_compare", "compare two different blueprints")

type compareInput struct {
	A string `query:"a" required:"true" doc:"The first blueprint's id."`
	B string `query:"b" required:"true" doc:"The second blueprint's id."`
}

type compareResponse struct {
	A catalog.CompareLeaf `json:"a" doc:"The first blueprint."`
	B catalog.CompareLeaf `json:"b" doc:"The second blueprint."`
}

type compareOutput struct {
	Body compareResponse
}

// compareLeaves answers GET /api/v1/compare: two blueprints side by side, each
// a public blueprint of a non-quick-preview entry's current revision, or the
// whole request is a 404.
func (s *server) compareLeaves(ctx context.Context, in *compareInput) (*compareOutput, error) {
	if in.A == in.B {
		return nil, errCompareSame
	}
	a, err := s.catalog.CompareLeaf(ctx, in.A)
	if err != nil {
		return nil, err
	}
	b, err := s.catalog.CompareLeaf(ctx, in.B)
	if err != nil {
		return nil, err
	}
	return &compareOutput{Body: compareResponse{A: a, B: b}}, nil
}

// registerCompare registers this file's operations.
func (s *server) registerCompare(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "compareBlueprints", Method: http.MethodGet, Path: "/compare",
		Tags:    []string{readAPI, "Entries"},
		Summary: "Two blueprints side by side",
		Description: "The analysis of two blueprints (rates, power, size, build cost) for comparing. Only " +
			"blueprints of the current revision of public entries that are not quick previews; any other " +
			"id, or an unknown one, is a 404.",
		Errors: []int{http.StatusBadRequest, http.StatusNotFound},
	}, accessOptional, limitRead), s.compareLeaves)
}
