// Package audit is the staff audit log (docs/admin.md): the staff member
// behind a write, and the admin_actions row every staff write adds, in the
// same transaction as its change. Staff are not expo accounts: the log knows
// them only by their console sign-in email.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/shortid"
)

// Staff is the staff member behind a staff write.
type Staff struct {
	Username string
}

// MaxReasonRunes bounds the reason a staff member gives for a write.
const MaxReasonRunes = 500

// ErrInvalidRequest wraps what is wrong with a staff write's input; its
// message is safe to show.
var ErrInvalidRequest = fault.New(fault.Invalid, "invalid_request", "invalid request")

// CleanReason trims a staff reason and checks its length.
func CleanReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > MaxReasonRunes {
		return "", ErrInvalidRequest.Withf("the reason is at most %d characters", MaxReasonRunes)
	}
	return reason, nil
}

// Record is one staff write for the audit log. Targets are ids, never
// personal data; Before and After hold the changed fields (nil for none).
type Record struct {
	Action     string
	TargetType string
	TargetID   string
	Reason     string
	Before     any
	After      any
}

// Write adds the audit row inside the transaction of queries: the one way a
// staff write is audited, together with the change it records.
func Write(ctx context.Context, queries *db.Queries, by Staff, record Record) error {
	if by.Username == "" {
		return fmt.Errorf("record staff action %s: no staff identity", record.Action)
	}
	before, err := encode(record.Before)
	if err != nil {
		return err
	}
	after, err := encode(record.After)
	if err != nil {
		return err
	}
	if err := queries.RecordAdminAction(ctx, db.RecordAdminActionParams{
		ID: shortid.New(), StaffUsername: by.Username,
		Action: record.Action, TargetType: record.TargetType, TargetID: record.TargetID,
		Reason: record.Reason, Before: before, After: after,
	}); err != nil {
		return fmt.Errorf("record staff action %s: %w", record.Action, err)
	}
	return nil
}

func encode(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode audit value: %w", err)
	}
	return encoded, nil
}

// Log writes audit rows on their own, for staff work that is no single
// transaction (a cleanup run in batches, a job re-run).
type Log struct {
	queries *db.Queries
}

// NewLog builds a Log over the generated queries.
func NewLog(queries *db.Queries) *Log { return &Log{queries: queries} }

// Add writes one audit row.
func (l *Log) Add(ctx context.Context, by Staff, record Record) error {
	reason, err := CleanReason(record.Reason)
	if err != nil {
		return err
	}
	record.Reason = reason
	return Write(ctx, l.queries, by, record)
}
