package jobs

// InterruptedCode fails a job whose run ended without an outcome: the worker
// crashed, hung past its lease or restarted mid-job.
const (
	InterruptedCode   = "interrupted"
	InterruptedDetail = "Preview generation was interrupted"
)

// Retryable is the one classification of failure codes (bridge phase codes
// and the runner's own, docs/bridge-protocol.md): whether running the same
// blueprint again can succeed. A job runs once; the API retries a retryable
// failure with a new job. The blueprint itself is at fault for these codes;
// every other failure (bridge, Factorio client, timeouts, interruptions) is
// retryable. factorio_restarted counts as the blueprint's fault: the worker
// never restarts Factorio and deploys stop the worker first, so a server that
// restarts during a job was almost always crashed by it, and running it again
// would only crash it again.
func Retryable(code string) bool {
	switch code {
	case "invalid_blueprint", "blueprint_too_large", "invalid_result", "result_mismatch", "analysis_failed",
		"factorio_restarted":
		return false
	default:
		return true
	}
}
