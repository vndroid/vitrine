// Package pattern compiles the regular expressions of the h5fs options
// ("view.hidden") and of client searches. Those were written for PCRE and
// JavaScript, Go uses RE2: the common subset works unchanged, escapes RE2
// does not know are translated and anything else (backreferences,
// lookarounds) is rejected.
package pattern

import (
	"regexp"
	"strings"
)

// Compile compiles a PCRE-style pattern (without delimiters).
func Compile(pattern string, ignoreCase bool) (*regexp.Regexp, error) {
	expr := translate(pattern)
	if ignoreCase {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

// translate rewrites escapes RE2 rejects but PCRE and JavaScript accept:
// a backslash before any character without a special meaning escapes it
// literally (the h5fs client escapes spaces and "#", for example).
func translate(pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern))
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c != '\\' || i+1 >= len(pattern) {
			b.WriteByte(c)
			continue
		}
		next := pattern[i+1]
		i++
		switch {
		case isASCIIAlnum(next):
			// class or escape sequence (\d, \w, \x41, ...), keep for RE2
			b.WriteByte('\\')
			b.WriteByte(next)
		case next < 0x80 && isPunct(next):
			b.WriteByte('\\')
			b.WriteByte(next)
		case next < 0x80:
			// literal ASCII character that must not be escaped in RE2
			b.WriteString(regexp.QuoteMeta(string(next)))
		default:
			// non-ASCII: keep the escaped character literally
			b.WriteByte(next)
		}
	}
	return b.String()
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isPunct reports whether RE2 accepts c after a backslash.
func isPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}
