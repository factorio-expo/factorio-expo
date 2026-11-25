// Package analysisresult is the single owner of the production-analysis
// payload: the runner's result file as written by the Lua bridge, the durable
// blueprint_stats snapshot derived from it, and the API view and search facts
// derived from that snapshot.
//
// It describes a wire shape only. It imports no catalog, database or runner
// code, so both the runner and the API can depend on it.
package analysisresult

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	// SchemaVersion is the analysis payload version the bridge emits and this
	// package accepts. It is independent of the bridge command protocol version.
	SchemaVersion = 1
	// MaxBytes caps an analysis result file, on both the runner and API side.
	MaxBytes = 8 << 20

	StateComplete = "complete"
	StatePartial  = "partial"

	phaseAnalysis = "analysis"
)

// ErrTooLarge reports an analysis payload over MaxBytes.
var ErrTooLarge = errors.New("analysis result exceeds the size limit")

// Failure is a bridge-reported phase failure.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result is a validated successful analysis result.
type Result struct {
	// State is "complete" or "partial", reflecting unresolved production
	// materials only (not Factorio's quality mechanic).
	State    string
	Snapshot *Snapshot

	sections map[string]json.RawMessage
}

// Stored is the durable form of a Result, already split across the
// blueprint_stats columns that hold it.
type Stored struct {
	// Snapshot is the per-second production data; transport fields and state
	// are not part of it.
	Snapshot      []byte
	SchemaVersion int32
	State         string
}

// ReadAll reads an analysis payload, failing with ErrTooLarge past MaxBytes.
func ReadAll(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Parse validates runner analysis bytes for jobID. On success exactly one of
// the returned Result and Failure is set: a bridge-reported failure is a valid
// parse, not an error.
func Parse(data []byte, jobID string) (*Result, *Failure, error) {
	if len(data) > MaxBytes {
		return nil, nil, ErrTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var body map[string]json.RawMessage
	if err := decoder.Decode(&body); err != nil {
		return nil, nil, fmt.Errorf("decode analysis result: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("analysis result must contain one JSON value")
	}
	status, err := validateEnvelope(body, jobID)
	if err != nil {
		return nil, nil, err
	}
	if status == "failed" {
		var failure Failure
		if err := requiredJSON(body, "error", &failure); err != nil || failure.Code == "" || failure.Message == "" {
			return nil, nil, errors.New("analysis result has invalid failure")
		}
		return nil, &failure, nil
	}
	result, err := parseSuccess(body)
	if err != nil {
		return nil, nil, err
	}
	return result, nil, nil
}

func validateEnvelope(body map[string]json.RawMessage, jobID string) (string, error) {
	var schemaVersion int
	if err := requiredJSON(body, "schema_version", &schemaVersion); err != nil || schemaVersion != SchemaVersion {
		return "", errors.New("analysis result has invalid schema version")
	}
	var status, phase, id string
	if err := requiredJSON(body, "status", &status); err != nil || (status != "succeeded" && status != "failed") {
		return "", errors.New("analysis result has invalid status")
	}
	if err := requiredJSON(body, "phase", &phase); err != nil || phase != phaseAnalysis {
		return "", errors.New("analysis result has invalid phase")
	}
	if err := requiredJSON(body, "job_id", &id); err != nil || id != jobID {
		return "", errors.New("analysis result does not match job")
	}
	var tick float64
	if err := requiredJSON(body, "tick", &tick); err != nil || !finite(tick) {
		return "", errors.New("analysis result has invalid tick")
	}
	return status, nil
}

func parseSuccess(body map[string]json.RawMessage) (*Result, error) {
	var state string
	if err := requiredJSON(body, "state", &state); err != nil || (state != StateComplete && state != StatePartial) {
		return nil, errors.New("analysis result has invalid state")
	}
	sections := make(map[string]json.RawMessage, len(snapshotKeys))
	for _, key := range snapshotKeys {
		raw, ok := body[key]
		if !ok {
			return nil, fmt.Errorf("analysis result is missing %s", key)
		}
		sections[key] = raw
	}
	for _, key := range []string{"completeness", "calculator"} {
		if !isObject(sections[key]) {
			return nil, fmt.Errorf("analysis result has invalid %s", key)
		}
	}
	snapshot, err := decodeSnapshot(sections, true)
	if err != nil {
		return nil, err
	}
	return &Result{State: state, Snapshot: snapshot, sections: sections}, nil
}

// Stored returns the durable form of the result.
func (r *Result) Stored() (Stored, error) {
	encoded, err := json.Marshal(r.sections)
	if err != nil {
		return Stored{}, fmt.Errorf("encode production snapshot: %w", err)
	}
	return Stored{
		Snapshot:      encoded,
		SchemaVersion: SchemaVersion,
		State:         r.State,
	}, nil
}

func requiredJSON[T any](body map[string]json.RawMessage, key string, target *T) error {
	raw, ok := body[key]
	if !ok || json.Unmarshal(raw, target) != nil {
		return errors.New("missing or invalid field")
	}
	return nil
}

func isObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
