package blueprint

import (
	"encoding/json"
	"factorio-expo/internal/icontoken"
	"fmt"
)

// ExportNode is the stored shape of a node, input for rebuilding an exchange
// string. Leaves and planners carry their self-contained ExchangeString;
// books are re-assembled from their stored metadata and children.
type ExportNode struct {
	Kind           Kind
	Label          string
	Description    string
	Icons          string
	ActiveIndex    *int32
	ExchangeString string
	Children       []ExportNode
}

// EncodeTree rebuilds the exchange string of a node. Books are never stored as
// strings, so this is the on-demand export for them (and works for leaves too).
func EncodeTree(node ExportNode, limits Limits) (string, error) {
	if node.Kind != KindBook {
		return node.ExchangeString, nil
	}
	envelope, _, err := exportEnvelope(node, limits)
	if err != nil {
		return "", err
	}
	return Encode(envelope)
}

// exportEnvelope returns the {"blueprint_book": ...} envelope plus the highest
// packed game version found among the descendant leaves.
func exportEnvelope(node ExportNode, limits Limits) (map[string]json.RawMessage, json.RawMessage, error) {
	children := make([]map[string]json.RawMessage, 0, len(node.Children))
	var version json.RawMessage
	var versionValue uint64
	for index, child := range node.Children {
		var envelope map[string]json.RawMessage
		var childVersion json.RawMessage
		var err error
		key := envelopeKey(child.Kind)
		if child.Kind == KindBook {
			envelope, childVersion, err = exportEnvelope(child, limits)
		} else {
			envelope, childVersion, err = leafEnvelope(child, key, limits)
		}
		if err != nil {
			return nil, nil, err
		}
		withIndex, err := setField(envelope[key], "index", index)
		if err != nil {
			return nil, nil, err
		}
		envelope[key] = withIndex
		children = append(children, envelope)
		var value uint64
		if json.Unmarshal(childVersion, &value) == nil && value > versionValue {
			versionValue, version = value, childVersion
		}
	}

	book := map[string]any{
		"item":        "blueprint-book",
		"label":       node.Label,
		"description": node.Description,
		"blueprints":  children,
	}
	if icons := richTextToIcons(node.Icons); len(icons) > 0 {
		book["icons"] = icons
	}
	if node.ActiveIndex != nil {
		book["active_index"] = *node.ActiveIndex
	}
	if version != nil {
		book["version"] = version
	}
	raw, err := json.Marshal(book)
	if err != nil {
		return nil, nil, fmt.Errorf("encode blueprint book: %w", err)
	}
	return map[string]json.RawMessage{keyBook: raw}, version, nil
}

// envelopeKey is the exchange-string envelope key of a node kind.
func envelopeKey(kind Kind) string {
	switch kind {
	case KindBook:
		return keyBook
	case KindBlueprint:
		return keyBlueprint
	default:
		return string(kind)
	}
}

// leafEnvelope decodes a stored blueprint or planner string, checking it holds
// the expected envelope key.
func leafEnvelope(leaf ExportNode, key string, limits Limits) (map[string]json.RawMessage, json.RawMessage, error) {
	payload, err := Decode(leaf.ExchangeString, limits.MaxDecodedBytes)
	if err != nil {
		return nil, nil, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope[key] == nil {
		return nil, nil, ErrInvalidExchangeString
	}
	var fields struct {
		Version json.RawMessage `json:"version"`
	}
	_ = json.Unmarshal(envelope[key], &fields)
	return envelope, fields.Version, nil
}

func setField(raw json.RawMessage, key string, value any) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, ErrInvalidExchangeString
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	object[key] = encoded
	return json.Marshal(object)
}

type exportIcon struct {
	Signal struct {
		Type    string `json:"type,omitempty"`
		Name    string `json:"name"`
		Quality string `json:"quality,omitempty"`
	} `json:"signal"`
	Index int `json:"index"`
}

// richTextToIcons is the inverse of iconsToRichText.
func richTextToIcons(richText string) []exportIcon {
	var icons []exportIcon
	for _, found := range icontoken.Scan(richText) {
		var entry exportIcon
		if found.Type != "item" {
			entry.Signal.Type = found.Type
		}
		if entry.Signal.Type == "virtual-signal" {
			entry.Signal.Type = "virtual"
		}
		entry.Signal.Name = found.Name
		if found.Quality != "normal" {
			entry.Signal.Quality = found.Quality
		}
		entry.Index = len(icons) + 1
		icons = append(icons, entry)
	}
	return icons
}
