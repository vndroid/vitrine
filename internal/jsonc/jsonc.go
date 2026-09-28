// Package jsonc reads JSON with "//" and "/* */" comments, the format of
// the h5fs configuration files.
package jsonc

// Strip removes comments from commented JSON. It follows the h5fs
// implementation, so config files are read exactly as before: a quote
// preceded by a backslash does not toggle the string state.
func Strip(src []byte) []byte {
	const (
		none = iota
		single
		multi
	)
	out := make([]byte, 0, len(src))
	inString := false
	comment := none

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
			out = append(out, c)
		case comment == none && c == '/' && next == '/':
			comment = single
			i++
		case comment == none && c == '/' && next == '*':
			comment = multi
			i++
		case comment == none:
			out = append(out, c)
		case comment == single && c == '\r' && next == '\n':
			comment = none
			out = append(out, c, next)
			i++
		case comment == single && c == '\n':
			comment = none
			out = append(out, c)
		case comment == multi && c == '*' && next == '/':
			comment = none
			i++
		}
	}
	return out
}
