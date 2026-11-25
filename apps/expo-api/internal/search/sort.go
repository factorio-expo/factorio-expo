package search

import "fmt"

// SortExpression accepts only public sort options, never arbitrary index
// fields. A book's leaves share its timestamps and counts, so the leaf's
// position is the last tiebreak: they list in book order.
func SortExpression(sort string) (string, error) {
	switch sort {
	case "":
		return "updated_at:desc,created_at:desc,position:asc", nil
	case "relevance":
		return "_text_match:desc,updated_at:desc,position:asc", nil
	case "created_at:asc", "created_at:desc":
		return sort + ",position:asc", nil
	case "updated_at:asc", "updated_at:desc",
		"entity_count:asc", "entity_count:desc",
		"leaf_count:asc", "leaf_count:desc",
		"view_count:asc", "view_count:desc":
		return sort + ",created_at:desc,position:asc", nil
	default:
		return "", fmt.Errorf("unsupported search sort %q", sort)
	}
}
