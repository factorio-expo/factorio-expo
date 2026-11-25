package staffops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/icontoken"
)

// Staff console -> Tags and Reserved usernames (docs/admin.md#tags): the
// curated tag groups, tags and suggestion rules, and the reserved usernames,
// seeded by 000002_seed_initial_data and edited here afterwards. Slugs never
// change (search documents and links hold them). Suggestion rules are held in
// memory by the catalog: a rule change (or a deleted tag) reloads them.

const (
	maxTagNameRunes      = 40
	maxTagSlugBytes      = 40
	maxTagPosition       = 10_000
	maxTagRuleCount      = 1_000_000
	maxTagRulePoints     = 100
	maxReservedNameRunes = 200
)

var (
	tagSlug      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	reservedName = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
)

// StaffTagGroup is a tag group with how many tags it holds.
type StaffTagGroup struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Position    int32  `json:"position"`
	Tags        int64  `json:"tags"`
}

// StaffTagRule is one tag suggestion rule.
type StaffTagRule struct {
	// Kind is entity or recipe; Name a prototype name.
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	MinCount int32  `json:"min_count"`
	// MaxCount is null for no upper bound.
	MaxCount *int32 `json:"max_count"`
	Points   int32  `json:"points"`
}

// StaffTag is a tag with its use and suggestion rules.
type StaffTag struct {
	ID          string         `json:"id"`
	GroupID     string         `json:"group_id"`
	Slug        string         `json:"slug"`
	DisplayName string         `json:"display_name"`
	Icon        string         `json:"icon"`
	Position    int32          `json:"position"`
	Entries     int64          `json:"entries"`
	Rules       []StaffTagRule `json:"rules"`
}

// StaffTags is the Tags page: groups and tags in their curated order.
type StaffTags struct {
	Groups []StaffTagGroup `json:"groups"`
	Tags   []StaffTag      `json:"tags"`
}

// ListStaffTags reads every tag group, tag and suggestion rule.
func (s *Service) ListStaffTags(ctx context.Context, by audit.Staff) (StaffTags, error) {
	groups, err := s.queries.ListStaffTagGroups(ctx)
	if err != nil {
		return StaffTags{}, fmt.Errorf("list tag groups: %w", err)
	}
	tags, err := s.queries.ListStaffTags(ctx)
	if err != nil {
		return StaffTags{}, fmt.Errorf("list tags: %w", err)
	}
	rules, err := s.queries.ListStaffTagRules(ctx)
	if err != nil {
		return StaffTags{}, fmt.Errorf("list tag rules: %w", err)
	}
	byTag := map[string][]StaffTagRule{}
	for _, rule := range rules {
		var maxCount *int32
		if rule.MaxCount.Valid {
			maxCount = &rule.MaxCount.Int32
		}
		byTag[rule.TagID] = append(byTag[rule.TagID], StaffTagRule{
			Kind: rule.Kind, Name: rule.Name, MinCount: rule.MinCount, MaxCount: maxCount, Points: rule.Points,
		})
	}
	page := StaffTags{Groups: make([]StaffTagGroup, 0, len(groups)), Tags: make([]StaffTag, 0, len(tags))}
	for _, group := range groups {
		page.Groups = append(page.Groups, StaffTagGroup{
			ID: group.ID, Slug: group.Slug, DisplayName: group.DisplayName, Position: group.Position, Tags: group.Tags,
		})
	}
	for _, tag := range tags {
		rules := byTag[tag.ID]
		if rules == nil {
			rules = []StaffTagRule{}
		}
		page.Tags = append(page.Tags, StaffTag{
			ID: tag.ID, GroupID: tag.GroupID, Slug: tag.Slug, DisplayName: tag.DisplayName,
			Icon: tag.Icon, Position: tag.Position, Entries: tag.Entries, Rules: rules,
		})
	}
	return page, nil
}

// TagFields are a tag's or group's editable fields; Slug is read only when
// one is created.
type TagFields struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	// Icon is a tag's game icon token ("[item=splitter]"); empty for none.
	Icon     string `json:"icon"`
	Position int32  `json:"position"`
	// GroupID is a tag's group (unused for a group).
	GroupID string `json:"group_id"`
}

