package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// Trending entries (docs/api.md#trending): the most viewed listed entries of
// today, this week or this month.

type trendingInput struct {
	Period string `query:"period" enum:"day,week,month" default:"week" doc:"The window views are counted in: today, this week (since Monday) or this month, all UTC."`
	Limit  int    `query:"limit" minimum:"1" maximum:"24" default:"12" doc:"How many entries to return."`
}

type trendingBody struct {
	Period  string                 `json:"period" enum:"day,week,month" doc:"The period the entries were ranked over."`
	Entries []catalog.EntrySummary `json:"entries" doc:"The most viewed entries in the period, most viewed first; fewer (or none) early in a period."`
}

type trendingOutput struct {
	Body trendingBody
}

// listTrending ranks listed entries by their views in the period.
func (s *server) listTrending(ctx context.Context, in *trendingInput) (*trendingOutput, error) {
	entries, err := s.catalog.ListTrending(ctx, catalog.TrendingPeriod(in.Period), int32(in.Limit), viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &trendingOutput{Body: trendingBody{Period: in.Period, Entries: entries}}, nil
}

func (s *server) registerTrending(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listTrending", Method: http.MethodGet, Path: "/trending",
		Tags:        []string{"Entries"},
		Summary:     "Trending entries",
		Description: "The listed entries with the most page views today, this week (since Monday) or this month, in UTC. A signed-in viewer sees whether they favorited each one.",
	}, accessOptional, limitRead), s.listTrending)
}
