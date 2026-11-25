package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/fault"
)

// Every API error answers with one application/problem+json body (RFC 9457,
// Problem): status, title, detail (the text to show a person) and a stable
// code for clients to branch on, plus validation errors and the fields a
// domain error carries (preview_failed's failure_code). Huma operations and
// plain routes write the same body.

// kindStatus maps an error kind to its status code.
var kindStatus = map[fault.Kind]int{
	fault.Internal:      http.StatusInternalServerError,
	fault.Invalid:       http.StatusBadRequest,
	fault.Unauthorized:  http.StatusUnauthorized,
	fault.Forbidden:     http.StatusForbidden,
	fault.NotFound:      http.StatusNotFound,
	fault.Conflict:      http.StatusConflict,
	fault.TooLarge:      http.StatusRequestEntityTooLarge,
	fault.Unprocessable: http.StatusUnprocessableEntity,
	fault.TooMany:       http.StatusTooManyRequests,
	fault.Unavailable:   http.StatusServiceUnavailable,
}

// statusCodes are the generic codes of errors written by status alone
// (httpError).
var statusCodes = map[int]string{
	http.StatusBadRequest:            "invalid_request",
	http.StatusUnauthorized:          "unauthorized",
	http.StatusForbidden:             "forbidden",
	http.StatusNotFound:              "not_found",
	http.StatusMethodNotAllowed:      "method_not_allowed",
	http.StatusConflict:              "conflict",
	http.StatusRequestEntityTooLarge: "too_large",
	http.StatusUnsupportedMediaType:  "unsupported_media_type",
	http.StatusUnprocessableEntity:   "validation_failed",
	http.StatusTooManyRequests:       "too_many_requests",
	http.StatusInternalServerError:   "internal",
	http.StatusBadGateway:            "unavailable",
	http.StatusServiceUnavailable:    "unavailable",
}

// frameworkMessages are the texts for people of errors Huma raises itself
// (validation, body size, content type), whose own messages are technical.
// The technical detail stays in the body's errors list.
var frameworkMessages = map[int]string{
	http.StatusBadRequest:            "That request could not be understood.",
	http.StatusRequestEntityTooLarge: "That is too large to send.",
	http.StatusUnsupportedMediaType:  "That kind of content is not supported.",
	http.StatusUnprocessableEntity:   "Some of what you entered is not valid. Check it and try again.",
}

// errInternal is what a server fault answers: its details stay in the log.
var errInternal = fault.New(fault.Internal, "internal", "internal error")

// writeError answers a domain error (fault.From) with its status, code and
// message, and anything else as a logged 500.
func writeError(w http.ResponseWriter, err error) {
	coded, ok := fault.From(err)
	if !ok || coded.Kind == fault.Internal {
		slog.Error("request failed", "error", err)
		coded = errInternal
	}
	writeErrorBody(w, kindStatus[coded.Kind], coded.Code, coded.Msg, coded.Fields)
}

// httpError is http.Error with the API's JSON body and the status's generic
// code.
func httpError(w http.ResponseWriter, message string, status int) {
	writeErrorBody(w, status, statusCode(status), message, nil)
}

// writeCodedError answers with a code of the handler's own (a paused action,
// a failed human check).
func writeCodedError(w http.ResponseWriter, status int, code, message string) {
	writeErrorBody(w, status, code, message, nil)
}

func writeErrorBody(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeProblem(w, newProblem(status, code, message, fields))
}

// writeProblem writes a problem outside a Huma operation.
func writeProblem(w http.ResponseWriter, problem *Problem) {
	header := w.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", problemContentType)
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(problem.Status)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		slog.Error("write error response", "error", err)
	}
}

const problemContentType = "application/problem+json"

// Problem is the API's error body.
type Problem struct {
	Status int    `json:"status" doc:"The HTTP status code."`
	Title  string `json:"title" doc:"The status text."`
	// Detail is the message written for the person who sees the error.
	Detail string `json:"detail" doc:"What went wrong, written for the person who sees it."`
	Code   string `json:"code" doc:"Stable snake_case error code to branch on (not_found, validation_failed, banned, ...)."`
	// FailureCode is preview_failed's render or analysis failure.
	FailureCode string              `json:"failure_code,omitempty" doc:"preview_failed only: why the earlier render or analysis failed."`
	Errors      []*huma.ErrorDetail `json:"errors,omitempty" doc:"validation_failed only: each invalid parameter or body field."`
}

func newProblem(status int, code, message string, fields map[string]string) *Problem {
	return &Problem{
		Status: status, Title: http.StatusText(status), Detail: message, Code: code,
		FailureCode: fields["failure_code"],
	}
}

func (p *Problem) Error() string  { return p.Detail }
func (p *Problem) GetStatus() int { return p.Status }

// ContentType answers JSON errors as application/problem+json.
func (p *Problem) ContentType(contentType string) string {
	if contentType == "application/json" {
		return problemContentType
	}
	return contentType
}

// problemFor is Huma's error constructor: a domain error among errs answers
// with its own status, code and message (as writeError does); anything else
// is Huma's own error (validation, body size, content type) with the
// status's generic code. Unexpected errors are logged and answer 500.
func problemFor(status int, message string, errs ...error) huma.StatusError {
	for _, err := range errs {
		if coded, ok := fault.From(err); ok && coded.Kind != fault.Internal {
			return newProblem(kindStatus[coded.Kind], coded.Code, coded.Msg, coded.Fields)
		}
	}
	if status == http.StatusInternalServerError && len(errs) > 0 {
		// An unexpected handler error: its details stay in the log.
		slog.Error("request failed", "error", errors.Join(errs...))
		return newProblem(status, statusCode(status), "internal error", nil)
	}
	if friendly, ok := frameworkMessages[status]; ok {
		message = friendly
	}
	problem := newProblem(status, statusCode(status), message, nil)
	for _, err := range errs {
		if detailer, ok := err.(huma.ErrorDetailer); ok {
			problem.Errors = append(problem.Errors, detailer.ErrorDetail())
		} else if err != nil {
			problem.Errors = append(problem.Errors, &huma.ErrorDetail{Message: err.Error()})
		}
	}
	return problem
}

func statusCode(status int) string {
	if code, ok := statusCodes[status]; ok {
		return code
	}
	return "error"
}

func init() {
	huma.NewError = problemFor
}