func (f TagFields) clean(withSlug bool) (TagFields, error) {
	f.DisplayName = strings.TrimSpace(f.DisplayName)
	if f.DisplayName == "" || !printable(f.DisplayName) || utf8.RuneCountInString(f.DisplayName) > maxTagNameRunes {
		return f, invalidStaffRequest("the name is 1 to %d characters", maxTagNameRunes)
	}
	if f.Icon = strings.TrimSpace(f.Icon); f.Icon != "" {
		token, ok := icontoken.Parse(f.Icon)
		if !ok || token.Quality != "" && token.Quality != "normal" || icontoken.Format(token, false) != f.Icon {
			return f, invalidStaffRequest("the icon is one icon token such as [item=splitter]")
		}
	}
	if f.Position < 0 || f.Position > maxTagPosition {
		return f, invalidStaffRequest("the position is 0 to %d", maxTagPosition)
	}
	if withSlug {
		f.Slug = strings.TrimSpace(f.Slug)
		if len(f.Slug) > maxTagSlugBytes || !tagSlug.MatchString(f.Slug) {
			return f, invalidStaffRequest("the slug is lowercase letters and digits in words joined by '-', at most %d", maxTagSlugBytes)
		}
	}
	return f, nil
}

// checkGroup refuses a tag's malformed group id (an unknown one fails its
// foreign key).
func (f TagFields) checkGroup() error {
	if _, err := parseID(f.GroupID); err != nil {
		return invalidStaffRequest("unknown tag group")
	}
	return nil
}

// CreateTagGroup adds an empty tag group; the public tag list shows it once
// it holds a tag.
func (s *Service) CreateTagGroup(ctx context.Context, by audit.Staff, fields TagFields) error {
	fields, err := fields.clean(true)
	if err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		group, err := queries.CreateTagGroup(ctx, db.CreateTagGroupParams{
			Slug: fields.Slug, DisplayName: fields.DisplayName, Position: fields.Position,
		})
		if uniqueViolation(err) {
			return invalidStaffRequest("a tag group with that slug exists")
		}
		if err != nil {
			return fmt.Errorf("create tag group: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "tag_group.create", TargetType: "tag_group", TargetID: group.ID,
			After: map[string]any{"slug": group.Slug, "display_name": group.DisplayName, "position": group.Position},
		})
	})
}

// UpdateTagGroup renames or moves a tag group.
func (s *Service) UpdateTagGroup(ctx context.Context, by audit.Staff, groupID string, fields TagFields) error {
	fields, err := fields.clean(false)
	if err != nil {
		return err
	}
	if groupID, err = parseID(groupID); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		changed, err := queries.UpdateTagGroup(ctx, db.UpdateTagGroupParams{
			ID: groupID, DisplayName: fields.DisplayName, Position: fields.Position,
		})
		if err != nil {
			return fmt.Errorf("update tag group: %w", err)
		}
		if changed == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "tag_group.update", TargetType: "tag_group", TargetID: groupID,
			After: map[string]any{"display_name": fields.DisplayName, "position": fields.Position},
		})
	})
}

// DeleteTagGroup deletes an empty tag group.
func (s *Service) DeleteTagGroup(ctx context.Context, by audit.Staff, groupID string) error {
	groupID, err := parseID(groupID)
	if err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		deleted, err := queries.DeleteEmptyTagGroup(ctx, groupID)
		if err != nil {
			return fmt.Errorf("delete tag group: %w", err)
		}
		if deleted == 0 {
			tags, err := queries.CountTagsInGroup(ctx, groupID)
			if err != nil {
				return fmt.Errorf("count group tags: %w", err)
			}
			if tags > 0 {
				return invalidStaffRequest("move or delete its %d tags first", tags)
			}
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "tag_group.delete", TargetType: "tag_group", TargetID: groupID,
		})
	})
}

// CreateTag adds a tag to a group.
func (s *Service) CreateTag(ctx context.Context, by audit.Staff, fields TagFields) error {
	fields, err := fields.clean(true)
	if err != nil {
		return err
	}
	if err := fields.checkGroup(); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		tag, err := queries.CreateTag(ctx, db.CreateTagParams{
			GroupID: fields.GroupID, Slug: fields.Slug, DisplayName: fields.DisplayName, Icon: fields.Icon,
			Position: fields.Position,
		})
		if uniqueViolation(err) {
			return invalidStaffRequest("a tag with that slug exists")
		}
		if foreignKeyViolation(err) {
			return invalidStaffRequest("unknown tag group")
		}
		if err != nil {
			return fmt.Errorf("create tag: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "tag.create", TargetType: "tag", TargetID: tag.ID,
			After: map[string]any{
				"slug": tag.Slug, "display_name": tag.DisplayName, "icon": tag.Icon, "group_id": tag.GroupID,
				"position": tag.Position,
			},
		})
	})
}

