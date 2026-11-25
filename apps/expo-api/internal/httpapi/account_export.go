package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/dataexport"
	"factorio-expo/apps/expo-api/internal/fault"
)

// exportWriteTimeout bounds one export download (the server's WriteTimeout
// is too short for a big account on a slow connection).
const exportWriteTimeout = 10 * time.Minute

// exportAccount streams the caller's data as a JSON download
// (docs/api.md#account-data-export). It runs behind a fresh sign-in and the
// export budget.
func (s *server) exportAccount(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
	account, err := s.exports.Find(ctx, viewerFrom(ctx))
	if errors.Is(err, dataexport.ErrNotFound) {
		return nil, fault.New(fault.NotFound, "not_found", "not found")
	}
	if err != nil {
		slog.Error("export account", "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}
	now := time.Now()
	// Usernames are [A-Za-z0-9_-], safe in a quoted filename.
	filename := fmt.Sprintf("factorio-expo-%s-%s.json", account.Username, now.UTC().Format("2006-01-02"))
	setup := func(w http.ResponseWriter) {
		_ = http.NewResponseController(w).SetWriteDeadline(now.Add(exportWriteTimeout))
		w.Header().Set("Cache-Control", "no-store")
	}
	return downloadStream("application/json; charset=utf-8", filename, setup, func(w io.Writer) error {
		err := s.exports.Write(ctx, w, account, now)
		if err != nil && ctx.Err() == nil {
			slog.Error("export account", "error", err)
		}
		return err
	}), nil
}

func (s *server) registerAccountExport(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "exportAccount", Method: http.MethodGet, Path: "/me/export",
		Tags:    []string{"Account"},
		Summary: "Download everything the account holds",
		Description: "A JSON attachment of the caller's data. Needs a fresh sign-in and draws its own, " +
			"smaller budget.",
		Responses: downloadResponse("application/json; charset=utf-8", "The account's data as a JSON attachment."),
		Errors:    []int{http.StatusNotFound},
	}, accessFresh, limitRead, s.budgetGate(limitExport)), s.exportAccount)
}
