package search

import (
	"encoding/json"
	"sort"
)

// entityCount is one row of blueprint_stats.entities.
type entityCount struct {
	Name    string `json:"name"`
	Quality string `json:"quality"`
}

func decodeEntityNames(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var rows []entityCount
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.Name]; exists {
			continue
		}
		seen[row.Name] = struct{}{}
		names = append(names, row.Name)
	}
	return names, nil
}

type recipeFact struct {
	Name    string `json:"name"`
	Quality string `json:"quality"`
}

func decodeRecipes(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var facts []recipeFact
	if err := json.Unmarshal(raw, &facts); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		if _, exists := seen[fact.Name]; exists {
			continue
		}
		seen[fact.Name] = struct{}{}
		names = append(names, fact.Name)
	}
	return names, nil
}

// decodeQualities merges the structural quality tiers of one leaf with the
// production tiers already decoded from its snapshot.
func decodeQualities(entities, recipes []byte, production []string) ([]string, error) {
	qualities := newStringSet()
	var entityFacts []entityCount
	if len(entities) > 0 {
		if err := json.Unmarshal(entities, &entityFacts); err != nil {
			return nil, err
		}
	}
	for _, fact := range entityFacts {
		qualities.add(fact.Quality)
	}

	if len(recipes) > 0 {
		var recipeFacts []recipeFact
		if err := json.Unmarshal(recipes, &recipeFacts); err != nil {
			return nil, err
		}
		for _, fact := range recipeFacts {
			qualities.add(fact.Quality)
		}
	}
	qualities.addAll(production)
	values := qualities.values()
	sort.Strings(values)
	return values, nil
}

func decodeEntityFacts(raw []byte) ([]string, error) {
	var facts []entityCount
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &facts); err != nil {
			return nil, err
		}
	}
	values := newStringSet()
	for _, fact := range facts {
		quality := fact.Quality
		if quality == "" {
			quality = "normal"
		}
		values.add(qualityFact(quality, fact.Name))
	}
	result := values.values()
	sort.Strings(result)
	return result, nil
}

func decodeRecipeFacts(raw []byte) ([]string, error) {
	var facts []recipeFact
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &facts); err != nil {
			return nil, err
		}
	}
	values := newStringSet()
	for _, fact := range facts {
		quality := fact.Quality
		if quality == "" {
			quality = "normal"
		}
		values.add(qualityFact(quality, fact.Name))
	}
	result := values.values()
	sort.Strings(result)
	return result, nil
}

func qualityFact(quality, name string) string {
	return quality + ":" + name
}

func sortedSetValues(values *stringSet) []string {
	result := values.values()
	sort.Strings(result)
	return result
}
