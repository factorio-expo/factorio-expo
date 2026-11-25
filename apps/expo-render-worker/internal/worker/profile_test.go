package worker

import (
	"context"
	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func TestDeferredRenderProfileStillValidatesPNG(t *testing.T) {
	for _, zoom := range []float64{1, 2} {
		t.Run(fmt.Sprint(zoom), func(t *testing.T) {
			root, err := os.MkdirTemp(".", ".profile-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			inbox := filepath.Join(root, "inbox")
			results, retained := testArtifacts(t, inbox, filepath.Join(root, "artifacts"))
			dir := filepath.Join(inbox, "pj_test")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(dir, "preview.png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			marker := `{"schema_version":1,"status":"succeeded","phase":"preview","job_id":"pj_test","image_path":"previews/pj_test/preview.png","resolution":{"x":2,"y":3},"zoom":1,"bounding_box":{},"space_platform":false}`
			if err := os.WriteFile(filepath.Join(dir, "preview-result.json"), []byte(marker), 0644); err != nil {
				t.Fatal(err)
			}
			store := &recordingStore{job: jobs.Job{ID: "pj_test", Blueprint: "0a",
				PreviewPhase: jobs.PhaseResult{Status: jobs.PhasePending}}}
			started := false
			client := bridge.NewClient(bridgetest.Commander(func(command string) (string, error) {
				switch {
				case strings.HasPrefix(command, "/bp-preview-start "):
					// Accepted without a render profile: the worker asks for it later.
					started = true
					return `{"status":"accepted","job_id":"pj_test","requested_phases":["preview"],"paths":{"preview_image":"previews/pj_test/preview.png","preview_result":"previews/pj_test/preview-result.json"}}`, nil
				case command != "/bp-preview-status":
					return `{"status":"accepted","job_id":"pj_test"}`, nil
				case !started:
					return `{"status":"ok","protocol_version":1,"capture":{"connected":true}}`, nil
				}
				return fmt.Sprintf(`{"status":"ok","protocol_version":1,"capture":{"connected":true},"completed_job":{"status":"accepted","job_id":"pj_test","requested_phases":["preview"],"paths":{"preview_image":"previews/pj_test/preview.png","preview_result":"previews/pj_test/preview-result.json"},"render":{"resolution":{"x":2,"y":3},"zoom":%g,"bounding_box":{},"space_platform":false}}}`, zoom), nil
			}))
			w := New("test", store, client, results, retained, time.Second, time.Millisecond)
			if err := w.runNext(context.Background()); err != nil {
				t.Fatal(err)
			}
			wantCompleted, wantFailed := 1, 0
			if zoom != 1 {
				wantCompleted, wantFailed = 0, 1
			}
			if store.completed != wantCompleted || store.failed != wantFailed {
				t.Fatalf("completed=%d failed=%d", store.completed, store.failed)
			}
		})
	}
}