// UpdateTag renames a tag, moves it to another group or position. Search
// documents hold slugs only, so nothing is reindexed.
func (s *Service) UpdateTag(ctx context.Context, by audit.Staff, tagID string, fields TagFields) error {
	fields, err := fields.clean(false)
	if err != nil {
		return err
	}
	if tagID, err = parseID(tagID); err != nil {
		return err
	}
	if err := fields.checkGroup(); err != nil {
		return err
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		before, err := queries.GetTagForUpdate(ctx, tagID)
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock tag: %w", err)
		}
		if _, err := queries.UpdateTag(ctx, db.UpdateTagParams{
			ID: tagID, GroupID: fields.GroupID, DisplayName: fields.DisplayName, Icon: fields.Icon,
			Position: fields.Position,
		}); foreignKeyViolation(err) {
			return invalidStaffRequest("unknown tag group")
		} else if err != nil {
			return fmt.Errorf("update tag: %w", err)
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "tag.update", TargetType: "tag", TargetID: tagID,
			Before: map[string]any{"display_name": before.DisplayName, "icon": before.Icon, "group_id": before.GroupID, "position": before.Position},
			After:  map[string]any{"display_name": fields.DisplayName, "icon": fields.Icon, "group_id": fields.GroupID, "position": fields.Position},
		})
	})
}

// DeleteTag deletes a tag: it leaves every entry that had it (their search
// documents are queued for a reindex) and its suggestion rules go with it.
func (s *Service) DeleteTag(ctx context.Context, by audit.Staff, tagID string) error {
	tagID, err := parseID(tagID)
	if err != nil {
		return err
	}
	return s.reloadRulesAfter(ctx, func() error {
		return s.catalog.InChange(ctx, func(c *catalog.Unit) error {
			queries := c.Queries()
			tag, err := queries.GetTagForUpdate(ctx, tagID)
			if errors.Is(err, pgx.ErrNoRows) {
				return catalog.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("lock tag: %w", err)
			}
			reindexed, err := c.ReindexTagged(ctx, tag.ID)
			if err != nil {
				return err
			}
			if _, err := queries.DeleteTag(ctx, tag.ID); err != nil {
				return fmt.Errorf("delete tag: %w", err)
			}
			return audit.Write(ctx, queries, by, audit.Record{
				Action: "tag.delete", TargetType: "tag", TargetID: tag.ID,
				Before: map[string]any{"slug": tag.Slug, "display_name": tag.DisplayName},
				After:  map[string]any{"entries_reindexed": reindexed},
			})
		})
	})
}

// SetTagRule adds or changes the tag's suggestion rule for a name; the new
// rules apply to the next upload at once.
func (s *Service) SetTagRule(ctx context.Context, by audit.Staff, tagID string, rule StaffTagRule) error {
	tagID, err := parseID(tagID)
	if err != nil {
		return err
	}
	rule.Name = strings.TrimSpace(rule.Name)
	switch rule.Kind {
	case "entity":
		if !s.catalog.Mods().DefinesEntity(rule.Name) {
			return invalidStaffRequest("no known entity is named %q", rule.Name)
		}
	case "recipe":
		if !s.catalog.Mods().DefinesRecipe(rule.Name) {
			return invalidStaffRequest("no known recipe is named %q", rule.Name)
		}
	case "quality":
		if rule.Name == "normal" || !icontoken.ValidQuality(rule.Name) {
			return invalidStaffRequest("the quality is uncommon, rare, epic or legendary")
		}
	default:
		return invalidStaffRequest("kind must be entity, recipe or quality")
	}
	if rule.MinCount < 1 || rule.MinCount > maxTagRuleCount ||
		(rule.MaxCount != nil && (*rule.MaxCount < rule.MinCount || *rule.MaxCount > maxTagRuleCount)) {
		return invalidStaffRequest("the counts are at least 1, the maximum (if any) at least the minimum")
	}
	if rule.Points < 1 || rule.Points > maxTagRulePoints {
		return invalidStaffRequest("points are 1 to %d", maxTagRulePoints)
	}
	maxCount := pgtype.Int4{}
	if rule.MaxCount != nil {
		maxCount = pgtype.Int4{Int32: *rule.MaxCount, Valid: true}
	}
	return s.reloadRulesAfter(ctx, func() error {
		return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
			if _, err := queries.GetTagForUpdate(ctx, tagID); errors.Is(err, pgx.ErrNoRows) {
				return catalog.ErrNotFound
			} else if err != nil {
				return fmt.Errorf("lock tag: %w", err)
			}
			if err := queries.UpsertTagRule(ctx, db.UpsertTagRuleParams{
				TagID: tagID, Kind: rule.Kind, Name: rule.Name, MinCount: rule.MinCount, MaxCount: maxCount,
				Points: rule.Points,
			}); err != nil {
				return fmt.Errorf("set tag rule: %w", err)
			}
			return audit.Write(ctx, queries, by, audit.Record{
				Action: "tag_rule.set", TargetType: "tag", TargetID: tagID, After: rule,
			})
		})
	})
}

