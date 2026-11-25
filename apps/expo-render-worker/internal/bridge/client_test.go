package bridge

import (
	"context"
	"encoding/json"
	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func TestSubmitUsesJSONAndValidatesPaths(t *testing.T) {
	t.Parallel()
	blueprint := "0" + strings.Repeat("a", 2001)
	var uploaded strings.Builder
	client := NewClient(bridgetest.Commander(func(command string) (string, error) {
		if len(command) > 1000 {
			t.Fatalf("command exceeds RCON limit: %d", len(command))
		}
		switch {
		case strings.HasPrefix(command, "/bp-preview-begin "):
			return `{"status":"accepted","job_id":"pj_test","requested_phases":["preview"]}`, nil
		case strings.HasPrefix(command, "/bp-preview-chunk "):
			var chunk uploadChunk
			if err := json.Unmarshal([]byte(strings.TrimPrefix(command, "/bp-preview-chunk ")), &chunk); err != nil {
				t.Fatalf("decode chunk: %v", err)
			}
			uploaded.WriteString(chunk.Data)
			return `{"status":"accepted","job_id":"pj_test"}`, nil
		case strings.HasPrefix(command, "/bp-preview-start "):
			if uploaded.String() != blueprint {
				t.Fatalf("uploaded blueprint = %q", uploaded.String())
			}
		default:
			t.Fatalf("unexpected command: %s", command)
		}
		return `{"status":"accepted","job_id":"pj_test","requested_phases":["preview"],` +
			`"paths":{"preview_image":"previews/pj_test/preview.png","preview_result":"previews/pj_test/preview-result.json"},` +
			`"render":{"resolution":{"x":100,"y":50},"zoom":1,"bounding_box":{}}}`, nil
	}))
	accepted, err := client.Submit(context.Background(), "pj_test", blueprint, []jobs.Phase{jobs.PhasePreview})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Render.Resolution.X != 100 {
		t.Fatalf("resolution = %#v", accepted.Render.Resolution)
	}
}
