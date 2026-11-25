// Package auth holds the session credential and the account rules: JWT
// issue/verify, signed short-lived flow cookies for the sign-in ceremonies,
// username rules and server key checks. A signed JWT (in an HttpOnly cookie
// for browsers) is the credential; it names a server-side session
// (internal/sessions), so deleting the session revokes it. Accounts have no
// password or email; they sign in with a provider identity or a passkey.
package auth

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

const (
	minUsername = 3
	maxUsername = 32
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

// ErrInvalidUsername is returned for a username that is not a 3-32 character
// URL-safe handle. Mirrors the users.username CHECK.
var ErrInvalidUsername = errors.New("username must be 3-32 characters: letters, digits, '_' or '-'")

// ValidateUsername checks a trimmed username against the handle rules.
func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

// SuggestUsername turns a provider's display or login name into a valid
// username, or "" when too little of it survives. Spaces, dots and other
// ASCII punctuation become '_', other characters are dropped, runs of '_'
// collapse, and the result is cut to 32 characters.
func SuggestUsername(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)), r == '-':
			b.WriteRune(r)
		case r < unicode.MaxASCII && (unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)):
			b.WriteRune('_')
		}
	}
	suggestion := b.String()
	for strings.Contains(suggestion, "__") {
		suggestion = strings.ReplaceAll(suggestion, "__", "_")
	}
	suggestion = strings.Trim(suggestion, "_-")
	if len(suggestion) > maxUsername {
		suggestion = strings.TrimRight(suggestion[:maxUsername], "_-")
	}
	if len(suggestion) < minUsername || ValidateUsername(suggestion) != nil {
		return ""
	}
	return suggestion
}
