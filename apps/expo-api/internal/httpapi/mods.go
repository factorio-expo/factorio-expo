package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/mods"
)

// modsetList is what mod detection knows beyond the game's own mods: the
// values the search `mods` filter accepts besides vanilla and space-age.
type modsetList struct {
	Modsets []mods.Listed `json:"modsets" doc:"The modsets the mod filter offers besides vanilla and space-age."`
}

type modsetListOutput struct {
	Body modsetList
}

func (s *server) listModsets(context.Context, *struct{}) (*modsetListOutput, error) {
	return &modsetListOutput{Body: modsetList{Modsets: s.catalog.Modsets()}}, nil
}

// registerModsets registers this file's operations.
func (s *server) registerModsets(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listModsets", Method: http.MethodGet, Path: "/modsets",
		Tags:        []string{readAPI, "Reference data"},
		Summary:     "The mods blueprints can be matched to",
		Description: "The modsets the search `mods` filter accepts besides vanilla and space-age.",
	}, accessPublic, limitRead), s.listModsets)
}
