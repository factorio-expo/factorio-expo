package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/preview"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// previewRenderOutput is 200 with the current state, or 202 when a render was queued.
type previewRenderOutput struct {
	Status       int
	CacheControl string `header:"Cache-Control" doc:"no-store: the state changes as the render runs."`
	Body         preview.FullRender
}

// requestPreviewRender is requestPreviewRender as a Huma operation.
func (s *server) requestPreviewRender(ctx context.Context, in *BlueprintPath) (*previewRenderOutput, error) {
	viewer := viewerFrom(ctx)
	file, err := s.catalog.PreviewSource(ctx, in.BlueprintID, viewer)
	if err != nil {
		return nil, err
	}
	ticket := s.renderQueue.Rerender(renderqueue.Requester{UserID: viewer, IPHash: s.ipHash(requestFrom(ctx))})
	state, queued, err := s.renders.RequestFull(
		ctx, in.BlueprintID, file.SourceSHA256, file.Width, file.Height, ticket,
	)
	if errors.Is(err, renderqueue.ErrFull) {
		return nil, err
	}
	if err != nil {
		slog.Error("request preview render", "blueprint_id", in.BlueprintID, "error", err)
		return nil, huma.Error500InternalServerError("preview render is unavailable")
	}
	status := http.StatusOK
	if queued {
		status = http.StatusAccepted
	}
	return &previewRenderOutput{Status: status, CacheControl: "no-store", Body: state}, nil
}

// registerPreviewRender registers this file's operations.
func (s *server) registerPreviewRender(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "requestPreviewRender", Method: http.MethodPost,
		Path:    "/blueprints/{blueprintID}/preview/render",
		Tags:    []string{"Previews"},
		Summary: "Render an evicted full preview again",
		Description: "Asks for the full-size preview of a blueprint, evicted by the storage budget, to be " +
			"rendered again. 202 when a render was queued, 200 with the current state otherwise. Follow the " +
			"render on the entry's event stream, like an upload.",
		Responses: map[string]*huma.Response{"202": {
			Description: "A render was queued; the body is the pending state.",
			Content: map[string]*huma.MediaType{
				"application/json": {Schema: api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[preview.FullRender](), true, "")},
			},
		}},
		Errors: []int{http.StatusNotFound},
	}, accessOptional, limitRender), s.requestPreviewRender)
}
