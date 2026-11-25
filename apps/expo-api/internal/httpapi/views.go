package httpapi

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/views"
	"factorio-expo/internal/shortid"
)

type recordViewInput struct {
	EntryPath
	UserAgent string `header:"User-Agent" doc:"The visitor's user agent; bots are not counted."`
}

// viewOutput is the no-body answer of recordView.
type viewOutput struct {
	CacheControl string `header:"Cache-Control" doc:"Always no-store."`
}

// recordView answers POST /api/v1/entries/{id}/view, the beacon the entry and
// leaf pages send once they are shown. It always answers 204 and never says
// whether the entry exists: the counter takes the view (or skips a bot or a
// repeat) and the periodic flush drops ids that name no countable entry.
func (s *server) recordView(ctx context.Context, in *recordViewInput) (*viewOutput, error) {
	if s.views != nil && shortid.Valid(in.EntryID) && !views.IsBot(in.UserAgent) {
		s.views.Record(in.EntryID, visitorAddr(s.clientIPs.clientIP(requestFrom(ctx))), in.UserAgent)
	}
	return &viewOutput{CacheControl: "no-store"}, nil
}

// visitorAddr is the address a visitor is known by: IPv6 by its /64, as the
// rate limits key it, since one household rotates through a whole prefix.
func visitorAddr(addr netip.Addr) netip.Addr {
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.Addr()
		}
	}
	return addr
}

func (s *server) registerViews(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "recordView", Method: http.MethodPost, Path: "/entries/{entryID}/view",
		Tags:          []string{"Entries"},
		Summary:       "Record an entry view",
		Description:   "The beacon the entry and leaf pages send once shown. Always 204; never says whether the entry exists.",
		DefaultStatus: http.StatusNoContent,
	}, accessPublic, limitView), s.recordView)
}
