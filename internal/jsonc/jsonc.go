// Package jsonc reads JSON with "//" and "/* */" comments, the format of
// the h5fs configuration files.
package jsonc

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Strip blanks out the comments of commented JSON: comment characters
// become spaces and line breaks are kept, so byte offsets, lines and
// columns of the result are those of the source. It follows the h5fs
// implementation, so config files are read exactly as before: a quote
// preceded by a backslash does not toggle the string state.
func Strip(src []byte) []byte {
	const (
		none = iota
		single
		multi
	)
	out := make([]byte, len(src))
	copy(out, src)
	inString := false
	comment := none
	blank := func(i int) {
		if out[i] != '\n' && out[i] != '\r' {
			out[i] = ' '
		}
	}

	for i := 0; i < len(src); i++ {
		c := src[i]
		var next byte
		if i+1 < len(src) {
			next = src[i+1]
		}
		var prev byte
		if i > 0 {
			prev = src[i-1]
		}

		if comment == none && c == '"' && prev != '\\' {
			inString = !inString
		}

		switch {
		case inString:
		case comment == none && c == '/' && next == '/':
			comment = single
			blank(i)
			blank(i + 1)
			i++
		case comment == none && c == '/' && next == '*':
			comment = multi
			blank(i)
			blank(i + 1)
			i++
		case comment == none:
		case comment == single && (c == '\n' || c == '\r'):
			comment = none
		case comment == multi && c == '*' && next == '/':
			comment = none
			blank(i)
			blank(i + 1)
			i++
		default:
			blank(i)
		}
	}
	return out
}

// Position returns the 1-based line and column (in characters) of a byte
// offset.
func Position(src []byte, offset int64) (line, col int) {
	if offset > int64(len(src)) {
		offset = int64(len(src))
	}
	line, col = 1, 1
	for _, r := range string(src[:offset]) {
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// ErrorAt describes a JSON decoding error with the line and column in src,
// if the error carries an offset.
func ErrorAt(src []byte, err error) error {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	var offset int64 = -1
	switch {
	case errors.As(err, &syntax):
		offset = syntax.Offset
	case errors.As(err, &typ):
		offset = typ.Offset
	}
	if offset < 0 {
		return err
	}
	// the offset points behind the offending character
	if offset > 0 {
		_, size := utf8.DecodeLastRune(src[:min(offset, int64(len(src)))])
		offset -= int64(size)
	}
	line, col := Position(src, offset)
	return fmt.Errorf("line %d, column %d: %w", line, col, err)
}
