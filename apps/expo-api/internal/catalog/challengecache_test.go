package catalog

import (
	"testing"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

func TestScoreCacheTTL(t *testing.T) {
	row := db.Challenge{ID: "c1"}
	now := time.Now()
	scored := []scoredEntry{{}}

	off := scoreCache{}
	off.put(row, now, scored)
	if _, ok := off.get(row, now); ok {
		t.Fatal("a zero TTL cached a board")
	}

	cache := scoreCache{ttl: 10 * time.Second}
	cache.put(row, now, scored)
	if _, ok := cache.get(row, now.Add(5*time.Second)); !ok {
		t.Fatal("a board within its TTL was not reused")
	}
	if _, ok := cache.get(row, now.Add(10*time.Second)); ok {
		t.Fatal("a board past its TTL was reused")
	}
	cache.put(row, now, scored)
	cache.drop("c1")
	if _, ok := cache.get(row, now); ok {
		t.Fatal("a dropped board was reused")
	}
	cache.put(row, now, scored)
	cache.dropAll()
	if _, ok := cache.get(row, now); ok {
		t.Fatal("dropAll left a board")
	}
}
