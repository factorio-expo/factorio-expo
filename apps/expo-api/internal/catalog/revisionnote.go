package catalog

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// normalizeNote trims a revision note and checks it: at most MaxRevisionNoteRunes
// characters, plain text (line breaks and tabs only as controls, and no format
// characters: bidirectional overrides and isolates, zero-width and joiner
// characters, the byte order mark, which can disguise or reorder what a note
// says). An empty note is no note.
func normalizeNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxRevisionNoteRunes || !utf8.ValidString(note) {
		return "", ErrInvalidRevisionNote
	}
	for _, r := range note {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", ErrInvalidRevisionNote
		}
		// Category Cf covers the bidirectional overrides and isolates
		// (U+202A-202E, U+2066-2069), the zero-width and joiner characters
		// (U+200B-200F, U+2060) and the byte order mark (U+FEFF).
		if unicode.Is(unicode.Cf, r) {
			return "", ErrInvalidRevisionNote
		}
	}
	return note, nil
}
