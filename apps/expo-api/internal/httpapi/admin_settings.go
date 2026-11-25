package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

// Settings (docs/admin.md#settings): every global setting with its value, and
// the change of some of them. The setting keys are fixed (settings.Definitions),
// so the list is typed; the update takes keys by name, each checked by the
// catalog (an unknown key is its invalid_request).

// adminSettingsUpdateBody is the body of a settings change. Both fields are
// optional in the schema: an empty change is a no-op, and the catalog checks
// the keys and values and the reason.
type adminSettingsUpdateBody struct {
	Values map[string]string `json:"values,omitempty" doc:"The new value of each setting, by its key."`
	Reason string            `json:"reason,omitempty" doc:"Why, recorded in the audit log."`
}

type adminSettingsUpdateInput struct {
	Body adminSettingsUpdateBody
}

type adminSettingsListResponse struct {
	Settings []staffops.StaffSetting `json:"settings"`
}

type adminSettingsListOutput struct {
	Body adminSettingsListResponse
}

type adminSettingsChangedResponse struct {
	// Changed are the keys whose value changed, sorted.
	Changed []string `json:"changed"`
}

type adminSettingsChangedOutput struct {
	Body adminSettingsChangedResponse
}

// listSettings is every global setting with its current value.
func (s *adminServer) listSettings(ctx context.Context, _ *struct{}) (*adminSettingsListOutput, error) {
	list, err := s.ops.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &adminSettingsListOutput{Body: adminSettingsListResponse{Settings: list}}, nil
}

// updateSettings sets some settings (values by key) and answers the keys that
// changed.
func (s *adminServer) updateSettings(ctx context.Context, in *adminSettingsUpdateInput) (*adminSettingsChangedOutput, error) {
	changed, err := s.ops.UpdateSettings(ctx, staffUser(ctx), staffops.SettingsUpdate(in.Body))
	if err != nil {
		return nil, err
	}
	return &adminSettingsChangedOutput{Body: adminSettingsChangedResponse{Changed: changed}}, nil
}

func (s *adminServer) registerAdminSettings(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listSettings", Method: http.MethodGet, Path: "/admin/settings",
		Tags: []string{"Settings"}, Summary: "Every global setting with its current value",
	}), s.listSettings)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateSettings", Method: http.MethodPost, Path: "/admin/settings",
		Tags: []string{"Settings"}, Summary: "Change some global settings",
		MaxBodyBytes: staffWriteBodyLimit,
	}), s.updateSettings)
}
