//go:build unix

package storage

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// FreeBytes is the space unprivileged writers may still use on the disk
// that holds the store.
func (l *Local) FreeBytes() (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(l.root, &stat); err != nil {
		return 0, fmt.Errorf("storage: statfs %s: %w", l.root, err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
