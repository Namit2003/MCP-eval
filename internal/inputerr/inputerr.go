// Package inputerr formats errors about input files: JSON decode errors get a
// line and column, and lists of problems get the file path on every line.
package inputerr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Describe rewrites syntax and type errors from decoding data so they include
// a 1-based line:column. Other errors are returned unchanged.
func Describe(data []byte, err error) error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		line, col := position(data, syn.Offset)
		return fmt.Errorf("invalid JSON at line %d, column %d: %v", line, col, syn)
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		line, col := position(data, typ.Offset)
		field := typ.Field
		if field == "" {
			field = "value"
		}
		return fmt.Errorf("line %d, column %d: %s must be %s, got %s", line, col, field, typ.Type, typ.Value)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return errors.New("invalid JSON: unexpected end of input")
	}
	return err
}

func position(data []byte, offset int64) (line, col int) {
	offset = max(0, min(offset, int64(len(data))))
	before := data[:offset]
	line = bytes.Count(before, []byte("\n")) + 1
	col = max(1, int(offset)-(bytes.LastIndexByte(before, '\n')+1))
	return line, col
}

// Prefix prepends path to err. If err is a joined error (errors.Join), each
// member is prefixed so every line of output names the file it came from.
func Prefix(path string, err error) error {
	if err == nil || path == "" {
		return err
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range joined.Unwrap() {
			out = append(out, fmt.Errorf("%s: %w", path, e))
		}
		return errors.Join(out...)
	}
	return fmt.Errorf("%s: %w", path, err)
}
