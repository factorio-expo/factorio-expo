package accounts

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

var codeShape = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){4}-[A-HJ-NP-Z2-9]$`)

func TestNewRecoveryCodeHasShapeEntropyAndMatchingHash(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		code, hash, err := newRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		if !codeShape.MatchString(code) {
			t.Fatalf("code %q is not 26 characters in groups of five", code)
		}
		if len(hash) != 32 {
			t.Fatalf("hash is %d bytes, want 32", len(hash))
		}
		if seen[code] {
			t.Fatalf("code %q repeated", code)
		}
		seen[code] = true
		// What the person types hashes to what was stored.
		parsed, ok := parseRecoveryCode(code)
		if !ok || !bytes.Equal(parsed, hash) {
			t.Fatalf("code %q does not parse back to its hash", code)
		}
	}
}

func TestParseRecoveryCodeIgnoresCaseDashesAndSpaces(t *testing.T) {
	code, hash, err := newRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	for name, typed := range map[string]string{
		"as shown":      code,
		"lower case":    strings.ToLower(code),
		"no dashes":     strings.ReplaceAll(code, "-", ""),
		"spaces":        strings.ReplaceAll(code, "-", " "),
		"padded":        "  " + code + "\n",
		"mixed":         strings.ToLower(code[:12]) + code[12:],
		"dashes inside": strings.ReplaceAll(code, "-", "--"),
	} {
		got, ok := parseRecoveryCode(typed)
		if !ok || !bytes.Equal(got, hash) {
			t.Errorf("%s: %q did not parse to the code's hash", name, typed)
		}
	}
}

func TestParseRecoveryCodeRejectsWhatWeNeverIssue(t *testing.T) {
	code, _, err := newRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	plain := strings.ReplaceAll(code, "-", "")
	for name, typed := range map[string]string{
		"empty":            "",
		"too short":        plain[:25],
		"too long":         plain + "A",
		"look-alike zero":  plain[:25] + "0",
		"look-alike one":   plain[:25] + "1",
		"look-alike I":     plain[:25] + "I",
		"look-alike O":     plain[:25] + "O",
		"punctuation":      plain[:25] + "!",
		"unicode":          plain[:25] + "é",
		"a username":       "some-user-name",
		"hex of the bytes": strings.Repeat("ab", 13),
		// The last character keeps 3 of its 5 bits; flipping the other 2
		// spells the same 128 bits differently, which must not be a second
		// valid code.
		"non-canonical end": plain[:25] + flipIgnoredBit(plain[25]),
	} {
		if _, ok := parseRecoveryCode(typed); ok {
			t.Errorf("%s: %q was accepted", name, typed)
		}
	}
}

// flipIgnoredBit returns the alphabet character whose index differs from c's
// in the lowest bit.
func flipIgnoredBit(c byte) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	return string(alphabet[strings.IndexByte(alphabet, c)^1])
}
