package communitynames

import "testing"

func TestNamesFileIsValid(t *testing.T) {
	entries := Names()
	if len(entries) < 40 {
		t.Fatalf("only %d community names entries", len(entries))
	}
}

func TestParseRefusesBadLists(t *testing.T) {
	for name, data := range map[string]string{
		"hyphen":    `[{"item":"a-b","names":["x-y"]}]`,
		"uppercase": `[{"item":"a-b","names":["Red"]}]`,
		"own name":  `[{"item":"iron-gear-wheel","names":["iron gear wheel"]}]`,
		"duplicate": `[{"item":"a","names":["x"]},{"item":"b","names":["x"]}]`,
		"no names":  `[{"item":"a","names":[]}]`,
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: Parse accepted %s", name, data)
		}
	}
}

func TestSynonymHoldsNamesWordsAndInternalName(t *testing.T) {
	got := Entry{Item: "automation-science-pack", Names: []string{"red science"}}.Synonym()
	want := []string{"red science", "automation science pack", "automation-science-pack"}
	if len(got) != len(want) {
		t.Fatalf("Synonym = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Synonym = %v, want %v", got, want)
		}
	}
}
