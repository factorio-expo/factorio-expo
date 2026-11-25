package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPutOpenDelete(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(filepath.Join(t.TempDir(), "root"))
	if err != nil {
		t.Fatal(err)
	}
	key := "previews/ab/abc.webp"
	if err := store.Put(ctx, key, strings.NewReader("image")); err != nil {
		t.Fatal(err)
	}
	info, err := store.Stat(ctx, key)
	if err != nil || info.Size != 5 {
		t.Fatalf("stat = %+v, %v", info, err)
	}
	object, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(object)
	object.Close()
	if string(body) != "image" || object.Info().Size != 5 {
		t.Fatalf("read %q, info %+v", body, object.Info())
	}
	entries, _ := os.ReadDir(filepath.Join(store.root, "previews", "ab"))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
	if _, err := store.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("open after delete = %v", err)
	}
	if _, err := store.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat after delete = %v", err)
	}
}

func TestLocalRejectsEscapingKeys(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "../x", "a/../b", "/abs", "a//b", "a\\b", "A.webp", "a/./b"} {
		if err := store.Put(context.Background(), key, strings.NewReader("x")); err == nil {
			t.Errorf("Put(%q) accepted", key)
		}
	}
}

func TestLocalPutFailureLeavesNothing(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, "a/b.webp", strings.NewReader("x")); err == nil {
		t.Fatal("Put with a canceled context succeeded")
	}
	entries, _ := os.ReadDir(filepath.Join(store.root, "a"))
	if len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestLocalFreeBytes(t *testing.T) {
	t.Parallel()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if free, err := store.FreeBytes(); err != nil || free <= 0 {
		t.Fatalf("FreeBytes = %d, %v", free, err)
	}
}

func TestLocalWalk(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(filepath.Join(t.TempDir(), "root"))
	if err != nil {
		t.Fatal(err)
	}
	walk := func(prefix string) map[string]int64 {
		t.Helper()
		found := map[string]int64{}
		if err := store.Walk(ctx, prefix, func(key string, info Info) error {
			found[key] = info.Size
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", prefix, err)
		}
		return found
	}
	if found := walk("previews/"); len(found) != 0 {
		t.Fatalf("walk of a missing directory = %v, want nothing", found)
	}
	for key, body := range map[string]string{
		"previews/ab/one.webp": "1", "previews/cd/two.thumb.webp": "22", "other/three.webp": "333",
	} {
		if err := store.Put(ctx, key, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	// A write in progress (Put's temp file) is not listed.
	if err := os.WriteFile(filepath.Join(store.root, "previews", "ab", ".put-123"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := walk("previews/")
	if len(found) != 2 || found["previews/ab/one.webp"] != 1 || found["previews/cd/two.thumb.webp"] != 2 {
		t.Fatalf("walk = %v, want the two preview files", found)
	}
	if err := store.Walk(ctx, "../", func(string, Info) error { return nil }); err == nil {
		t.Fatal("walk outside the root succeeded")
	}
}
