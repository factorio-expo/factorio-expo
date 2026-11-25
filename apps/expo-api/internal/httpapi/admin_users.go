package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

// Users (admins only): the account list, one account, force logout, clearing
// a profile's status and links, and account deletion (catalog/staffusers.go).

// StaffUserPath names one account.
type StaffUserPath struct {
	UserID string `path:"userID" doc:"The account's id."`
}

type adminListUsersInput struct {
	Search string `query:"q" doc:"Matches part of the username or email."`
	Status string `query:"status" doc:"all, active, banned or deleted; empty is all."`
	Cursor string `query:"cursor" doc:"The previous page's next_cursor; empty for the first page."`
}

type adminUserPageOutput struct {
	Body staffops.StaffUserPage
}

type adminUserOutput struct {
	Body staffops.StaffUserDetail
}

type adminDeletionQueueResponse struct {
	Accounts []staffops.DeletedAccount `json:"accounts"`
}

type adminDeletionQueueOutput struct {
	Body adminDeletionQueueResponse
}

type adminUserReasonInput struct {
	StaffUserPath
	Body staffReason
}

// listAdminUsers lists accounts newest first, deleted ones included.
func (s *adminServer) listAdminUsers(ctx context.Context, in *adminListUsersInput) (*adminUserPageOutput, error) {
	page, err := s.ops.ListStaffUsers(ctx, staffUser(ctx), staffops.StaffUserQuery{
		Search: in.Search, Status: in.Status, Cursor: in.Cursor,
	})
	if err != nil {
		return nil, err
	}
	return &adminUserPageOutput{Body: page}, nil
}

func (s *adminServer) getAdminUser(ctx context.Context, in *StaffUserPath) (*adminUserOutput, error) {
	user, err := s.ops.GetStaffUser(ctx, staffUser(ctx), in.UserID)
	if err != nil {
		return nil, err
	}
	return &adminUserOutput{Body: user}, nil
}

// listDeletionQueue lists deleted accounts waiting for their hard delete.
func (s *adminServer) listDeletionQueue(ctx context.Context, _ *struct{}) (*adminDeletionQueueOutput, error) {
	accounts, err := s.ops.ListDeletionQueue(ctx, staffUser(ctx))
	if err != nil {
		return nil, err
	}
	return &adminDeletionQueueOutput{Body: adminDeletionQueueResponse{Accounts: accounts}}, nil
}

func (s *adminServer) logoutUser(ctx context.Context, in *adminUserReasonInput) (*struct{}, error) {
	return nil, s.ops.StaffLogoutUser(ctx, staffUser(ctx), in.UserID, in.Body.Reason)
}

func (s *adminServer) clearUserProfile(ctx context.Context, in *adminUserReasonInput) (*struct{}, error) {
	return nil, s.ops.StaffClearProfile(ctx, staffUser(ctx), in.UserID, in.Body.Reason)
}

func (s *adminServer) deleteUser(ctx context.Context, in *adminUserReasonInput) (*struct{}, error) {
	return nil, s.ops.StaffDeleteUser(ctx, staffUser(ctx), in.UserID, in.Body.Reason)
}

func (s *adminServer) registerAdminUsers(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listAdminUsers", Method: http.MethodGet, Path: "/admin/users",
		Tags: []string{"Users"}, Summary: "The account list, filtered and paged",
	}), s.listAdminUsers)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listDeletionQueue", Method: http.MethodGet, Path: "/admin/users/deletion-queue",
		Tags: []string{"Users"}, Summary: "Deleted accounts waiting for their hard delete",
	}), s.listDeletionQueue)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getAdminUser", Method: http.MethodGet, Path: "/admin/users/{userID}",
		Tags: []string{"Users"}, Summary: "One account with its entries and bans",
		Errors: []int{http.StatusNotFound},
	}), s.getAdminUser)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "logoutUser", Method: http.MethodPost, Path: "/admin/users/{userID}/logout",
		Tags: []string{"Users"}, Summary: "End every session of an account",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), s.logoutUser)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "clearUserProfile", Method: http.MethodPost, Path: "/admin/users/{userID}/clear-profile",
		Tags: []string{"Users"}, Summary: "Clear an account's profile status and links",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), s.clearUserProfile)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteUser", Method: http.MethodPost, Path: "/admin/users/{userID}/delete",
		Tags: []string{"Users"}, Summary: "Delete an account; its entries wait for the hard delete",
		MaxBodyBytes: staffWriteBodyLimit, Errors: []int{http.StatusNotFound},
	}), s.deleteUser)
}
