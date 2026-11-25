package accountdeletion

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLedgerRecordsAndReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "deleted-users.log")
	ledger, err := NewLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record([]string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(nil); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record([]string{"cccccccccccc", "aaaaaaaaaaaa"}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"}; !slices.Equal(got, want) {
		t.Fatalf("ledger = %v, want %v", got, want)
	}
}

func TestReadLedgerSkipsJunkAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	if ids, err := ReadLedger(filepath.Join(dir, "none.log")); err != nil || len(ids) != 0 {
		t.Fatalf("missing file = %v, %v; want empty", ids, err)
	}
	path := filepath.Join(dir, "ledger.log")
	if err := os.WriteFile(path, []byte("aaaaaaaaaaaa\n\nnot an id\n  bbbbbbbbbbbb  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}; !slices.Equal(got, want) {
		t.Fatalf("ledger = %v, want %v", got, want)
	}
}

func TestNilLedgerRecordsNothing(t *testing.T) {
	var ledger *Ledger
	if err := ledger.Record([]string{"aaaaaaaaaaaa"}); err != nil {
		t.Fatal(err)
	}
}
