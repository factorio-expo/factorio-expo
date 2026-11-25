package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// deleteAccount is DELETE /api/v1/me: the caller deletes their own account
// (behind a fresh sign-in). Step one of account deletion
// (accountdeletion.Service.DeleteOwn, docs/admin.md#account-deletion): every session
// and sign-in method goes in the same transaction, so this token and every
// copy of it stop working at once and nobody can sign in to the account
// again. The cookie and a pending sign-up are cleared too.
func (s *server) deleteAccount(ctx context.Context, _ *struct{}) (*cookiesOutput, error) {
	if err := s.deletion.DeleteOwn(ctx, viewerFrom(ctx)); err != nil {
		return nil, err
	}
	out := &cookiesOutput{}
	out.Add(s.expiredSessionCookie(), s.expiredFlowCookie(signupFlowCookie))
	return out, nil
}

func (s *server) registerAccountDelete(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteAccount", Method: http.MethodDelete, Path: "/me",
		Tags:    []string{"Account"},
		Summary: "Delete the caller's own account",
		Description: "Needs a fresh sign-in. Every session and sign-in method goes at once; the session " +
			"cookie is cleared.",
	}, accessFresh, limitWrite), s.deleteAccount)
}
