package search

import (
	"testing"

	"factorio-expo/internal/analysisresult"
)

func TestFootprintSides(t *testing.T) {
	sides := footprintSides{measured: true}
	sides.add(&analysisresult.Footprint{Width: 10, Height: 100}, true)
	sides.add(&analysisresult.Footprint{Width: 50, Height: 40}, true)
	if short, long := sides.short(), sides.long(); short == nil || *short != 40 || long == nil || *long != 100 {
		t.Fatalf("sides = %v, %v; want 40, 100", short, long)
	}

	// One leaf without a current measurement leaves the entry unmeasured.
	sides.add(nil, true)
	if sides.short() != nil || sides.long() != nil {
		t.Fatal("an unmeasured leaf still gave the entry a footprint")
	}
	stale := footprintSides{measured: true}
	stale.add(&analysisresult.Footprint{Width: 3, Height: 3}, false)
	if stale.short() != nil {
		t.Fatal("a stale analysis gave the entry a footprint")
	}
}
