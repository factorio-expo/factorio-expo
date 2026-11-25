package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

// The home page's hand-picked leaves (staffops/homepicks.go,
// docs/admin.md#home-picks): list, add, change the caption, remove.

// HomePickPath names one pick by its blueprint.
type HomePickPath struct {
	BlueprintID string `path:"blueprintID" doc:"The picked blueprint's id."`
}

type staffHomePicks struct {
	Picks []staffops.StaffHomePick `json:"picks" doc:"Every pick, newest first, shown or not."`
}

type staffHomePicksOutput struct {
	Body staffHomePicks
}

// staffHomePickBody adds a pick.
type staffHomePickBody struct {
	BlueprintID string `json:"blueprint_id" doc:"The blueprint to pick: one of the current revision of a public entry."`
	Caption     string `json:"caption,omitempty" doc:"Why it was picked: one line of at most 140 characters; empty for none."`
}

// staffHomePickCaptionBody changes a pick's caption.
type staffHomePickCaptionBody struct {
	Caption string `json:"caption,omitempty" doc:"The new caption; empty for none."`
}

type staffHomePickInput struct {
	Body staffHomePickBody
}

type staffHomePickCaptionInput struct {
	HomePickPath
	Body staffHomePickCaptionBody
}

func (s *adminServer) listHomePicks(ctx context.Context, _ *struct{}) (*staffHomePicksOutput, error) {
	picks, err := s.ops.ListHomePicks(ctx)
	if err != nil {
		return nil, err
	}
	return &staffHomePicksOutput{Body: staffHomePicks{Picks: picks}}, nil
}

func (s *adminServer) addHomePick(ctx context.Context, in *staffHomePickInput) (*struct{}, error) {
	return nil, s.ops.AddHomePick(ctx, staffUser(ctx), in.Body.BlueprintID, in.Body.Caption)
}

func (s *adminServer) setHomePickCaption(ctx context.Context, in *staffHomePickCaptionInput) (*struct{}, error) {
	return nil, s.ops.SetHomePickCaption(ctx, staffUser(ctx), in.BlueprintID, in.Body.Caption)
}

func (s *adminServer) removeHomePick(ctx context.Context, in *HomePickPath) (*struct{}, error) {
	return nil, s.ops.RemoveHomePick(ctx, staffUser(ctx), in.BlueprintID)
}

// registerAdminHomePicks registers the home picks operations.
func (s *adminServer) registerAdminHomePicks(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listHomePicks", Method: http.MethodGet, Path: "/admin/home-picks",
		Tags:        []string{"Home picks"},
		Summary:     "List home picks",
		Description: "Every hand-picked blueprint, newest first, with whether the home page shows it.",
	}), s.listHomePicks)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "addHomePick", Method: http.MethodPost, Path: "/admin/home-picks",
		Tags:         []string{"Home picks"},
		Summary:      "Pick a blueprint for the home page",
		Description:  "At most six picks; the blueprint must be in the current revision of a public entry.",
		MaxBodyBytes: staffWriteBodyLimit,
	}), s.addHomePick)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "setHomePickCaption", Method: http.MethodPost, Path: "/admin/home-picks/{blueprintID}",
		Tags:         []string{"Home picks"},
		Summary:      "Change a pick's caption",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.setHomePickCaption)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removeHomePick", Method: http.MethodPost, Path: "/admin/home-picks/{blueprintID}/delete",
		Tags:    []string{"Home picks"},
		Summary: "Remove a pick",
		Errors:  []int{http.StatusNotFound},
	}), s.removeHomePick)
}
