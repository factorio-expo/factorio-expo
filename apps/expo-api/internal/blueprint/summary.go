package blueprint

import "sort"

// Summary is what a whole submission holds, read structurally (no game): the
// root's own label, description and icons (a book's, not its active
// blueprint's), and the entities and recipes of all its leaves together.
type Summary struct {
	Kind        Kind
	Label       string
	Description string
	Icons       string
	GameVersion string
	// BlueprintCount is the number of leaves; a single blueprint is 1.
	BlueprintCount int
	// PlannerCount is the number of upgrade and deconstruction planners.
	PlannerCount int
	EntityCount  int
	// Entities sums every leaf's counts per name and quality.
	Entities []EntityStat
	// Recipes sums every leaf's recipe counts per name and quality.
	Recipes []RecipeStat
	// Compat21 is the submission's Factorio 2.1 compatibility (Compat21).
	Compat21 string
}

// Summarize totals a parsed submission.
func Summarize(root Node) Summary {
	summary := Summary{Kind: root.Kind, GameVersion: GameVersion(root)}
	switch {
	case root.Kind == KindBook:
		summary.Label = root.Book.Label
		summary.Description = root.Book.Description
		summary.Icons = root.Book.Icons
	case root.Kind.IsPlanner():
		summary.Label = root.Planner.Label
		summary.Description = root.Planner.Description
		summary.Icons = root.Planner.Icons
	default:
		summary.Label = root.Blueprint.Label
		summary.Description = root.Blueprint.Description
		summary.Icons = root.Blueprint.Icons
	}

	entities := map[[2]string]int32{}
	recipes := map[[2]string]int32{}
	var visit func(Node)
	visit = func(node Node) {
		if node.Kind == KindBook {
			for _, child := range node.Book.Children {
				visit(child)
			}
			return
		}
		if node.Kind.IsPlanner() {
			summary.PlannerCount++
			return
		}
		leaf := node.Blueprint
		summary.BlueprintCount++
		summary.EntityCount += int(leaf.EntityCount)
		for _, entity := range leaf.Entities {
			entities[[2]string{entity.Name, entity.Quality}] += entity.Count
		}
		for _, recipe := range leaf.Recipes {
			recipes[[2]string{recipe.Name, recipe.Quality}] += recipe.Count
		}
	}
	visit(root)
	summary.Compat21 = Compat21(root)

	summary.Entities = make([]EntityStat, 0, len(entities))
	for key, count := range entities {
		summary.Entities = append(summary.Entities, EntityStat{Name: key[0], Quality: key[1], Count: count})
	}
	sort.Slice(summary.Entities, func(i, j int) bool {
		a, b := summary.Entities[i], summary.Entities[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Quality < b.Quality
	})
	summary.Recipes = make([]RecipeStat, 0, len(recipes))
	for key, count := range recipes {
		summary.Recipes = append(summary.Recipes, RecipeStat{Name: key[0], Quality: key[1], Count: count})
	}
	sort.Slice(summary.Recipes, func(i, j int) bool {
		a, b := summary.Recipes[i], summary.Recipes[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Quality < b.Quality
	})
	return summary
}
