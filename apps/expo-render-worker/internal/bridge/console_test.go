package bridge

import (
	"context"
	"testing"

	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
)

// Factorio sends console messages queued since the previous RCON command in
// front of the next command's output, e.g. the client's join on a fresh server.
func TestStatusIgnoresQueuedConsoleMessages(t *testing.T) {
	t.Parallel()
	client := NewClient(bridgetest.Commander(func(string) (string, error) {
		return "2026-10-27 09:30:01 [JOIN]  joined the game\r\n" +
			"2026-10-27 09:30:02 [CHAT] <server>: {\"status\":\"error\"}\n" +
			`{"status":"ok","protocol_version":1,"capture":{"connected":true,"player":""}}` + "\n", nil
	}))
	status, err := client.Status(context.Background())
	if err != nil || !status.Capture.Connected {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestStatusRejectsTwoValuesOnTheReplyLine(t *testing.T) {
	t.Parallel()
	client := NewClient(bridgetest.Commander(func(string) (string, error) {
		return `{"status":"ok","protocol_version":1,"capture":{"connected":true}} {}`, nil
	}))
	if _, err := client.Status(context.Background()); err == nil {
		t.Fatal("expected an error for two JSON values")
	}
}
