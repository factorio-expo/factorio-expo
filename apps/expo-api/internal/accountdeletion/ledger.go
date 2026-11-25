package accountdeletion

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"factorio-expo/internal/shortid"
)

// Ledger is the append-only list of hard-deleted account ids (one per line, no
// personal data). It lives outside the database, so restoring an older dump
// cannot bring a deleted account back for good: ReplayDeleted deletes every id
// in it again (docs/admin.md#account-deletion). A nil *Ledger records nothing.
type Ledger struct {
	path string
	mu   sync.Mutex
}

// NewLedger opens (creating it and its directory when missing) the ledger file
// at path. It fails when the file cannot be appended to.
func NewLedger(path string) (*Ledger, error) {
	if path == "" {
		return nil, errors.New("ledger path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create ledger directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close ledger: %w", err)
	}
	return &Ledger{path: path}, nil
}

// Record appends ids and syncs the file before returning, so an id that was
// hard-deleted is on disk by the time the sweep moves on.
func (l *Ledger) Record(ids []string) error {
	if l == nil || len(ids) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	if _, err := file.WriteString(strings.Join(ids, "\n") + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write ledger: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync ledger: %w", err)
	}
	return file.Close()
}

// ReadLedger returns the valid, distinct ids of the ledger file at path in the
// order they were recorded (a missing file is an empty ledger; lines that are
// not ids are skipped).
func ReadLedger(path string) ([]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	defer file.Close()
	seen := map[string]bool{}
	var ids []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		id := strings.TrimSpace(scanner.Text())
		if shortid.Valid(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	return ids, nil
}