// reloadRulesAfter runs a write that changes tag suggestion rules, then has
// the catalog read them again.
func (s *Service) reloadRulesAfter(ctx context.Context, write func() error) error {
	if err := write(); err != nil {
		return err
	}
	return s.catalog.LoadTagSuggestionRules(ctx)
}

// DeleteTagRule deletes one of the tag's suggestion rules.
func (s *Service) DeleteTagRule(ctx context.Context, by audit.Staff, tagID, kind, name string) error {
	tagID, err := parseID(tagID)
	if err != nil {
		return err
	}
	return s.reloadRulesAfter(ctx, func() error {
		return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
			deleted, err := queries.DeleteTagRule(ctx, db.DeleteTagRuleParams{TagID: tagID, Kind: kind, Name: name})
			if err != nil {
				return fmt.Errorf("delete tag rule: %w", err)
			}
			if deleted == 0 {
				return catalog.ErrNotFound
			}
			return audit.Write(ctx, queries, by, audit.Record{
				Action: "tag_rule.delete", TargetType: "tag", TargetID: tagID,
				Before: map[string]any{"kind": kind, "name": name},
			})
		})
	})
}

// ReservedUsername is a name nobody may sign up with.
type ReservedUsername struct {
	// Name is normalized: lower case, without '_' and '-'.
	Name      string    `json:"name"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	// TakenBy is the account that already has it (reserved after its
	// sign-up), if any; it keeps the name.
	TakenBy string `json:"taken_by"`
}

// ListReservedUsernames lists the reserved names alphabetically.
func (s *Service) ListReservedUsernames(ctx context.Context, by audit.Staff) ([]ReservedUsername, error) {
	rows, err := s.queries.ListReservedUsernames(ctx)
	if err != nil {
		return nil, fmt.Errorf("list reserved usernames: %w", err)
	}
	names := make([]ReservedUsername, 0, len(rows))
	for _, row := range rows {
		names = append(names, ReservedUsername{
			Name: row.Name, Reason: row.Reason, CreatedAt: row.CreatedAt.Time,
			TakenBy: row.TakenBy,
		})
	}
	return names, nil
}

// NormalizeReservedName is how reserved names are stored and matched:
// lower case, without '_' and '-' (the users.sql sign-up check).
func NormalizeReservedName(name string) string {
	return strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(name)))
}

// ReserveUsername reserves a name (normalized) or changes its reason. An
// account that already has it keeps it.
func (s *Service) ReserveUsername(ctx context.Context, by audit.Staff, name, reason string) error {
	name = NormalizeReservedName(name)
	if !reservedName.MatchString(name) {
		return invalidStaffRequest("a reserved name is 1 to 32 letters and digits ('_' and '-' are ignored)")
	}
	reason = strings.TrimSpace(reason)
	if !printable(reason) || utf8.RuneCountInString(reason) > maxReservedNameRunes {
		return invalidStaffRequest("the reason is at most %d characters", maxReservedNameRunes)
	}
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		inserted, err := queries.AddReservedUsername(ctx, db.AddReservedUsernameParams{Name: name, Reason: reason})
		if err != nil {
			return fmt.Errorf("reserve username: %w", err)
		}
		action := "reserved_username.update"
		if inserted {
			action = "reserved_username.add"
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: action, TargetType: "reserved_username", TargetID: name, After: map[string]any{"reason": reason},
		})
	})
}

// ReleaseUsername removes a reserved name.
func (s *Service) ReleaseUsername(ctx context.Context, by audit.Staff, name string) error {
	name = NormalizeReservedName(name)
	return s.catalog.InTransaction(ctx, func(queries *db.Queries) error {
		deleted, err := queries.DeleteReservedUsername(ctx, name)
		if err != nil {
			return fmt.Errorf("release username: %w", err)
		}
		if deleted == 0 {
			return catalog.ErrNotFound
		}
		return audit.Write(ctx, queries, by, audit.Record{
			Action: "reserved_username.remove", TargetType: "reserved_username", TargetID: name,
		})
	})
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func foreignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
