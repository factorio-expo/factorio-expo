package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/runner"
)

type healthResponse struct {
	Service string `json:"service" doc:"The service answering (api, api-admin)." enum:"api,api-admin"`
	Status  string `json:"status" doc:"Always ok." enum:"ok"`
}

type runnerHealthOutput struct {
	Body runner.Health
}

// runnerHealth surfaces the internal runner's health to operators without
// exposing the runner itself to browsers.
func (s *server) runnerHealth(ctx context.Context, _ *struct{}) (*runnerHealthOutput, error) {
	response, err := s.runner.Health(ctx)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	return &runnerHealthOutput{Body: response}, nil
}

func (s *server) registerRunnerHealth(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getRunnerHealth", Method: http.MethodGet, Path: "/runner/health",
		Tags:    []string{"Site"},
		Summary: "The render runner's health",
		Errors:  []int{http.StatusBadGateway},
	}, accessPublic, limitRead), s.runnerHealth)
}
