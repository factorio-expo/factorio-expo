package catalog

import (
	"reflect"
	"testing"

	"factorio-expo/apps/expo-api/internal/blueprint"
)

func TestSuggestTagsScoresRules(t *testing.T) {
	t.Parallel()
	rules := []tagSuggestionRule{
		{tag: "a", kind: "entity", name: "drill", minCount: 4, points: 2},
		{tag: "a", kind: "recipe", name: "plate", minCount: 1, points: 1},
		{tag: "b", kind: "entity", name: "drill", minCount: 1, points: 1},
		{tag: "b", kind: "recipe", name: "gear", minCount: 3, points: 1},
		{tag: "c", kind: "entity", name: "furnace", minCount: 10, points: 5},
	}
	summary := blueprint.Summary{
		// Qualities sum: 3 normal + 1 rare drills reach the minimum of 4.
		Entities: []blueprint.EntityStat{
			{Name: "drill", Quality: "normal", Count: 3},
			{Name: "drill", Quality: "rare", Count: 1},
			{Name: "furnace", Quality: "normal", Count: 9},
		},
		Recipes: []blueprint.RecipeStat{
			{Name: "plate", Quality: "normal", Count: 1},
			{Name: "gear", Quality: "normal", Count: 3},
		},
	}
	// a: 2+1, b: 1+1, c: 9 furnaces miss the minimum of 10.
	if got := suggestTags(rules, summary, 1); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("suggestTags = %v, want [a b]", got)
	}
	if got := suggestTags(rules, summary, 3); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("min score 3: suggestTags = %v, want [a]", got)
	}
	if got := suggestTags(rules, blueprint.Summary{}, 1); got == nil || len(got) != 0 {
		t.Fatalf("empty blueprint: suggestTags = %#v, want an empty list", got)
	}
}

func TestSuggestTagsKeepsTiesWithTheLast(t *testing.T) {
	t.Parallel()
	var rules []tagSuggestionRule
	for _, tag := range []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8"} {
		rules = append(rules, tagSuggestionRule{tag: tag, group: tag, kind: "entity", name: "lamp", minCount: 1, points: 1})
	}
	rules = append(rules, tagSuggestionRule{tag: "t5", group: "t5", kind: "entity", name: "pole", minCount: 1, points: 1})
	summary := blueprint.Summary{Entities: []blueprint.EntityStat{
		{Name: "lamp", Quality: "normal", Count: 1},
		{Name: "pole", Quality: "normal", Count: 1},
	}}
	// t5 scores 2 and leads; t1-t8 tie at 1 for the last places, so all follow in rule order.
	want := []string{"t5", "t1", "t2", "t3", "t4", "t6", "t7", "t8"}
	if got := suggestTags(rules, summary, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestTags = %v, want %v", got, want)
	}

	for _, tag := range []string{"t1", "t2", "t3", "t4", "t6"} {
		rules = append(rules, tagSuggestionRule{tag: tag, group: tag, kind: "entity", name: "pole", minCount: 1, points: 1})
	}
	// Six tags score 2 and lead; t7 and t8 (1) fill the last two places.
	want = []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8"}
	if got := suggestTags(rules, summary, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestTags = %v, want %v", got, want)
	}
}

func TestSuggestTagsHonoursMaxCount(t *testing.T) {
	t.Parallel()
	four := int32(4)
	rules := []tagSuggestionRule{
		{tag: "small", kind: "entity", name: "thruster", minCount: 1, maxCount: &four, points: 2},
		{tag: "fast", kind: "entity", name: "thruster", minCount: 5, points: 2},
	}
	for count, want := range map[int32][]string{
		0: {},
		4: {"small"},
		5: {"fast"},
	} {
		summary := blueprint.Summary{Entities: []blueprint.EntityStat{{Name: "thruster", Quality: "normal", Count: count}}}
		if got := suggestTags(rules, summary, 2); !reflect.DeepEqual(got, want) {
			t.Fatalf("%d thrusters: suggestTags = %v, want %v", count, got, want)
		}
	}
}

func TestSuggestTagsCapsEachGroup(t *testing.T) {
	t.Parallel()
	rules := []tagSuggestionRule{
		{tag: "turrets", group: "defense", kind: "entity", name: "turret", minCount: 1, points: 5},
		{tag: "walls", group: "defense", kind: "entity", name: "wall", minCount: 1, points: 4},
		{tag: "lasers", group: "defense", kind: "entity", name: "laser", minCount: 1, points: 3},
		{tag: "mines", group: "defense", kind: "entity", name: "mine", minCount: 1, points: 2},
		{tag: "large-ship", group: "ships", kind: "entity", name: "hub", minCount: 1, points: 2},
	}
	summary := blueprint.Summary{Entities: []blueprint.EntityStat{
		{Name: "turret", Quality: "normal", Count: 1},
		{Name: "wall", Quality: "normal", Count: 1},
		{Name: "laser", Quality: "normal", Count: 1},
		{Name: "mine", Quality: "normal", Count: 1},
		{Name: "hub", Quality: "normal", Count: 1},
	}}
	// Defense keeps its best three; the ship tag has a slot of its own.
	want := []string{"turrets", "walls", "lasers", "large-ship"}
	if got := suggestTags(rules, summary, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestTags = %v, want %v", got, want)
	}
}

func TestSuggestTagsCountsQuality(t *testing.T) {
	t.Parallel()
	rules := []tagSuggestionRule{
		{tag: "epic-quality", group: "quality", kind: "quality", name: "epic", minCount: 4, points: 2},
		{tag: "rare-quality", group: "quality", kind: "quality", name: "rare", minCount: 4, points: 2},
	}
	summary := blueprint.Summary{Entities: []blueprint.EntityStat{
		{Name: "lab", Quality: "epic", Count: 3},
		{Name: "assembling-machine-3", Quality: "epic", Count: 2},
		{Name: "lab", Quality: "rare", Count: 3},
	}}
	if got := suggestTags(rules, summary, 2); !reflect.DeepEqual(got, []string{"epic-quality"}) {
		t.Fatalf("suggestTags = %v, want [epic-quality]", got)
	}
}
