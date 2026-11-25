package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

type bansResponse struct {
	Active []staffops.StaffBan `json:"active"`
	// Ended are revoked or expired bans, newest first.
	Ended []staffops.StaffBan `json:"ended"`
}

type listBansInput struct {
	Username string `query:"username" doc:"Only this user's bans; empty lists every user's."`
}

type bansOutput struct {
	Body bansResponse
}

// listBans lists user bans, active and ended, of every user or of
// ?username=.
func (s *adminServer) listBans(ctx context.Context, in *listBansInput) (*bansOutput, error) {
	active, err := s.ops.ListBans(ctx, in.Username, true)
	if err != nil {
		return nil, err
	}
	ended, err := s.ops.ListBans(ctx, in.Username, false)
	if err != nil {
		return nil, err
	}
	return &bansOutput{Body: bansResponse{Active: active, Ended: ended}}, nil
}

// newBanBody is the body of a new ban. The domain checks each field and
// answers its own code, so none is required here.
type newBanBody struct {
	Username   string     `json:"username,omitempty" doc:"The user to ban."`
	Actions    []string   `json:"actions,omitempty" doc:"What the ban stops: comment, upload and/or report."`
	ReasonCode string     `json:"reason_code,omitempty" doc:"Why: spam, abuse, illegal_content or other."`
	Note       string     `json:"note,omitempty" doc:"The staff note kept on the ban and in the audit log."`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" doc:"When the ban ends, unless permanent."`
	Permanent  bool       `json:"permanent,omitempty" doc:"The ban never ends."`
}

type createBanInput struct {
	Body newBanBody
}

type createBanOutput struct {
	Body staffops.StaffBan
}

func (s *adminServer) createBan(ctx context.Context, in *createBanInput) (*createBanOutput, error) {
	b := in.Body
	ban, err := s.ops.CreateBan(ctx, staffUser(ctx), staffops.NewBan{
		Username: b.Username, Actions: b.Actions, ReasonCode: b.ReasonCode, Note: b.Note,
		ExpiresAt: b.ExpiresAt, Permanent: b.Permanent,
	})
	if err != nil {
		return nil, err
	}
	return &createBanOutput{Body: ban}, nil
}

type BanPath struct {
	BanID string `path:"banID" doc:"The ban's id."`
}

// banEndBody is a new end for a ban: a time, or permanent (admins only).
type banEndBody struct {
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"The ban's new end."`
	Permanent bool       `json:"permanent,omitempty" doc:"Make the ban permanent (admins only)."`
	Reason    string     `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

type changeBanEndInput struct {
	BanPath
	Body banEndBody
}

func (s *adminServer) changeBanEnd(ctx context.Context, in *changeBanEndInput) (*struct{}, error) {
	return nil, s.ops.ChangeBanEnd(ctx, staffUser(ctx), in.BanID, staffops.BanEnd(in.Body))
}

type revokeBanInput struct {
	BanPath
	Body staffReason
}

func (s *adminServer) revokeBan(ctx context.Context, in *revokeBanInput) (*struct{}, error) {
	return nil, s.ops.RevokeBan(ctx, staffUser(ctx), in.BanID, in.Body.Reason)
}

type ipBansResponse struct {
	Bans []staffops.StaffIPBan `json:"bans"`
}

type ipBansOutput struct {
	Body ipBansResponse
}

func (s *adminServer) listIPBans(ctx context.Context, _ *struct{}) (*ipBansOutput, error) {
	bans, err := s.ops.ListIPBans(ctx)
	if err != nil {
		return nil, err
	}
	return &ipBansOutput{Body: ipBansResponse{Bans: bans}}, nil
}

// IPBanPath names an address ban by the hex HMAC of the address.
type IPBanPath struct {
	Hash string `path:"hash" doc:"The address ban's hash (hex HMAC of the address)."`
}

type liftIPBanRequest struct {
	// Action is the one action to lift; empty lifts every action.
	Action string `json:"action,omitempty" doc:"The one action to lift: comment, upload or report. Empty lifts every action."`
	Reason string `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

type liftIPBanInput struct {
	IPBanPath
	Body liftIPBanRequest
}

func (s *adminServer) liftIPBan(ctx context.Context, in *liftIPBanInput) (*struct{}, error) {
	return nil, s.ops.LiftIPBan(ctx, staffUser(ctx), in.Hash, in.Body.Action, in.Body.Reason)
}

func (s *adminServer) registerBans(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "changeBanEnd", Method: http.MethodPost, Path: "/admin/bans/{banID}/end",
		Tags:         []string{"Bans"},
		Summary:      "Move an active ban's end",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}), s.changeBanEnd)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listBans", Method: http.MethodGet, Path: "/admin/bans",
		Tags:    []string{"Bans"},
		Summary: "List user bans",
	}), s.listBans)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createBan", Method: http.MethodPost, Path: "/admin/bans",
		Tags:          []string{"Bans"},
		Summary:       "Ban a user",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  staffWriteBodyLimit,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}), s.createBan)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "revokeBan", Method: http.MethodPost, Path: "/admin/bans/{banID}/revoke",
		Tags:         []string{"Bans"},
		Summary:      "Revoke a ban",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict},
	}), s.revokeBan)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listIPBans", Method: http.MethodGet, Path: "/admin/ip-bans",
		Tags:    []string{"Bans"},
		Summary: "List address bans",
	}), s.listIPBans)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "liftIPBan", Method: http.MethodPost, Path: "/admin/ip-bans/{hash}/lift",
		Tags:         []string{"Bans"},
		Summary:      "Lift an address ban",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound},
	}), s.liftIPBan)
}
