package artifacts

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
	"factorio-expo/internal/analysisresult"
)

// Inbox reads the result files the bridge writes into the shared inbox
// directory, one subdirectory per job (layout in bridge/protocol.go). It only
// reads and validates; keeping a result is Store's job.
type Inbox struct {
	path      string
	maxBytes  int64 // preview image
	maxPixels int64
}

type Marker struct {
	SchemaVersion int               `json:"schema_version"`
	Status        string            `json:"status"`
	Phase         string            `json:"phase"`
	JobID         string            `json:"job_id"`
	ImagePath     string            `json:"image_path"`
	Resolution    bridge.Resolution `json:"resolution"`
	Zoom          float64           `json:"zoom"`
	BoundingBox   json.RawMessage   `json:"bounding_box"`
	SpacePlatform bool              `json:"space_platform"`
	Tick          int64             `json:"tick"`
}

type PhaseFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PreviewImage is a validated preview PNG still in the inbox, open and rewound
// for Store.RetainPreview. The caller closes it.
type PreviewImage struct {
	JobID  string
	Size   int64
	Width  int
	Height int
	file   *os.File
}

func (p *PreviewImage) Close() error { return p.file.Close() }

// Analysis is an analysis result read and validated once. Data is exactly the
// bytes that were checked, for Store.RetainAnalysis; Failure is set when the
// bridge reported the phase as failed.
type Analysis struct {
	Data    []byte
	Failure *PhaseFailure
}

func NewInbox(path string, maxImageBytes, maxPixels int64) (*Inbox, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create inbox directory %q: %w", path, err)
	}
	return &Inbox{path: path, maxBytes: maxImageBytes, maxPixels: maxPixels}, nil
}

func (i *Inbox) Ready() error { return writableDir(i.path) }

func (i *Inbox) file(jobID, name string) string { return filepath.Join(i.path, jobID, name) }

// ClientAlive reports whether the capturing client rewrote its heartbeat file
// within maxAge. The server counts a killed client as connected until its peer
// timeout; the file is written by the client itself, so it goes stale at once.
func (i *Inbox) ClientAlive(maxAge time.Duration) bool {
	info, err := regularFile(filepath.Join(i.path, bridge.ClientHeartbeatFile))
	return err == nil && time.Since(info.ModTime()) <= maxAge
}

// Cleanup removes a job's inbox directory.
func (i *Inbox) Cleanup(jobID string) error {
	return os.RemoveAll(filepath.Join(i.path, jobID))
}

