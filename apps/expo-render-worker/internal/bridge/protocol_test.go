package bridge

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"factorio-expo/apps/expo-render-worker/internal/bridge/bridgetest"
)

// The Lua mod and this package each hold the protocol version; they must agree.
func TestProtocolVersionMatchesLuaConfig(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../factorio/factorio-expo-bridge/scripts/config.lua")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`protocol_version\s*=\s*(\d+)`).FindSubmatch(raw)
	if match == nil {
		t.Fatal("config.lua has no protocol_version")
	}
	if got, _ := strconv.Atoi(string(match[1])); got != ProtocolVersion {
		t.Fatalf("config.lua protocol_version = %d, Go ProtocolVersion = %d", got, ProtocolVersion)
	}
}

// Lua command names are `config.*_command_name` without the slash.
func TestCommandNamesMatchLuaConfig(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../factorio/factorio-expo-bridge/scripts/config.lua")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{CommandStatus, CommandBegin, CommandChunk, CommandStart} {
		if !strings.Contains(string(raw), `"`+strings.TrimPrefix(command, "/")+`"`) {
			t.Errorf("config.lua does not define command %q", command)
		}
	}
}

// The upload limits are enforced on both sides; config.lua must match.
func TestUploadLimitsMatchLuaConfig(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../factorio/factorio-expo-bridge/scripts/config.lua")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"upload_chunk_length": chunkSize, "upload_max_chunks": maxUploadChunk} {
		match := regexp.MustCompile(name + `\s*=\s*(\d+)`).FindSubmatch(raw)
		if match == nil {
			t.Errorf("config.lua has no %s", name)
			continue
		}
		if got, _ := strconv.Atoi(string(match[1])); got != want {
			t.Errorf("config.lua %s = %d, Go = %d", name, got, want)
		}
	}
}

func TestStatusRejectsOtherProtocolVersion(t *testing.T) {
	t.Parallel()
	for _, reply := range []string{
		`{"status":"ok","capture":{"connected":true}}`,
		`{"status":"ok","protocol_version":2,"capture":{"connected":true}}`,
	} {
		client := NewClient(bridgetest.Commander(func(string) (string, error) { return reply, nil }))
		if _, err := client.Status(t.Context()); !errors.Is(err, ErrProtocolMismatch) {
			t.Fatalf("reply %s: err = %v, want ErrProtocolMismatch", reply, err)
		}
	}
}
