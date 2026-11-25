// Package storage keeps the API's durable files (preview images) behind one
// small interface, so the backend can change without touching the catalog.
// Keys are internal and never appear in public links. Objects are immutable:
// a key is written once and only ever deleted, never rewritten in place.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrNotFound is returned for a key that holds no object.
var ErrNotFound = errors.New("storage: object not found")

// Info describes a stored object.
type Info struct {
	Size    int64
	ModTime time.Time
}

// Object is an open stored object; the caller closes it.
type Object interface {
	io.ReadSeekCloser
	Info() Info
}

// Store is a blob store keyed by slash-separated relative keys.
type Store interface {
	// Put stores body under key atomically: readers see the whole object or
	// none. An existing object is replaced (callers write a key once).
	Put(ctx context.Context, key string, body io.Reader) error
	Open(ctx context.Context, key string) (Object, error)
	Stat(ctx context.Context, key string) (Info, error)
	// Delete removes key; a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// Walk calls fn for every object whose key starts with prefix (a
	// directory, such as "previews/"), in no particular order. Objects being
	// written are not listed.
	Walk(ctx context.Context, prefix string, fn func(key string, info Info) error) error
}

// validKey accepts relative keys of [a-z0-9._-] segments separated by "/",
// with no empty, "." or ".." segments, so a key can never leave the store.
func validKey(key string) error {
	if key == "" || len(key) > 512 {
		return fmt.Errorf("storage: invalid key %q", key)
	}
	for segment := range strings.SplitSeq(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("storage: invalid key %q", key)
		}
		for _, r := range segment {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
			if !ok {
				return fmt.Errorf("storage: invalid key %q", key)
			}
		}
	}
	return nil
}
