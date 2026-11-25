package bridge

import (
	"errors"
	"fmt"
)

// ProtocolVersion is the Go<->Lua bridge contract version. The Lua side
// reports `config.protocol_version` in every status reply and the runner
// refuses to work with any other value. Bump both together when a command,
// reply or result-file field changes incompatibly (see docs/bridge-protocol.md);
// a test pins this constant to `scripts/config.lua`.
const ProtocolVersion = 1

// ErrProtocolMismatch means the running bridge mod speaks another protocol
// version than this runner.
var ErrProtocolMismatch = errors.New("bridge protocol version mismatch")

// RCON command names. They are registered by the Lua mod in `control.lua` from
// `scripts/config.lua`.
const (
	CommandStatus = "/bp-preview-status"
	CommandBegin  = "/bp-preview-begin"
	CommandChunk  = "/bp-preview-chunk"
	CommandStart  = "/bp-preview-start"
)

// Upload limits; equal to `upload_chunk_length` and `upload_max_chunks` in
// `scripts/config.lua` (pinned by TestUploadLimitsMatchLuaConfig).
const (
	chunkSize      = 800
	maxUploadChunk = 4096
)

// Inbox layout: every job writes under `previews/<job_id>/` on the shared
// volume. The bridge reports these relative paths and the runner checks them.
const (
	InboxDir           = "previews"
	PreviewImageFile   = "preview.png"
	PreviewResultFile  = "preview-result.json"
	AnalysisResultFile = "analysis-result.json"
	// ClientHeartbeatFile sits directly in the inbox; the capturing client
	// rewrites it every second (`client_heartbeat_file` in config.lua).
	ClientHeartbeatFile = "client-heartbeat"
)

// RelativePath is the bridge-reported location of one job file.
func RelativePath(jobID, file string) string { return InboxDir + "/" + jobID + "/" + file }

func checkProtocol(status Status) error {
	if status.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("%w: bridge reports %d, runner expects %d",
			ErrProtocolMismatch, status.ProtocolVersion, ProtocolVersion)
	}
	return nil
}
