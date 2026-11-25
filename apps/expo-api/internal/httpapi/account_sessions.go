package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/sessions"
)

// The account page's signed-in devices: list them, sign one out, sign out
// everywhere. Signing out this device is also POST /api/v1/auth/logout.

type sessionResponse struct {
	sessions.Info
	// Current marks the session that made the request ("this device").
	Current bool `json:"current" doc:"This is the session that made the request (this device)."`
}

type sessionsResponse struct {
	Sessions []sessionResponse `json:"sessions" doc:"The account's signed-in devices."`
}

type sessionsOutput struct {
	Body sessionsResponse
}

func (s *server) listSessions(ctx context.Context, _ *struct{}) (*sessionsOutput, error) {
	session, _ := sessionFromContext(ctx)
	list, err := s.sessions.List(ctx, session.UserID)
	if err != nil {
		slog.Error("list sessions", "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}
	response := sessionsResponse{Sessions: make([]sessionResponse, 0, len(list))}
	for _, info := range list {
		response.Sessions = append(response.Sessions, sessionResponse{Info: info, Current: info.ID == session.ID})
	}
	return &sessionsOutput{Body: response}, nil
}

type endSessionInput struct {
	SessionID string `path:"sessionID" doc:"The session's id."`
}

// endSession signs one of the caller's sessions out; ending the current one
// also clears the cookie.
func (s *server) endSession(ctx context.Context, in *endSessionInput) (*cookiesOutput, error) {
	session, _ := sessionFromContext(ctx)
	id, err := parseID(in.SessionID)
	if err != nil {
		return nil, fault.New(fault.NotFound, "not_found", "not found")
	}
	switch err := s.sessions.End(ctx, session.UserID, id); {
	case err == nil:
	case errors.Is(err, sessions.ErrNotFound):
		return nil, fault.New(fault.NotFound, "not_found", "not found")
	default:
		slog.Error("end session", "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}
	out := &cookiesOutput{}
	if id == session.ID {
		out.Add(s.expiredSessionCookie())
	}
	return out, nil
}

// endAllSessions signs the caller out everywhere, this device included.
func (s *server) endAllSessions(ctx context.Context, _ *struct{}) (*cookiesOutput, error) {
	session, _ := sessionFromContext(ctx)
	if err := s.sessions.EndAll(ctx, session.UserID); err != nil {
		slog.Error("end sessions", "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}
	out := &cookiesOutput{}
	out.Add(s.expiredSessionCookie())
	return out, nil
}

func (s *server) registerSessions(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listSessions", Method: http.MethodGet, Path: "/me/sessions",
		Tags:    []string{"Account"},
		Summary: "The account's signed-in devices",
	}, accessRequired, limitRead), s.listSessions)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "endAllSessions", Method: http.MethodDelete, Path: "/me/sessions",
		Tags:    []string{"Account"},
		Summary: "Sign out everywhere, this device included",
	}, accessRequired, limitWrite), s.endAllSessions)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "endSession", Method: http.MethodDelete, Path: "/me/sessions/{sessionID}",
		Tags:        []string{"Account"},
		Summary:     "Sign one device out",
		Description: "Ending the current session also clears the session cookie.",
		Errors:      []int{http.StatusNotFound},
	}, accessRequired, limitWrite), s.endSession)
}
