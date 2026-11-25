// Package shortid generates and checks the random 12-character ids that key
// every catalog row and appear in public URLs (`/e/{id}`).
package shortid

import (
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// Alphabet leaves out nanoid's "-" and "_" so an id selects with a
// double-click and never starts with a dash. 62^12 is about 71 bits.
const (
	Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	Length   = 12
)

// New returns a fresh random id. It panics only if the system random source
// fails, which leaves nothing sensible to do.
func New() string {
	return gonanoid.MustGenerate(Alphabet, Length)
}

// Valid reports whether value has the shape of an id. Callers treat an
// invalid id as "not found", because it can never name a row.
func Valid(value string) bool {
	if len(value) != Length {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !('0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z') {
			return false
		}
	}
	return true
}
