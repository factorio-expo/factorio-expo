package prototypes

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
)

// PickerKind is one of the web icon picker's lists, named after the
// blueprint editor's pickers: icon (blueprint and display-panel icons),
// item (item filters), entity (the main inventory) and recipe, plus fluid
// (fluids alone, offered next to items where a product can be either).
type PickerKind string

const (
	PickerIcon   PickerKind = "icon"
	PickerItem   PickerKind = "item"
	PickerFluid  PickerKind = "fluid"
	PickerEntity PickerKind = "entity"
	PickerRecipe PickerKind = "recipe"
)

// PickerKinds lists every kind in output order.
var PickerKinds = []PickerKind{PickerIcon, PickerItem, PickerFluid, PickerEntity, PickerRecipe}

// PickerGroup is one picker tab: an item group whose rows are its subgroups,
// each a list of rich-text tokens such as "[item=iron-plate]".
type PickerGroup struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

// Picker holds the tabs of every kind.
type Picker map[PickerKind][]PickerGroup

// Item groups the game keeps out of its own inventory and signal pickers.
var pickerHiddenGroups = []string{"environment", "enemies", "effects", "tiles", "other"}

// creativeItems are hidden items the main inventory still offers, in their
// own last tab, as the blueprint editor does.
var creativeItems = []string{
	"loader", "fast-loader", "express-loader", "turbo-loader", "infinity-chest",
	"heat-interface", "infinity-pipe", "electric-energy-interface",
}

const creative = "creative"

type pickerEntry struct {
	token    string
	name     string
	subgroup string
	order    string
}

type orderedName struct {
	group string
	order string
}

type pickerFields struct {
	Subgroup    string          `json:"subgroup"`
	Order       string          `json:"order"`
	Hidden      bool            `json:"hidden"`
	PlaceResult string          `json:"place_result"`
	MainProduct *string         `json:"main_product"`
	Results     json.RawMessage `json:"results"`
	Group       string          `json:"group"`
	Flags       []string        `json:"flags"`
}

// ExtractPicker builds the icon picker lists the way Factorio lays out its
// inventory: tabs are item groups, rows are subgroups, and each level is
// sorted by its order string, then by name. Hidden prototypes are left out.
func ExtractPicker(dump Dump) (Picker, error) {
	groups := map[string]string{creative: "z"}
	if err := eachPrototype(dump, "item-group", func(name string, fields pickerFields) {
		if !slices.Contains(pickerHiddenGroups, name) {
			groups[name] = fields.Order
		}
	}); err != nil {
		return nil, err
	}
	subgroups := map[string]orderedName{creative: {group: creative, order: "z"}}
	if err := eachPrototype(dump, "item-subgroup", func(name string, fields pickerFields) {
		subgroups[name] = orderedName{group: fields.Group, order: fields.Order}
	}); err != nil {
		return nil, err
	}

	entities := make(map[string]bool)
	itemFields := make(map[string]pickerFields)
	for prototypeType, named := range dump {
		for name, encoded := range named {
			var prototype rawPrototype
			if err := json.Unmarshal(encoded, &prototype); err != nil {
				return nil, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}
			_, isItem := prototype["stack_size"]
			isEntity := isEntityPrototype(prototypeType, prototype)
			if !isItem && !isEntity {
				continue
			}
			var fields pickerFields
			if err := json.Unmarshal(encoded, &fields); err != nil {
				return nil, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}
			// Only player-built entities go in blueprints; seeds place
			// plants, which do not.
			if isEntity && slices.Contains(fields.Flags, "player-creation") {
				entities[name] = true
			}
			// Every item has a stack size; equipment shares its item's name
			// but has none.
			if isItem {
				itemFields[name] = fields
			}
		}
	}

	entries := make(map[PickerKind][]pickerEntry)
	add := func(kind PickerKind, prefix, name, subgroup, order string) {
		entries[kind] = append(entries[kind], pickerEntry{
			token: "[" + prefix + "=" + name + "]", name: name, subgroup: subgroup, order: order,
		})
	}

	for name, fields := range itemFields {
		if index := slices.Index(creativeItems, name); index >= 0 {
			if entities[fields.PlaceResult] {
				add(PickerEntity, "entity", fields.PlaceResult, creative, fmt.Sprintf("%02d", index))
			}
			continue
		}
		if fields.Hidden {
			continue
		}
		add(PickerIcon, "item", name, fields.Subgroup, fields.Order)
		add(PickerItem, "item", name, fields.Subgroup, fields.Order)
		// Tiles are left out: blueprint search counts entities only.
		if entities[fields.PlaceResult] {
			add(PickerEntity, "entity", fields.PlaceResult, fields.Subgroup, fields.Order)
		}
	}

	fluids := make(map[string]pickerFields)
	if err := eachPrototype(dump, "fluid", func(name string, fields pickerFields) {
		fluids[name] = fields
		if !fields.Hidden {
			add(PickerIcon, "fluid", name, cmp.Or(fields.Subgroup, "fluid"), fields.Order)
			add(PickerFluid, "fluid", name, cmp.Or(fields.Subgroup, "fluid"), fields.Order)
		}
	}); err != nil {
		return nil, err
	}
	// Planets and other space locations sit in the space tab's last row
	// (the "planets" subgroup).
	for _, prototypeType := range []string{"virtual-signal", "planet", "space-location"} {
		if err := eachPrototype(dump, prototypeType, func(name string, fields pickerFields) {
			if !fields.Hidden {
				add(PickerIcon, prototypeType, name, fields.Subgroup, fields.Order)
			}
		}); err != nil {
			return nil, err
		}
	}

	var recipeErr error
	if err := eachPrototype(dump, "recipe", func(name string, fields pickerFields) {
		if fields.Hidden || recipeErr != nil {
			return
		}
		// Recipes with their own place in the inventory are also icons, unless
		// an item or fluid of the same name already shows the picture.
		_, isItemName := itemFields[name]
		_, isFluidName := fluids[name]
		if fields.Subgroup != "" && !isItemName && !isFluidName {
			add(PickerIcon, "recipe", name, fields.Subgroup, fields.Order)
		}
		subgroup, order := fields.Subgroup, fields.Order
		if subgroup == "" || order == "" {
			product, err := mainProduct(fields)
			if err != nil {
				recipeErr = fmt.Errorf("decode recipe %q results: %w", name, err)
				return
			}
			var productFields pickerFields
			if product.Type == "fluid" {
				productFields = fluids[product.Name]
				productFields.Subgroup = cmp.Or(productFields.Subgroup, "fluid")
			} else {
				productFields = itemFields[product.Name]
			}
			subgroup = cmp.Or(subgroup, productFields.Subgroup)
			order = cmp.Or(order, productFields.Order)
		}
		add(PickerRecipe, "recipe", name, subgroup, order)
	}); err != nil {
		return nil, err
	}
	if recipeErr != nil {
		return nil, recipeErr
	}

	picker := make(Picker, len(PickerKinds))
	for _, kind := range PickerKinds {
		picker[kind] = layout(entries[kind], groups, subgroups)
	}
	return picker, nil
}

