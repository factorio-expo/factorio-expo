package dataexport

import (
	"bufio"
	"encoding/json"
	"io"
)

// stream writes one JSON document piece by piece: the export is never built
// in memory. The first error sticks; later writes do nothing.
type stream struct {
	w   *bufio.Writer
	err error
}

func newStream(w io.Writer) *stream {
	return &stream{w: bufio.NewWriterSize(w, 32<<10)}
}

// raw writes JSON syntax as is.
func (s *stream) raw(text string) {
	if s.err == nil {
		_, s.err = s.w.WriteString(text)
	}
}

// value writes v as JSON.
func (s *stream) value(v any) {
	if s.err != nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		s.err = err
		return
	}
	_, s.err = s.w.Write(data)
}

// field starts a member of the open object: `,"name":` (no comma before the
// first). name is a constant of this package, never user data.
func (s *stream) field(name string, first bool) {
	if !first {
		s.raw(",")
	}
	s.raw(`"` + name + `":`)
}

// fail records an error from outside the stream (a database read).
func (s *stream) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

// flush writes out what is buffered and returns the first error.
func (s *stream) flush() error {
	if s.err == nil {
		s.err = s.w.Flush()
	}
	return s.err
}

// list writes a JSON array of items.
func list[T any](s *stream, items []T) {
	s.raw("[")
	for i, item := range items {
		if i > 0 {
			s.raw(",")
		}
		s.value(item)
	}
	s.raw("]")
}
