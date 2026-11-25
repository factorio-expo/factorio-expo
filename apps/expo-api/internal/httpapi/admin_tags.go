package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/staffops"
)

// Tags and reserved usernames (catalog/stafftags.go): the curated tag
// groups, tags and suggestion rules, and the names nobody may sign up with.
// Every body field is optional: the catalog validates each one and answers
// with its own code, which a schema check would replace.

type staffTagsOutput struct {
	Body staffops.StaffTags
}

// TagGroupPath is the tag group an operation is about.
type TagGroupPath struct {
	GroupID string `path:"groupID" doc:"The tag group's id."`
}

// TagPath is the tag an operation is about.
type TagPath struct {
	TagID string `path:"tagID" doc:"The tag's id."`
}

// ReservedNamePath is the reserved name an operation is about.
type ReservedNamePath struct {
	Name string `path:"name" doc:"The reserved name, as listed."`
}

// staffTagFieldsBody is a tag's or group's fields. Slug is read only when one
// is created; group_id is a tag's group (unused for a group).
type staffTagFieldsBody struct {
	Slug        string `json:"slug,omitempty" doc:"The URL slug; read only when created."`
	DisplayName string `json:"display_name,omitempty" doc:"The name shown to users."`
	Icon        string `json:"icon,omitempty" doc:"A tag's game icon token such as [item=splitter]; empty for none."`
	Position    int32  `json:"position,omitempty" doc:"The place in the curated order."`
	GroupID     string `json:"group_id,omitempty" doc:"A tag's group; unused for a group."`
}

func (b staffTagFieldsBody) fields() staffops.TagFields {
	return staffops.TagFields{Slug: b.Slug, DisplayName: b.DisplayName, Icon: b.Icon, Position: b.Position, GroupID: b.GroupID}
}

// staffTagRuleBody is one suggestion rule of a tag.
type staffTagRuleBody struct {
	Kind     string `json:"kind,omitempty" doc:"entity, recipe or quality."`
	Name     string `json:"name,omitempty" doc:"A prototype name."`
	MinCount int32  `json:"min_count,omitempty" doc:"The fewest uses that match, at least 1."`
	MaxCount *int32 `json:"max_count,omitempty" doc:"The most uses that match; null for no upper bound."`
	Points   int32  `json:"points,omitempty" doc:"The points the rule adds."`
}

// staffTagRuleKeyBody names the rule a delete removes.
type staffTagRuleKeyBody struct {
	Kind string `json:"kind,omitempty" doc:"entity, recipe or quality."`
	Name string `json:"name,omitempty" doc:"The prototype name."`
}

// staffReservedNameBody reserves a name.
type staffReservedNameBody struct {
	Name   string `json:"name,omitempty" doc:"The name to reserve (letters and digits)."`
	Reason string `json:"reason,omitempty" doc:"Why, recorded with the name."`
}

type staffReservedUsernames struct {
	Names []staffops.ReservedUsername `json:"names" doc:"The reserved names, alphabetically."`
}

type staffReservedUsernamesOutput struct {
	Body staffReservedUsernames
}

type staffTagGroupInput struct {
	Body staffTagFieldsBody
}

type staffTagGroupUpdateInput struct {
	TagGroupPath
	Body staffTagFieldsBody
}

type staffTagInput struct {
	Body staffTagFieldsBody
}

type staffTagUpdateInput struct {
	TagPath
	Body staffTagFieldsBody
}

type staffTagRuleInput struct {
	TagPath
	Body staffTagRuleBody
}

type staffTagRuleDeleteInput struct {
	TagPath
	Body staffTagRuleKeyBody
}

type staffReservedNameInput struct {
	Body staffReservedNameBody
}

func (s *adminServer) listTags(ctx context.Context, _ *struct{}) (*staffTagsOutput, error) {
	tags, err := s.ops.ListStaffTags(ctx, staffUser(ctx))
	if err != nil {
		return nil, err
	}
	return &staffTagsOutput{Body: tags}, nil
}

func (s *adminServer) createTagGroup(ctx context.Context, in *staffTagGroupInput) (*struct{}, error) {
	return nil, s.ops.CreateTagGroup(ctx, staffUser(ctx), in.Body.fields())
}

func (s *adminServer) updateTagGroup(ctx context.Context, in *staffTagGroupUpdateInput) (*struct{}, error) {
	return nil, s.ops.UpdateTagGroup(ctx, staffUser(ctx), in.GroupID, in.Body.fields())
}

func (s *adminServer) deleteTagGroup(ctx context.Context, in *TagGroupPath) (*struct{}, error) {
	return nil, s.ops.DeleteTagGroup(ctx, staffUser(ctx), in.GroupID)
}

func (s *adminServer) createTag(ctx context.Context, in *staffTagInput) (*struct{}, error) {
	return nil, s.ops.CreateTag(ctx, staffUser(ctx), in.Body.fields())
}

func (s *adminServer) updateTag(ctx context.Context, in *staffTagUpdateInput) (*struct{}, error) {
	return nil, s.ops.UpdateTag(ctx, staffUser(ctx), in.TagID, in.Body.fields())
}

