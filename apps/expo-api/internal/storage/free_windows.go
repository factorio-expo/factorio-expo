package storage

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// FreeBytes is the space the caller may still use on the disk that holds
// the store.
func (l *Local) FreeBytes() (int64, error) {
	root, err := windows.UTF16PtrFromString(l.root)
	if err != nil {
		return 0, fmt.Errorf("storage: free space of %s: %w", l.root, err)
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(root, &available, &total, &free); err != nil {
		return 0, fmt.Errorf("storage: free space of %s: %w", l.root, err)
	}
	return int64(available), nil
}
