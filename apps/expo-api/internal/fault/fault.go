// Package fault is the API's one kind of domain error: a Kind (what went
// wrong, which httpapi maps to a status), a stable Code the clients tell
// errors apart by, and a message written for the person who sees it.
// Domain packages declare their errors with New and give a specific message
// with Withf; httpapi answers every one with the same JSON body.
package fault

import (
	"errors"
	"fmt"
	"maps"
)

// Kind says what went wrong; httpapi maps it to a status code.
type Kind uint8

const (
	// Internal is a server fault; its message is never shown.
	Internal Kind = iota
	Invalid
	Unauthorized
	Forbidden
	NotFound
	Conflict
	TooLarge
	Unprocessable
	TooMany
	Unavailable
)

// Error is a coded domain error. Two Errors match (errors.Is) when their
// codes match, so a sentinel matches every Withf of it.
type Error struct {
	Kind Kind
	// Code is the stable snake_case name clients branch on.
	Code string
	// Msg is the text shown to the person.
	Msg string
	// Fields are extra string fields of the response body (preview_failed's
	// failure_code).
	Fields map[string]string
}

// New declares a coded error.
func New(kind Kind, code, msg string) *Error {
	return &Error{Kind: kind, Code: code, Msg: msg}
}

func (e *Error) Error() string { return e.Msg }

// Is matches any Error with the same code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Withf is the same error with a more specific message.
func (e *Error) Withf(format string, args ...any) *Error {
	return &Error{Kind: e.Kind, Code: e.Code, Msg: fmt.Sprintf(format, args...), Fields: e.Fields}
}

// With is the same error carrying an extra body field.
func (e *Error) With(field, value string) *Error {
	fields := maps.Clone(e.Fields)
	if fields == nil {
		fields = map[string]string{}
	}
	fields[field] = value
	return &Error{Kind: e.Kind, Code: e.Code, Msg: e.Msg, Fields: fields}
}

// Coder is an error type of its own (a ban, with its details) that answers
// as an Error.
type Coder interface {
	error
	Fault() *Error
}

// From finds the coded error in err's chain: an *Error, or a Coder's.
func From(err error) (*Error, bool) {
	if coded, ok := errors.AsType[*Error](err); ok {
		return coded, true
	}
	if coder, ok := errors.AsType[Coder](err); ok {
		return coder.Fault(), true
	}
	return nil, false
}
