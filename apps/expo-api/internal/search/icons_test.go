package search

import (
	"reflect"
	"testing"
)

func TestExtractIconTokens(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantIcons []string
		wantText  string
	}{
		{
			name:      "icon only",
			input:     "[fluid=water]",
			wantIcons: []string{"[fluid=water]"},
			wantText:  "",
		},
		{
			name:      "icon then text",
			input:     "[fluid=water] cool tank",
			wantIcons: []string{"[fluid=water]"},
			wantText:  "cool tank",
		},
		{
			name:      "text then icon",
			input:     "cool tank [fluid=water]",
			wantIcons: []string{"[fluid=water]"},
			wantText:  "cool tank",
		},
		{
			name:      "multiple icons",
			input:     "[item=iron-plate][fluid=water] smelting setup",
			wantIcons: []string{"[item=iron-plate]", "[fluid=water]"},
			wantText:  "smelting setup",
		},
		{
			name:      "no icons",
			input:     "just a name",
			wantIcons: nil,
			wantText:  "just a name",
		},
		{
			name:      "unclosed bracket kept as text",
			input:     "weird [not-closed name",
			wantIcons: nil,
			wantText:  "weird [not-closed name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			icons, text := extractIconTokens(tc.input)
			if !reflect.DeepEqual(icons, tc.wantIcons) {
				t.Errorf("icons = %#v, want %#v", icons, tc.wantIcons)
			}
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

func TestSplitIconTokens(t *testing.T) {
	got := splitIconTokens("[item=iron-plate][fluid=water,quality=rare]")
	// Fluids have no quality: a fluid token is indexed without one.
	want := []string{"[item=iron-plate]", "[fluid=water]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitIconTokens = %#v, want %#v", got, want)
	}
}

func TestParseQueryText(t *testing.T) {
	icons, text := ParseQueryText("[fluid=water] cooling tower")
	if !reflect.DeepEqual(icons, []string{"[fluid=water]"}) {
		t.Errorf("icons = %#v", icons)
	}
	if text != "cooling tower" {
		t.Errorf("text = %q", text)
	}
}

func TestSanitizeToken(t *testing.T) {
	cases := map[string]string{
		"iron-gear-wheel": "iron_gear_wheel",
		"iron_plate":      "iron_plate",
		"water":           "water",
		"foo.bar[baz]":    "foo_bar_baz_",
	}
	for input, want := range cases {
		if got := sanitizeToken(input); got != want {
			t.Errorf("sanitizeToken(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRateFieldName(t *testing.T) {
	if got := rateFieldName("out", "", "iron-gear-wheel"); got != "rate_out_iron_gear_wheel" {
		t.Errorf("rateFieldName = %q", got)
	}
	if got := rateFieldName("in", "", "water"); got != "rate_in_water" {
		t.Errorf("rateFieldName = %q", got)
	}
	if got := rateFieldName("out", "legendary", "iron-gear-wheel"); got != "rate_out_legendary_iron_gear_wheel" {
		t.Errorf("quality rateFieldName = %q", got)
	}
}

func TestStripEmbeddedIcons(t *testing.T) {
	text, icons := stripEmbeddedIcons("[item=agricultural-science-pack]Gleba science [item=turbo-transport-belt]+")
	if text != "Gleba science +" {
		t.Errorf("text = %q, want %q", text, "Gleba science +")
	}
	want := []string{"[item=agricultural-science-pack]", "[item=turbo-transport-belt]"}
	if !reflect.DeepEqual(icons, want) {
		t.Errorf("icons = %#v, want %#v", icons, want)
	}
}

func TestStripEmbeddedIconsDropsFormattingTags(t *testing.T) {
	text, icons := stripEmbeddedIcons("[color=red]All Items:[/color] [font=default-bold]bold[/font][space-age][item=iron-plate]")
	if text != "All Items: bold" {
		t.Errorf("text = %q, want %q", text, "All Items: bold")
	}
	if want := []string{"[item=iron-plate]"}; !reflect.DeepEqual(icons, want) {
		t.Errorf("icons = %#v, want %#v", icons, want)
	}
}

func TestMergeIconLists(t *testing.T) {
	got := mergeIconLists(
		[]string{"[item=rocket-silo]"},
		[]string{"[item=agricultural-science-pack]", "[item=rocket-silo]"},
		nil,
		[]string{"[item=turbo-transport-belt]"},
	)
	want := []string{"[item=rocket-silo]", "[item=agricultural-science-pack]", "[item=turbo-transport-belt]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeIconLists = %#v, want %#v", got, want)
	}
}
