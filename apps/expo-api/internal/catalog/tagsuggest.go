package catalog

import (
	"context"
	"fmt"
	"sort"

	"factorio-expo/apps/expo-api/internal/blueprint"
)

// Tag suggestions: inspect scores the curated tags against what the pasted
// exchange string holds, using the rules seeded in tag_suggestion_rules.

// DefaultTagSuggestionMinScore is the score a tag needs to be suggested.
const DefaultTagSuggestionMinScore = 2

// maxTagSuggestions is how many tags are suggested overall, before ties with
// the last one are added; maxTagSuggestionsPerGroup caps one group, so a
// group with weak evidence (a space platform's size class) is not crowded out
// by another group's strong one.
const (
	maxTagSuggestions         = 8
	maxTagSuggestionsPerGroup = 3
)

// tagSuggestionRule is one row of tag_suggestion_rules.
type tagSuggestionRule struct {
	tag      string
	group    string // the tag's group slug
	kind     string
	name     string
	minCount int32
	maxCount *int32 // nil: no upper bound
	points   int32
}

// LoadTagSuggestionRules reads the rules into memory: once before the service
// serves requests, and again after staff edit the rules or delete a tag. The
// swap is atomic, so a submission being inspected sees the old or the new set.
func (s *Service) LoadTagSuggestionRules(ctx context.Context) error {
	rows, err := s.queries.ListTagSuggestionRules(ctx)
	if err != nil {
		return fmt.Errorf("list tag suggestion rules: %w", err)
	}
	rules := make([]tagSuggestionRule, 0, len(rows))
	for _, row := range rows {
		rule := tagSuggestionRule{
			tag: row.TagSlug, group: row.GroupSlug, kind: row.Kind, name: row.Name, minCount: row.MinCount, points: row.Points,
		}
		if row.MaxCount.Valid {
			rule.maxCount = &row.MaxCount.Int32
		}
		rules = append(rules, rule)
	}
	s.tagRules.Store(&rules)
	return nil
}

// suggestTags scores every tag that has rules: each rule whose name the
// submission holds at least minCount (and at most maxCount) times adds its points
// (kind "quality": entities of that quality). Tags below minScore are dropped;
// the best of each group (maxTagSuggestionsPerGroup) are kept, then the best
// maxTagSuggestions of those are returned, plus any tag tied with the last.
// Equal scores keep the rules' curated tag order.
func suggestTags(rules []tagSuggestionRule, summary blueprint.Summary, minScore int) []string {
	counts := map[[2]string]int32{}
	for _, entity := range summary.Entities {
		counts[[2]string{"entity", entity.Name}] += entity.Count
		counts[[2]string{"quality", entity.Quality}] += entity.Count
	}
	for _, recipe := range summary.Recipes {
		counts[[2]string{"recipe", recipe.Name}] += recipe.Count
	}

	type scored struct {
		tag, group string
		score      int
	}
	var tags []scored
	index := map[string]int{}
	for _, rule := range rules {
		count := counts[[2]string{rule.kind, rule.name}]
		if count < rule.minCount || (rule.maxCount != nil && count > *rule.maxCount) {
			continue
		}
		i, ok := index[rule.tag]
		if !ok {
			i = len(tags)
			index[rule.tag] = i
			tags = append(tags, scored{tag: rule.tag, group: rule.group})
		}
		tags[i].score += int(rule.points)
	}
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].score > tags[j].score })

	suggested := []string{}
	perGroup := map[string]int{}
	lastScore := 0
	for _, tag := range tags {
		if tag.score < minScore {
			break
		}
		if perGroup[tag.group] >= maxTagSuggestionsPerGroup {
			continue
		}
		if len(suggested) >= maxTagSuggestions && tag.score < lastScore {
			break
		}
		perGroup[tag.group]++
		suggested = append(suggested, tag.tag)
		if len(suggested) == maxTagSuggestions {
			lastScore = tag.score
		}
	}
	return suggested
}

// currentTagRules is the loaded rule set (none before the first load).
func (s *Service) currentTagRules() []tagSuggestionRule {
	if rules := s.tagRules.Load(); rules != nil {
		return *rules
	}
	return nil
}
