package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

// Store keeps the retained artifacts: the copies the runner serves to the API
// until the API deletes them or their TTL expires. Inputs arrive already
// validated by Inbox.
type Store struct {
	path string
	ttl  time.Duration
}

// retainedFile is one file published into the store.
type retainedFile struct {
	relative string
	size     int64
	sha256   string
}

func NewStore(path string, ttl time.Duration) (*Store, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact directory %q: %w", path, err)
	}
	return &Store{path: path, ttl: ttl}, nil
}

func (s *Store) Ready() error { return writableDir(s.path) }

// RetainPreview copies a validated inbox image into the store and checks the
// copy's PNG chunks again before publishing it, so the bytes served are the
// bytes checked.
func (s *Store) RetainPreview(ctx context.Context, image *PreviewImage) (jobs.Artifact, error) {
	file, err := s.retain(ctx, image.JobID, bridge.PreviewImageFile,
		func(w io.Writer) error {
			written, err := io.Copy(w, io.LimitReader(image.file, image.Size+1))
			if err != nil {
				return fmt.Errorf("copy preview image: %w", err)
			}
			if written != image.Size {
				return errors.New("retained preview image size is outside configured limits")
			}
			return nil
		},
		func(retained *os.File, size int64) error {
			if err := validatePNGChunks(retained, size); err != nil {
				return fmt.Errorf("validate retained PNG: %w", err)
			}
			return nil
		})
	if err != nil {
		return jobs.Artifact{}, err
	}
	return s.artifact(image.JobID, jobs.PhasePreview, file, "image/png", image.Width, image.Height), nil
}

// RetainAnalysis stores the analysis bytes Inbox.Analysis validated, unchanged.
func (s *Store) RetainAnalysis(ctx context.Context, jobID string, data []byte) (jobs.Artifact, error) {
	file, err := s.retain(ctx, jobID, bridge.AnalysisResultFile,
		func(w io.Writer) error {
			if _, err := w.Write(data); err != nil {
				return fmt.Errorf("write analysis result: %w", err)
			}
			return nil
		}, nil)
	if err != nil {
		return jobs.Artifact{}, err
	}
	return s.artifact(jobID, jobs.PhaseAnalysis, file, "application/json", 0, 0), nil
}

// retain publishes one file atomically: it is written to a temp file in the
// job's directory while being hashed, synced, optionally checked, and only
// then renamed into place.
func (s *Store) retain(
	ctx context.Context, jobID, name string,
	write func(io.Writer) error, check func(*os.File, int64) error,
) (retainedFile, error) {
	directory := filepath.Join(s.path, jobID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return retainedFile{}, fmt.Errorf("create retained artifact directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, "."+name+"-*.tmp")
	if err != nil {
		return retainedFile{}, fmt.Errorf("create retained artifact: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hash := sha256.New()
	counter := &countingWriter{writer: io.MultiWriter(temp, hash)}
	if err := write(counter); err != nil {
		temp.Close()
		return retainedFile{}, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return retainedFile{}, fmt.Errorf("sync retained artifact: %w", err)
	}
	if err := temp.Close(); err != nil {
		return retainedFile{}, fmt.Errorf("close retained artifact: %w", err)
	}
	if check != nil {
		if err := checkFile(tempName, counter.written, check); err != nil {
			return retainedFile{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return retainedFile{}, err
	}
	relative := filepath.Join(jobID, name)
	if err := os.Rename(tempName, filepath.Join(s.path, relative)); err != nil {
		return retainedFile{}, fmt.Errorf("publish retained artifact: %w", err)
	}
	return retainedFile{relative: relative, size: counter.written, sha256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func checkFile(path string, size int64, check func(*os.File, int64) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reopen retained artifact: %w", err)
	}
	defer file.Close()
	return check(file, size)
}

func (s *Store) artifact(jobID string, phase jobs.Phase, file retainedFile, mediaType string, width, height int) jobs.Artifact {
	now := time.Now().UTC()
	return jobs.Artifact{
		JobID: jobID, Phase: phase, RelativePath: file.relative, State: jobs.ArtifactAvailable,
		MediaType: mediaType, Size: file.size, SHA256: file.sha256, Width: width, Height: height,
		CreatedAt: now, ExpiresAt: now.Add(s.ttl),
	}
}

func (s *Store) Path(artifact jobs.Artifact) string {
	return filepath.Join(s.path, artifact.RelativePath)
}

// Delete removes a retained artifact (and its job directory once empty). A
// file that is already gone is not an error.
func (s *Store) Delete(artifact jobs.Artifact) error {
	err := os.Remove(s.Path(artifact))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete retained artifact: %w", err)
	}
	_ = os.Remove(filepath.Dir(s.Path(artifact)))
	return nil
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	count, err := w.writer.Write(data)
	w.written += int64(count)
	return count, err
}

// writableDir checks a directory accepts new files (readiness).
func writableDir(root string) error {
	file, err := os.CreateTemp(root, ".ready-*")
	if err != nil {
		return fmt.Errorf("artifact directory %q is not writable: %w", root, err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return fmt.Errorf("close readiness file: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove readiness file: %w", err)
	}
	return nil
}
