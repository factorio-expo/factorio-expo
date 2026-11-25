package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/jobs"
	"factorio-expo/apps/expo-api/internal/maintenance"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// Every domain error answers with its status and code, and its own message;
// anything else is a 500 that keeps its details in the log.
func TestWriteError(t *testing.T) {
	t.Parallel()
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, test := range []struct {
		err     error
		status  int
		code    string
		message string
		fields  map[string]string
	}{
		{catalog.ErrNotFound, http.StatusNotFound, "not_found", "not found", nil},
		{fmt.Errorf("load entry: %w", catalog.ErrForbidden), http.StatusForbidden, "forbidden", "not allowed to modify this entry", nil},
		{catalog.ErrInvalidEntryMetadata, http.StatusBadRequest, "invalid_entry_metadata",
			"title is required and must be at most 160 characters; description must be at most 5000 characters", nil},
		{catalog.ErrCommentLimit, http.StatusTooManyRequests, "comment_limit", catalog.ErrCommentLimit.Msg, nil},
		{catalog.ErrChallengeRefused.Withf("voting is not open"), http.StatusConflict, "challenge_refused", "voting is not open", nil},
		{staffops.ErrEdgePurgeOff, http.StatusBadRequest, "invalid_request", "the Cloudflare purge is not configured", nil},
		{&catalog.BanError{Ban: catalog.Ban{Action: catalog.BanComment, ReasonCode: "spam", ExpiresAt: &until}},
			http.StatusForbidden, "banned", "You are banned from commenting for spam until 2 January 2030, 03:04 UTC.", nil},
		{fmt.Errorf("upload: %w", renderqueue.ErrFull), http.StatusTooManyRequests, "render_queue_full", renderqueue.ErrFull.Msg, nil},
		{&catalog.PreviewFailedError{Code: "factorio_restarted"}, http.StatusUnprocessableEntity, "preview_failed",
			"This blueprint already failed to render or analyse.", map[string]string{"failure_code": "factorio_restarted"}},
		{blueprint.ErrLimitExceeded.Withf("blueprint exceeds allowed limits: more than 3 nodes"), http.StatusUnprocessableEntity,
			"limit_exceeded", "blueprint exceeds allowed limits: more than 3 nodes", nil},
		{accounts.ErrIdentityHeld, http.StatusForbidden, "identity_held", accounts.ErrIdentityHeld.Msg, nil},
		{accounts.ErrUsernameTaken, http.StatusConflict, "username_taken", "that username is not available", nil},
		{maintenance.ErrUnknown, http.StatusNotFound, "not_found", "unknown cleanup", nil},
		{jobs.ErrAlreadyQueued, http.StatusConflict, "already_queued", "already queued", nil},
		{errors.New("connection refused"), http.StatusInternalServerError, "internal", "internal error", nil},
	} {
		recorder := httptest.NewRecorder()
		writeError(recorder, test.err)
		var body Problem
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("%v: body %q: %v", test.err, recorder.Body, err)
		}
		if recorder.Code != test.status || body.Status != test.status || body.Code != test.code ||
			body.Detail != test.message || body.FailureCode != test.fields["failure_code"] ||
			recorder.Header().Get("Content-Type") != problemContentType {
			t.Errorf("%v = %d %s", test.err, recorder.Code, recorder.Body)
		}

		// A Huma operation returning the error answers the same.
		problem, ok := problemFor(http.StatusInternalServerError, "unexpected error occurred", test.err).(*Problem)
		if !ok || !reflect.DeepEqual(*problem, Problem{
			Status: test.status, Title: http.StatusText(test.status), Detail: test.message,
			Code: test.code, FailureCode: test.fields["failure_code"],
		}) {
			t.Errorf("problemFor(%v) = %+v", test.err, problem)
		}
	}
}

// Huma's own errors (validation, body size) carry the status's generic code
// and their details.
func TestProblemForHumaErrors(t *testing.T) {
	t.Parallel()
	problem := problemFor(http.StatusUnprocessableEntity, "validation failed",
		&huma.ErrorDetail{Message: "expected length <= 160", Location: "body.title"}).(*Problem)
	if problem.Status != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
		problem.Detail != frameworkMessages[http.StatusUnprocessableEntity] || len(problem.Errors) != 1 ||
		problem.Errors[0].Location != "body.title" {
		t.Errorf("validation problem = %+v", problem)
	}
	tooLarge := problemFor(http.StatusRequestEntityTooLarge, "request body is too large").(*Problem)
	if tooLarge.Code != "too_large" || tooLarge.Detail != frameworkMessages[http.StatusRequestEntityTooLarge] {
		t.Errorf("too large problem = %+v", tooLarge)
	}
}

// A sentinel matches its specific variants, and no other code.
func TestFaultMatching(t *testing.T) {
	t.Parallel()
	refused := catalog.ErrChallengeRefused.Withf("voting is not open")
	if !errors.Is(fmt.Errorf("vote: %w", refused), catalog.ErrChallengeRefused) {
		t.Error("a refusal does not match ErrChallengeRefused")
	}
	if errors.Is(refused, catalog.ErrChallengeForbidden) {
		t.Error("a refusal matches ErrChallengeForbidden")
	}
}

// Errors written by status alone carry the status's generic code.
func TestHTTPError(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Type", "text/plain")
	httpError(recorder, "invalid JSON body", http.StatusBadRequest)
	var body Problem
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != http.StatusBadRequest || body.Code != "invalid_request" || body.Detail != "invalid JSON body" ||
		recorder.Header().Get("Content-Type") != problemContentType {
		t.Errorf("httpError = %d %v %s", recorder.Code, recorder.Header(), recorder.Body)
	}
}
