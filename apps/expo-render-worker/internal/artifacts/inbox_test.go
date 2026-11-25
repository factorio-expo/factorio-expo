package artifacts

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newArtifacts opens an inbox and a retained store under one temp root and
// returns the inbox directory for writing bridge results into.
func newArtifacts(t *testing.T, maxImageBytes int64) (*Inbox, *Store, string) {
	t.Helper()
	root := t.TempDir()
	inboxPath := filepath.Join(root, "inbox")
	inbox, err := NewInbox(inboxPath, maxImageBytes, 100)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := NewStore(filepath.Join(root, "retained"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return inbox, retained, inboxPath
}

func previewMarker(jobID string, width, height int) Marker {
	marker := Marker{
		SchemaVersion: 1, Status: "succeeded", Phase: "preview", JobID: jobID,
		ImagePath: "previews/" + jobID + "/preview.png", Zoom: 1, BoundingBox: json.RawMessage(`{}`), Tick: 10,
	}
	marker.Resolution.X, marker.Resolution.Y = width, height
	return marker
}

func TestMarkerAndRetainPreview(t *testing.T) {
	t.Parallel()
	inbox, retained, inboxPath := newArtifacts(t, 1<<20)
	jobID := "pj_test"
	jobDir := filepath.Join(inboxPath, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	imageFile, err := os.Create(filepath.Join(jobDir, "preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	source.Set(1, 1, color.RGBA{R: 255, A: 255})
	if err := png.Encode(imageFile, source); err != nil {
		t.Fatal(err)
	}
	if err := imageFile.Close(); err != nil {
		t.Fatal(err)
	}
	markerBody, err := json.Marshal(previewMarker(jobID, 3, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "preview-result.json"), markerBody, 0o644); err != nil {
		t.Fatal(err)
	}

	decoded, ready, err := inbox.PreviewMarker(jobID)
	if err != nil || !ready {
		t.Fatalf("marker = %#v, %v, %v", decoded, ready, err)
	}
	preview, err := inbox.OpenPreviewImage(decoded)
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	artifact, err := retained.RetainPreview(context.Background(), preview)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Width != 3 || artifact.Height != 2 || artifact.Size == 0 || artifact.MediaType != "image/png" {
		t.Fatalf("artifact = %#v", artifact)
	}
	copied, err := os.ReadFile(retained.Path(artifact))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(jobDir, "preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != string(original) || int64(len(copied)) != artifact.Size {
		t.Fatal("retained preview differs from the inbox image")
	}
}

func TestMarkerRejectsMismatchedJob(t *testing.T) {
	t.Parallel()
	inbox, _, inboxPath := newArtifacts(t, 1024)
	jobDir := filepath.Join(inboxPath, "pj_test")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":1,"status":"succeeded","phase":"preview","job_id":"pj_other","image_path":"previews/pj_other/preview.png","resolution":{"x":1,"y":1},"zoom":1,"bounding_box":{},"tick":1}`
	if err := os.WriteFile(filepath.Join(jobDir, "preview-result.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inbox.PreviewMarker("pj_test"); err == nil {
		t.Fatal("expected marker mismatch error")
	}
}

func TestOpenPreviewImageRejectsCorruptPNG(t *testing.T) {
	t.Parallel()
	inbox, _, inboxPath := newArtifacts(t, 1024)
	jobDir := filepath.Join(inboxPath, "pj_test")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(jobDir, "preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(jobDir, "preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 0xff
	if err := os.WriteFile(filepath.Join(jobDir, "preview.png"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.OpenPreviewImage(previewMarker("pj_test", 1, 1)); err == nil {
		t.Fatal("expected corrupt PNG error")
	}
}

func goldenAnalysis(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "test-data", "analysis", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func analysisInbox(t *testing.T, jobID string, body []byte) (*Inbox, *Store) {
	t.Helper()
	inbox, retained, inboxPath := newArtifacts(t, 1024)
	jobDir := filepath.Join(inboxPath, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "analysis-result.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return inbox, retained
}

// The analysis is read and parsed once; the store keeps exactly those bytes.
func TestRetainAnalysisPreservesValidatedJSON(t *testing.T) {
	t.Parallel()
	body := goldenAnalysis(t, "analysis-succeeded.json")
	inbox, retained := analysisInbox(t, "pj_golden", body)
	analysis, ready, err := inbox.Analysis("pj_golden")
	if err != nil || !ready || analysis.Failure != nil {
		t.Fatalf("analysis = %+v %v %v", analysis.Failure, ready, err)
	}
	artifact, err := retained.RetainAnalysis(context.Background(), "pj_golden", analysis.Data)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Phase != "analysis" || artifact.MediaType != "application/json" || artifact.Size != int64(len(body)) {
		t.Fatalf("artifact = %#v", artifact)
	}
	copied, err := os.ReadFile(retained.Path(artifact))
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != string(body) {
		t.Fatalf("analysis JSON was changed: %s", copied)
	}
}

func TestAnalysisReadiness(t *testing.T) {
	t.Parallel()
	inbox, _ := analysisInbox(t, "pj_golden", goldenAnalysis(t, "analysis-succeeded.json"))
	if analysis, ready, err := inbox.Analysis("pj_golden"); err != nil || !ready || analysis.Failure != nil {
		t.Fatalf("succeeded = %+v %v %v", analysis.Failure, ready, err)
	}
	if _, ready, err := inbox.Analysis("pj_other"); err != nil || ready {
		t.Fatalf("missing result ready=%v err=%v", ready, err)
	}
	failed, _ := analysisInbox(t, "pj_golden", goldenAnalysis(t, "analysis-failed.json"))
	analysis, ready, err := failed.Analysis("pj_golden")
	if err != nil || !ready || analysis.Failure == nil ||
		analysis.Failure.Code != "analysis_failed" || analysis.Failure.Message != "boom" {
		t.Fatalf("failed = %+v %v %v", analysis.Failure, ready, err)
	}
	wrongJob, _ := analysisInbox(t, "pj_wrong", goldenAnalysis(t, "analysis-succeeded.json"))
	if _, _, err := wrongJob.Analysis("pj_wrong"); err == nil {
		t.Fatal("result for another job accepted")
	}
}

func TestAnalysisRejectsInvalidResult(t *testing.T) {
	t.Parallel()
	inbox, _ := analysisInbox(t, "pj_golden", []byte(`{"schema_version":1}`))
	if _, _, err := inbox.Analysis("pj_golden"); err == nil {
		t.Fatal("invalid analysis accepted")
	}
}
