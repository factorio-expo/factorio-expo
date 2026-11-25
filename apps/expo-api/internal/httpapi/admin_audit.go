package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/staffops"
)

const auditPageSize = 50

type adminActionResponse struct {
	ID            string    `json:"id" doc:"The action's id."`
	StaffUsername string    `json:"staff_username" doc:"The staff member who acted."`
	Action        string    `json:"action" doc:"What was done, e.g. entry.delete."`
	TargetType    string    `json:"target_type" doc:"What kind of thing it was done to."`
	TargetID      string    `json:"target_id" doc:"The id of the thing it was done to."`
	Reason        string    `json:"reason" doc:"The reason the staff member gave."`
	Before        OpenJSON  `json:"before" doc:"The target's state before, as the action recorded it; null when it recorded none."`
	After         OpenJSON  `json:"after" doc:"The target's state after, as the action recorded it; null when it recorded none."`
	CreatedAt     time.Time `json:"created_at" doc:"When it was done."`
}

type auditPageResponse struct {
	Actions []adminActionResponse `json:"actions" doc:"The actions, newest first."`
	// NextCursor fetches the next (older) page; empty on the last one.
	NextCursor string `json:"next_cursor" doc:"Pass as cursor to fetch the next (older) page; empty on the last one."`
}

type auditInput struct {
	Cursor string `query:"cursor" doc:"The previous page's next_cursor."`
}

type auditOutput struct {
	Body auditPageResponse
}

// listAudit serves the audit log newest first, a page at a time
// (?cursor= from the previous page's next_cursor).
func (s *adminServer) listAudit(ctx context.Context, in *auditInput) (*auditOutput, error) {
	params := db.ListAdminActionsParams{PageSize: auditPageSize + 1}
	if in.Cursor != "" {
		at, id, ok := staffops.ParseTimeCursor(in.Cursor)
		if !ok {
			return nil, fault.New(fault.Invalid, "invalid_request", "invalid cursor")
		}
		params.BeforeCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.BeforeID = id
	}
	rows, err := s.queries.ListAdminActions(ctx, params)
	if err != nil {
		slog.Error("list admin actions", "error", err)
		return nil, huma.Error500InternalServerError("could not load the audit log")
	}
	page := auditPageResponse{Actions: make([]adminActionResponse, 0, min(len(rows), auditPageSize))}
	for i, row := range rows {
		if i == auditPageSize {
			last := page.Actions[len(page.Actions)-1]
			page.NextCursor = staffops.FormatTimeCursor(last.CreatedAt, last.ID)
			break
		}
		page.Actions = append(page.Actions, adminActionResponse{
			ID: row.ID, StaffUsername: row.StaffUsername,
			Action: row.Action, TargetType: row.TargetType, TargetID: row.TargetID, Reason: row.Reason,
			Before: rawJSON(row.Before), After: rawJSON(row.After), CreatedAt: row.CreatedAt.Time,
		})
	}
	return &auditOutput{Body: page}, nil
}

// rawJSON passes stored jsonb through; a null column stays null.
func rawJSON(value []byte) OpenJSON {
	if len(value) == 0 {
		return OpenJSON{}
	}
	return OpenJSON{value}
}

func (s *adminServer) registerAdminAudit(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listAudit", Method: http.MethodGet, Path: "/admin/audit",
		Tags:    []string{"Audit"},
		Summary: "The audit log, newest first",
		Errors:  []int{http.StatusInternalServerError},
	}), s.listAudit)
}
