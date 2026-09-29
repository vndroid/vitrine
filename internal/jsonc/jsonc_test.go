package jsonc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrip(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", `{"a": 1}`, `{"a": 1}`},
		{"line comment", "{\n// c\n\"a\": 1 // x\n}", "{\n    \n\"a\": 1     \n}"},
		{"crlf line comment", "{// c\r\n\"a\": 1}", "{    \r\n\"a\": 1}"},
		{"block comment", `{/* c */"a": /* x */1}`, `{       "a":        1}`},
		{"multiline block keeps lines", "{/*\n * c\n */\"a\": 1}", "{  \n    \n   \"a\": 1}"},
		{"slashes in string", `{"u": "//host/*x*/"}`, `{"u": "//host/*x*/"}`},
		{"escaped quote in string", `{"s": "a\"//b"}`, `{"s": "a\"//b"}`},
		{"quote in comment", `{/* " */"a": 1}`, `{       "a": 1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(Strip([]byte(tt.in)))
			if got != tt.want {
				t.Errorf("Strip(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if len(got) != len(tt.in) {
				t.Errorf("length changed: %d -> %d", len(tt.in), len(got))
			}
		})
	}
}

func TestPosition(t *testing.T) {
	src := []byte("ab\n中文x\ny")
	for off, want := range map[int64][2]int{0: {1, 1}, 2: {1, 3}, 3: {2, 1}, 9: {2, 3}, 11: {3, 1}} {
		if l, c := Position(src, off); l != want[0] || c != want[1] {
			t.Errorf("Position(%d) = %d:%d, want %d:%d", off, l, c, want[0], want[1])
		}
	}
}

func TestErrorAt(t *testing.T) {
	src := []byte("{\n  /* comment */\n  \"a\": 1,\n  \"b\" 2\n}")
	var v any
	err := json.Unmarshal(Strip(src), &v)
	if got := ErrorAt(src, err).Error(); !strings.HasPrefix(got, "line 4, column 7:") {
		t.Errorf("ErrorAt = %s", got)
	}
}