// PreviewMarker reads the preview result marker; ready is false while the
// bridge has not written it yet.
func (i *Inbox) PreviewMarker(jobID string) (Marker, bool, error) {
	path := i.file(jobID, bridge.PreviewResultFile)
	info, err := regularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Marker{}, false, nil
	}
	if err != nil {
		return Marker{}, false, fmt.Errorf("inspect result marker: %w", err)
	}
	if info.Size() > 64<<10 {
		return Marker{}, false, fmt.Errorf("result marker is too large")
	}
	file, err := os.Open(path)
	if err != nil {
		return Marker{}, false, fmt.Errorf("open result marker: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	var marker Marker
	if err := decoder.Decode(&marker); err != nil {
		return Marker{}, false, fmt.Errorf("decode result marker: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Marker{}, false, fmt.Errorf("result marker must contain one JSON value")
	}
	if marker.SchemaVersion != 1 || marker.JobID != jobID || marker.Phase != string(jobs.PhasePreview) ||
		(marker.Status != "succeeded" && marker.Status != "failed") {
		return Marker{}, false, fmt.Errorf("result marker does not match job")
	}
	if marker.Status == "failed" {
		return marker, true, nil
	}
	expectedImage := bridge.RelativePath(jobID, bridge.PreviewImageFile)
	if marker.ImagePath != expectedImage {
		return Marker{}, false, fmt.Errorf("result marker does not match job")
	}
	if marker.Resolution.X < 1 || marker.Resolution.Y < 1 || marker.Zoom <= 0 {
		return Marker{}, false, fmt.Errorf("result marker has invalid render metadata")
	}
	return marker, true, nil
}

// PreviewFailure reads the failure a failed preview marker carries.
func (i *Inbox) PreviewFailure(jobID string) (PhaseFailure, error) {
	file, err := os.Open(i.file(jobID, bridge.PreviewResultFile))
	if err != nil {
		return PhaseFailure{}, err
	}
	defer file.Close()
	var envelope struct {
		SchemaVersion int          `json:"schema_version"`
		Status        string       `json:"status"`
		Phase         jobs.Phase   `json:"phase"`
		JobID         string       `json:"job_id"`
		Error         PhaseFailure `json:"error"`
	}
	if err := json.NewDecoder(file).Decode(&envelope); err != nil {
		return PhaseFailure{}, err
	}
	if envelope.SchemaVersion != 1 || envelope.Status != "failed" || envelope.Phase != jobs.PhasePreview ||
		envelope.JobID != jobID || envelope.Error.Code == "" || envelope.Error.Message == "" {
		return PhaseFailure{}, errors.New("invalid phase failure")
	}
	return envelope.Error, nil
}

// OpenPreviewImage opens the image a succeeded marker names and validates it:
// a regular file within the size limit, well-formed PNG chunks, and the
// marker's dimensions within the pixel limit.
func (i *Inbox) OpenPreviewImage(marker Marker) (*PreviewImage, error) {
	source := i.file(marker.JobID, bridge.PreviewImageFile)
	info, err := regularFile(source)
	if err != nil {
		return nil, fmt.Errorf("inspect preview image: %w", err)
	}
	if info.Size() < 1 || info.Size() > i.maxBytes {
		return nil, fmt.Errorf("preview image size is outside configured limits")
	}
	input, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open preview image: %w", err)
	}
	image := &PreviewImage{JobID: marker.JobID, Size: info.Size(), file: input}
	if err := i.validatePreview(image, marker); err != nil {
		input.Close()
		return nil, err
	}
	return image, nil
}

func (i *Inbox) validatePreview(image *PreviewImage, marker Marker) error {
	if err := validatePNGChunks(image.file, image.Size); err != nil {
		return fmt.Errorf("validate preview PNG: %w", err)
	}
	if _, err := image.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind preview image: %w", err)
	}
	config, err := png.DecodeConfig(bufio.NewReader(io.LimitReader(image.file, image.Size)))
	if err != nil {
		return fmt.Errorf("validate preview PNG: %w", err)
	}
	if config.Width != marker.Resolution.X || config.Height != marker.Resolution.Y ||
		int64(config.Width)*int64(config.Height) > i.maxPixels {
		return fmt.Errorf("preview PNG dimensions do not match result marker")
	}
	if _, err := image.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind preview image: %w", err)
	}
	image.Width, image.Height = config.Width, config.Height
	return nil
}

// Analysis reads the analysis result once and validates it with
// analysisresult.Parse; ready is false while the bridge has not written it.
func (i *Inbox) Analysis(jobID string) (Analysis, bool, error) {
	path := i.file(jobID, bridge.AnalysisResultFile)
	if _, err := regularFile(path); errors.Is(err, os.ErrNotExist) {
		return Analysis{}, false, nil
	} else if err != nil {
		return Analysis{}, false, fmt.Errorf("inspect analysis result: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return Analysis{}, false, fmt.Errorf("open analysis result: %w", err)
	}
	defer file.Close()
	data, err := analysisresult.ReadAll(file)
	if err != nil {
		return Analysis{}, false, err
	}
	_, bridgeFailure, err := analysisresult.Parse(data, jobID)
	if err != nil {
		return Analysis{}, false, err
	}
	analysis := Analysis{Data: data}
	if bridgeFailure != nil {
		failure := PhaseFailure(*bridgeFailure)
		analysis.Failure = &failure
	}
	return analysis, true, nil
}

func regularFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("file is not a regular file")
	}
	return info, nil
}
