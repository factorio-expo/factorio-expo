package httpapi

import "testing"

func TestStreamSlotsCapPerClientAndRelease(t *testing.T) {
	t.Parallel()
	slots := newStreamSlots()
	for i := range maxStreamsPerClient {
		if !slots.acquire("a") {
			t.Fatalf("stream %d refused below the cap", i+1)
		}
	}
	if slots.acquire("a") {
		t.Fatal("a client may not open more than maxStreamsPerClient streams")
	}
	if !slots.acquire("b") {
		t.Fatal("another client is not affected by a's cap")
	}
	slots.release("a")
	if !slots.acquire("a") {
		t.Fatal("a released slot is free again")
	}
	for range maxStreamsPerClient {
		slots.release("a")
	}
	slots.release("b")
	if slots.total != 0 || len(slots.perClient) != 0 {
		t.Fatalf("leaked slots: total %d, clients %v", slots.total, slots.perClient)
	}

	var off *streamSlots
	if !off.acquire("a") {
		t.Fatal("nil slots (rate limits off) allow everything")
	}
	off.release("a")
}
