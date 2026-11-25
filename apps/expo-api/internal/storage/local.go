package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Local stores objects as files under a root directory (in compose, a host
// bind mount). Put writes a temp file in the target directory, syncs it and
// renames it into place, so a crash never leaves a partial object.
type Local struct {
	root string
}

// NewLocal opens root, creating it if needed, and proves it is writable, so a
// misconfigured mount fails at startup instead of at the first import.
func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, errors.New("storage: root directory is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create root: %w", err)
	}
	probe, err := os.CreateTemp(root, ".probe-*")
	if err != nil {
		return nil, fmt.Errorf("storage: root %s is not writable: %w", root, err)
	}
	name := probe.Name()
	probe.Close()
	if err := os.Remove(name); err != nil {
		return nil, fmt.Errorf("storage: remove probe: %w", err)
	}
	return &Local{root: root}, nil
}

func (l *Local) path(key string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}

func (l *Local) Put(ctx context.Context, key string, body io.Reader) error {
	target, err := l.path(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: create directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".put-*")
	if err != nil {
		return fmt.Errorf("storage: create temp file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			temp.Close()
			os.Remove(temp.Name())
		}
	}()
	if _, err := io.Copy(temp, contextReader{ctx, body}); err != nil {
		return fmt.Errorf("storage: write %s: %w", key, err)
	}
	if err := temp.Chmod(0o644); err != nil {
		return fmt.Errorf("storage: chmod %s: %w", key, err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("storage: sync %s: %w", key, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("storage: close %s: %w", key, err)
	}
	if err := os.Rename(temp.Name(), target); err != nil {
		return fmt.Errorf("storage: publish %s: %w", key, err)
	}
	committed = true
	return nil
}

func (l *Local) Open(_ context.Context, key string) (Object, error) {
	target, err := l.path(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", key, err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("storage: stat %s: %w", key, err)
	}
	return localObject{File: file, info: Info{Size: stat.Size(), ModTime: stat.ModTime()}}, nil
}

func (l *Local) Stat(_ context.Context, key string) (Info, error) {
	target, err := l.path(key)
	if err != nil {
		return Info{}, err
	}
	stat, err := os.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, ErrNotFound
	} else if err != nil {
		return Info{}, fmt.Errorf("storage: stat %s: %w", key, err)
	}
	return Info{Size: stat.Size(), ModTime: stat.ModTime()}, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	target, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

func (l *Local) Walk(ctx context.Context, prefix string, fn func(key string, info Info) error) error {
	dir, err := l.path(strings.TrimSuffix(prefix, "/"))
	if err != nil {
		return err
	}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// Put's temporary files start with a dot.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		relative, err := filepath.Rel(l.root, path)
		if err != nil {
			return err
		}
		stat, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return fn(filepath.ToSlash(relative), Info{Size: stat.Size(), ModTime: stat.ModTime()})
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: walk %s: %w", prefix, err)
	}
	return nil
}

type localObject struct {
	*os.File
	info Info
}

func (o localObject) Info() Info { return o.info }

// contextReader stops a long copy once ctx is canceled.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
