package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"factorio-expo/internal/runnerapi"
)

var errUnsupportedMediaType = errors.New("content type must be application/json")

// decodeJSON enforces a JSON content type and a byte ceiling, rejects unknown
// fields, and requires the body to contain exactly one JSON value.
func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

// writeProblem writes an RFC 7807 style error body with the runner's stable
// machine-readable error code.
func writeProblem(w http.ResponseWriter, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, status, runnerapi.Problem{
		Type: "about:blank", Title: title, Status: status, Code: code, Detail: detail,
	})
}
