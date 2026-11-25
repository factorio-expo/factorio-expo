package catalog

import (
	"context"
	"fmt"
	"slices"
)

// Tag is one entry in the curated, admin-defined discovery taxonomy: seeded
// by 000002_seed_initial_data, edited by staff in the console (stafftags.go).
type Tag struct {
	ID          string `json:"id" doc:"The tag's id."`
	Slug        string `json:"slug" doc:"The tag's slug."`
	DisplayName string `json:"display_name" doc:"The tag's display name."`
	Icon        string `json:"icon" doc:"The tag's game icon as a rich-text token such as [item=splitter]; empty for none."`
}

// TagGroup is a named, ordered set of tags (for example "Logistics"). Every
// tag belongs to exactly one group.
type TagGroup struct {
	ID          string `json:"id" doc:"The group's id."`
	Slug        string `json:"slug" doc:"The group's slug."`
	DisplayName string `json:"display_name" doc:"The group's display name."`
	Tags        []Tag  `json:"tags" doc:"The group's tags."`
}

// ListTagGroups returns every curated tag grouped, groups and the tags inside
// them in their curated order. Groups without tags are left out.
func (s *Service) ListTagGroups(ctx context.Context) ([]TagGroup, error) {
	groupRows, err := s.queries.ListTagGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tag groups: %w", err)
	}
	tagRows, err := s.queries.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	groups := make([]TagGroup, 0, len(groupRows))
	index := make(map[string]int, len(groupRows))
	for _, row := range groupRows {
		id := row.ID
		index[id] = len(groups)
		groups = append(groups, TagGroup{ID: id, Slug: row.Slug, DisplayName: row.DisplayName, Tags: []Tag{}})
	}
	for _, row := range tagRows {
		i, ok := index[row.GroupID]
		if !ok {
			continue
		}
		groups[i].Tags = append(groups[i].Tags, Tag{ID: row.ID, Slug: row.Slug, DisplayName: row.DisplayName, Icon: row.Icon})
	}
	// A group staff just made has no tags yet; it shows once it has one.
	return slices.DeleteFunc(groups, func(group TagGroup) bool { return len(group.Tags) == 0 }), nil
}
