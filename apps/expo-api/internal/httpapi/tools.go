package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/fault"
)

// The blueprint string tools (catalog/tools.go): they write nothing and
// share the tools rate limit. Bodies are capped like uploads.

type blueprintPayloadBody struct {
	Payload OpenJSON `json:"payload" doc:"The blueprint string's JSON, as Factorio defines it."`
}

// blueprintStringBody is the body of a string tool: the exchange string.
type blueprintStringBody struct {
	Blueprint string `json:"blueprint" minLength:"1" doc:"The exchange string, as the game exports it."`
}

type inspectBlueprintInput struct {
	Body blueprintStringBody
}

type inspectBlueprintOutput struct {
	Body catalog.Inspection
}

// inspectBlueprint answers the upload form's summary (catalog.Inspection).
func (s *server) inspectBlueprint(ctx context.Context, in *inspectBlueprintInput) (*inspectBlueprintOutput, error) {
	inspection, err := s.catalog.InspectBlueprint(ctx, in.Body.Blueprint)
	if err != nil {
		return nil, err
	}
	return &inspectBlueprintOutput{Body: inspection}, nil
}

type decodeBlueprintInput struct {
	Body blueprintStringBody
}

type decodeBlueprintOutput struct {
	Body blueprintPayloadBody
}

// decodeBlueprint answers {"payload": <the string's JSON>}.
func (s *server) decodeBlueprint(ctx context.Context, in *decodeBlueprintInput) (*decodeBlueprintOutput, error) {
	payload, err := s.catalog.DecodeBlueprint(in.Body.Blueprint)
	if err != nil {
		return nil, err
	}
	return &decodeBlueprintOutput{Body: blueprintPayloadBody{Payload: OpenJSON{payload}}}, nil
}

type encodeBlueprintInput struct {
	Body blueprintPayloadBody
}

type encodeBlueprintOutput struct {
	Body blueprintStringBody
}

// encodeBlueprint answers {"blueprint": "<exchange string>"}.
func (s *server) encodeBlueprint(ctx context.Context, in *encodeBlueprintInput) (*encodeBlueprintOutput, error) {
	if len(in.Body.Payload.RawMessage) == 0 || string(in.Body.Payload.RawMessage) == "null" {
		return nil, fault.New(fault.Invalid, "invalid_request", "payload is required")
	}
	exchange, err := s.catalog.EncodeBlueprint(in.Body.Payload.RawMessage)
	if err != nil {
		return nil, err
	}
	return &encodeBlueprintOutput{Body: blueprintStringBody{Blueprint: exchange}}, nil
}

// registerTools registers this file's operations.
func (s *server) registerTools(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "decodeBlueprint", Method: http.MethodPost, Path: "/blueprints/decode",
		Tags:         []string{"Blueprint tools"},
		Summary:      "Decode an exchange string to its JSON",
		MaxBodyBytes: maxSubmissionBytes,
		Errors:       []int{http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
	}, accessOptional, limitTools), s.decodeBlueprint)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "encodeBlueprint", Method: http.MethodPost, Path: "/blueprints/encode",
		Tags:         []string{"Blueprint tools"},
		Summary:      "Encode blueprint JSON to an exchange string",
		MaxBodyBytes: maxSubmissionBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
	}, accessOptional, limitTools), s.encodeBlueprint)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "inspectBlueprint", Method: http.MethodPost, Path: "/blueprints/inspect",
		Tags:    []string{"Blueprint tools"},
		Summary: "Summarize a pasted exchange string",
		Description: "Parses the string like an upload and summarizes it: kind, label, icons, game " +
			"version, counts, entities and recipes, suggested tags and the mods it needs. Stores nothing.",
		MaxBodyBytes: maxSubmissionBytes,
		Errors:       []int{http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
	}, accessOptional, limitTools), s.inspectBlueprint)
}
