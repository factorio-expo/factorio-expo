package preview

import (
	"errors"
	"net/http"

	"factorio-expo/apps/expo-api/internal/outbox"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/internal/runnerapi"
)

// failureOutcome turns a failed runner phase into an outcome. The runner runs
// each job once and says whether its failure can succeed on another run
// (runnerapi.Failure.Retryable); a retryable failure spends the phase's retry
// budget on a new runner job, anything else fails the phase now. A failure
// without details (an older runner) is retried.
func failureOutcome(failure *runnerapi.Failure, fallbackCode string) outbox.Outcome {
	if failure == nil || failure.Code == "" {
		return outbox.Transient(fallbackCode, "")
	}
	if failure.Retryable {
		return outbox.Transient(failure.Code, failure.Detail)
	}
	return outbox.Permanent(failure.Code, failure.Detail)
}

// dispatchOutcome classifies a runner create error. A client error the runner
// raised about the request itself (422 invalid_blueprint, 413 request_too_large
// ...) can never succeed on retry; availability and transport errors can.
func dispatchOutcome(err error) outbox.Outcome {
	var runnerError *runner.Error
	if errors.As(err, &runnerError) && runnerError.Status >= 400 && runnerError.Status < 500 &&
		runnerError.Status != http.StatusNotFound && runnerError.Status != http.StatusRequestTimeout &&
		runnerError.Status != http.StatusTooManyRequests {
		code := runnerError.Code
		if code == "" {
			code = "dispatch_rejected"
		}
		return outbox.Permanent(code, runnerError.Error()+": "+runnerError.Detail)
	}
	return outbox.Transient("dispatch_failed", err.Error())
}
