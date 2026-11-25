package bridge

import (
	"context"
	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func TestFastAcceptanceAndDeferredRenderStatus(t *testing.T) {
	t.Parallel()
	client := NewClient(bridgetest.Commander(func(command string) (string, error) {
		if command == "/bp-preview-status" {
			return `{"status":"ok","protocol_version":1,"capture":{"connected":true},"active_job":{"job_id":"pj_test","phase":"stabilizing","requested_phases":["preview"],"phases":{"preview":"pending","analysis":"not_requested"},"render":{"resolution":{"x":100,"y":50},"zoom":1,"bounding_box":{},"space_platform":false}},"completed_job":null}`, nil
		}
		if strings.HasPrefix(command, "/bp-preview-start ") {
			return `{"status":"accepted","job_id":"pj_test","requested_phases":["preview"],"paths":{"preview_image":"previews/pj_test/preview.png","preview_result":"previews/pj_test/preview-result.json"}}`, nil
		}
		return `{"status":"accepted","job_id":"pj_test"}`, nil
	}))
	accepted, err := client.Submit(context.Background(), "pj_test", "0a", []jobs.Phase{jobs.PhasePreview})
	if err != nil || accepted.Render != nil {
		t.Fatalf("fast ACK: accepted=%+v err=%v", accepted, err)
	}
	status, err := client.Status(context.Background())
	if err != nil || status.ActiveJob == nil || status.ActiveJob.Render == nil || status.ActiveJob.Render.Resolution.X != 100 {
		t.Fatalf("deferred render profile: status=%+v err=%v", status, err)
	}
}