// mainProduct is the product a recipe takes its inventory place from: its
// main_product, or its only result. An empty main_product means none.
func mainProduct(fields pickerFields) (recipeResult, error) {
	results, err := recipeResults(fields.Results)
	if err != nil {
		return recipeResult{}, err
	}
	if fields.MainProduct != nil {
		for _, result := range results {
			if result.Name == *fields.MainProduct {
				return result, nil
			}
		}
		return recipeResult{}, nil
	}
	if len(results) == 1 {
		return results[0], nil
	}
	return recipeResult{}, nil
}

// recipeResults decodes a recipe's results; the dump writes an empty list as
// {} and may write a list as an object keyed by index.
func recipeResults(encoded json.RawMessage) ([]recipeResult, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	var results []recipeResult
	if json.Unmarshal(encoded, &results) == nil {
		return results, nil
	}
	var keyed map[string]recipeResult
	if err := json.Unmarshal(encoded, &keyed); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(keyed))
	for key := range keyed {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		results = append(results, keyed[key])
	}
	return results, nil
}

// layout sorts entries into tabs and rows, dropping entries whose subgroup
// or group is unknown or hidden and any token already placed.
func layout(entries []pickerEntry, groups map[string]string, subgroups map[string]orderedName) []PickerGroup {
	byOrder := func(aOrder, aName, bOrder, bName string) int {
		return cmp.Or(cmp.Compare(aOrder, bOrder), cmp.Compare(aName, bName))
	}
	slices.SortFunc(entries, func(a, b pickerEntry) int {
		subA, subB := subgroups[a.subgroup], subgroups[b.subgroup]
		return cmp.Or(
			byOrder(groups[subA.group], subA.group, groups[subB.group], subB.group),
			byOrder(subA.order, a.subgroup, subB.order, b.subgroup),
			byOrder(a.order, a.name, b.order, b.name),
			cmp.Compare(a.token, b.token),
		)
	})
	var out []PickerGroup
	seen := make(map[string]bool)
	lastSubgroup := ""
	for _, entry := range entries {
		subgroup, known := subgroups[entry.subgroup]
		if _, shown := groups[subgroup.group]; !known || !shown || seen[entry.token] {
			continue
		}
		seen[entry.token] = true
		if len(out) == 0 || out[len(out)-1].Name != subgroup.group {
			out = append(out, PickerGroup{Name: subgroup.group})
			lastSubgroup = ""
		}
		tab := &out[len(out)-1]
		if entry.subgroup != lastSubgroup {
			tab.Rows = append(tab.Rows, nil)
			lastSubgroup = entry.subgroup
		}
		tab.Rows[len(tab.Rows)-1] = append(tab.Rows[len(tab.Rows)-1], entry.token)
	}
	return out
}

// eachPrototype decodes the picker fields of every prototype of one type, in
// name order.
func eachPrototype(dump Dump, prototypeType string, visit func(string, pickerFields)) error {
	named := dump[prototypeType]
	names := make([]string, 0, len(named))
	for name := range named {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		var fields pickerFields
		if err := json.Unmarshal(named[name], &fields); err != nil {
			return fmt.Errorf("decode %s %q: %w", prototypeType, name, err)
		}
		visit(name, fields)
	}
	return nil
}
