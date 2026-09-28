package jsonc

import "testing"

func TestStrip(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", `{"a": 1}`, `{"a": 1}`},
		{"line comment", "{\n// c\n\"a\": 1 // x\n}", "{\n\n\"a\": 1 \n}"},
		{"crlf line comment", "{// c\r\n\"a\": 1}", "{\r\n\"a\": 1}"},
		{"block comment", `{/* c */"a": /* x */1}`, `{"a": 1}`},
		{"multiline block", "{/*\n * c\n */\"a\": 1}", `{"a": 1}`},
		{"slashes in string", `{"u": "//host/*x*/"}`, `{"u": "//host/*x*/"}`},
		{"escaped quote in string", `{"s": "a\"//b"}`, `{"s": "a\"//b"}`},
		{"quote in comment", `{/* " */"a": 1}`, `{"a": 1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(Strip([]byte(tt.in))); got != tt.want {
				t.Errorf("Strip(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
