package httpapi

import (
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
)

// Input and output pieces several operations share.

// PageQuery pages a list.
type PageQuery struct {
	Limit  int `query:"limit" minimum:"1" maximum:"100" default:"20" doc:"How many items to return."`
	Offset int `query:"offset" minimum:"0" maximum:"1048576" default:"0" doc:"How many items to skip."`
}

// ExchangeString is the body of an export: a book, blueprint or planner as
// a Factorio exchange string.
type ExchangeString struct {
	ExchangeString string `json:"exchange_string" doc:"The exchange string, as the game imports it."`
}

type exchangeStringOutput struct {
	Body ExchangeString
}

// BlueprintPath is the blueprint (leaf) an operation is about.
type BlueprintPath struct {
	BlueprintID string `path:"blueprintID" doc:"The blueprint's id."`
}

// EntryBlueprintPath is a blueprint of an entry's live revision.
type EntryBlueprintPath struct {
	EntryPath
	BlueprintID string `path:"blueprintID" doc:"The blueprint's id."`
}

// OpenJSON is a JSON object whose shape the API does not own: Factorio's own
// blueprint JSON, or the audit log's per-action snapshots. The document
// declares it an open object, never a typed one.
type OpenJSON struct {
	json.RawMessage
}

// Schema describes OpenJSON in the document.
func (OpenJSON) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type:                 huma.TypeObject,
		Nullable:             true,
		AdditionalProperties: true,
		Description:          "A JSON object whose keys the API passes through as stored or as the game defines them.",
	}
}