func (s *adminServer) deleteTag(ctx context.Context, in *TagPath) (*struct{}, error) {
	return nil, s.ops.DeleteTag(ctx, staffUser(ctx), in.TagID)
}

func (s *adminServer) setTagRule(ctx context.Context, in *staffTagRuleInput) (*struct{}, error) {
	rule := staffops.StaffTagRule{
		Kind: in.Body.Kind, Name: in.Body.Name, MinCount: in.Body.MinCount,
		MaxCount: in.Body.MaxCount, Points: in.Body.Points,
	}
	return nil, s.ops.SetTagRule(ctx, staffUser(ctx), in.TagID, rule)
}

func (s *adminServer) deleteTagRule(ctx context.Context, in *staffTagRuleDeleteInput) (*struct{}, error) {
	return nil, s.ops.DeleteTagRule(ctx, staffUser(ctx), in.TagID, in.Body.Kind, in.Body.Name)
}

func (s *adminServer) listReservedUsernames(ctx context.Context, _ *struct{}) (*staffReservedUsernamesOutput, error) {
	names, err := s.ops.ListReservedUsernames(ctx, staffUser(ctx))
	if err != nil {
		return nil, err
	}
	return &staffReservedUsernamesOutput{Body: staffReservedUsernames{Names: names}}, nil
}

func (s *adminServer) reserveUsername(ctx context.Context, in *staffReservedNameInput) (*struct{}, error) {
	return nil, s.ops.ReserveUsername(ctx, staffUser(ctx), in.Body.Name, in.Body.Reason)
}

func (s *adminServer) releaseUsername(ctx context.Context, in *ReservedNamePath) (*struct{}, error) {
	return nil, s.ops.ReleaseUsername(ctx, staffUser(ctx), in.Name)
}

// registerAdminTags registers the Tags and Reserved usernames operations.
func (s *adminServer) registerAdminTags(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listAdminTags", Method: http.MethodGet, Path: "/admin/tags",
		Tags:    []string{"Tags"},
		Summary: "List tags",
		Description: "The curated tag groups, tags and their suggestion rules, in their order, " +
			"with how many entries use each tag.",
	}), s.listTags)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createTagGroup", Method: http.MethodPost, Path: "/admin/tag-groups",
		Tags:         []string{"Tags"},
		Summary:      "Create a tag group",
		MaxBodyBytes: staffWriteBodyLimit,
	}), s.createTagGroup)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateTagGroup", Method: http.MethodPost, Path: "/admin/tag-groups/{groupID}",
		Tags:         []string{"Tags"},
		Summary:      "Update a tag group",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.updateTagGroup)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteTagGroup", Method: http.MethodPost, Path: "/admin/tag-groups/{groupID}/delete",
		Tags:    []string{"Tags"},
		Summary: "Delete a tag group",
		Errors:  []int{http.StatusNotFound},
	}), s.deleteTagGroup)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createTag", Method: http.MethodPost, Path: "/admin/tags",
		Tags:         []string{"Tags"},
		Summary:      "Create a tag",
		MaxBodyBytes: staffWriteBodyLimit,
	}), s.createTag)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateTag", Method: http.MethodPost, Path: "/admin/tags/{tagID}",
		Tags:         []string{"Tags"},
		Summary:      "Update a tag",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.updateTag)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteTag", Method: http.MethodPost, Path: "/admin/tags/{tagID}/delete",
		Tags:    []string{"Tags"},
		Summary: "Delete a tag",
		Errors:  []int{http.StatusNotFound},
	}), s.deleteTag)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "setTagRule", Method: http.MethodPost, Path: "/admin/tags/{tagID}/rules",
		Tags:         []string{"Tags"},
		Summary:      "Set a tag suggestion rule",
		Description:  "Adds a rule, or replaces the rule for the same kind and name.",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.setTagRule)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteTagRule", Method: http.MethodPost, Path: "/admin/tags/{tagID}/rules/delete",
		Tags:         []string{"Tags"},
		Summary:      "Delete a tag suggestion rule",
		MaxBodyBytes: staffWriteBodyLimit,
		Errors:       []int{http.StatusNotFound},
	}), s.deleteTagRule)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listReservedUsernames", Method: http.MethodGet, Path: "/admin/reserved-usernames",
		Tags:    []string{"Reserved usernames"},
		Summary: "List reserved usernames",
		Description: "The names nobody may sign up with, alphabetically, with the account that " +
			"already took one (it keeps the name).",
	}), s.listReservedUsernames)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "reserveUsername", Method: http.MethodPost, Path: "/admin/reserved-usernames",
		Tags:         []string{"Reserved usernames"},
		Summary:      "Reserve a username",
		MaxBodyBytes: staffWriteBodyLimit,
	}), s.reserveUsername)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "releaseUsername", Method: http.MethodPost, Path: "/admin/reserved-usernames/{name}/delete",
		Tags:    []string{"Reserved usernames"},
		Summary: "Release a reserved username",
		Errors:  []int{http.StatusNotFound},
	}), s.releaseUsername)
}
