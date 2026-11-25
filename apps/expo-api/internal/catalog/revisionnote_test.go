package catalog

import "testing"

func TestNormalizeNoteRejectsControlAndFormatCharacters(t *testing.T) {
	for name, note := range map[string]string{
		"control":                "bell\x07",
		"invalid utf-8":          "a\xffb",
		"left-to-right embed":    "a\u202ab",
		"right-to-left embed":    "a\u202bb",
		"pop directional":        "a\u202cb",
		"left-to-right override": "a\u202db",
		"right-to-left override": "a\u202eb",
		"left-to-right isolate":  "a\u2066b",
		"right-to-left isolate":  "a\u2067b",
		"first strong isolate":   "a\u2068b",
		"pop isolate":            "a\u2069b",
		"zero width space":       "a\u200bb",
		"zero width non-joiner":  "a\u200cb",
		"zero width joiner":      "a\u200db",
		"left-to-right mark":     "a\u200eb",
		"right-to-left mark":     "a\u200fb",
		"word joiner":            "a\u2060b",
		"byte order mark":        "a\ufeffb",
		"override in the middle": "fine text \u202e txet",
	} {
		if _, err := normalizeNote(note); err == nil {
			t.Errorf("%s: accepted %q", name, note)
		}
	}
	for name, note := range map[string]string{
		"plain":       "Fixed the belts",
		"accents":     "d\u00e9j\u00e0 vu, na\u00efve",
		"newline tab": "line one\n\tline two",
		"non latin":   "\u30d9\u30eb\u30c8 \u4fee\u6b63",
	} {
		if got, err := normalizeNote(note); err != nil || got != note {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
}
