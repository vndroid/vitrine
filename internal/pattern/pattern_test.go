package pattern

import "testing"

func TestCompile(t *testing.T) {
	tests := []struct {
		pattern    string
		ignoreCase bool
		match      []string
		noMatch    []string
	}{
		{`^\.`, false, []string{".git", ".env"}, []string{"a.txt"}},
		{`^_vitrine`, false, []string{"_vitrine", "_vitrine.header.md"}, []string{"x_vitrine"}},
		// the h5fs client escapes spaces and "#" (esc_pattern)
		{`my\ file\#1`, false, []string{"my file#1.txt"}, []string{"myfile#1"}},
		// advanced search: characters joined with ".*?"
		{`a.*?b|c`, true, []string{"AxB", "c"}, []string{"ba"}},
		{`\d+\.jpg$`, true, []string{"IMG123.JPG"}, []string{"img.jpg"}},
		{`\-\[x\]`, false, []string{"-[x]"}, []string{"x"}},
	}
	for _, tt := range tests {
		re, err := Compile(tt.pattern, tt.ignoreCase)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tt.pattern, err)
		}
		for _, s := range tt.match {
			if !re.MatchString(s) {
				t.Errorf("%q should match %q", tt.pattern, s)
			}
		}
		for _, s := range tt.noMatch {
			if re.MatchString(s) {
				t.Errorf("%q should not match %q", tt.pattern, s)
			}
		}
	}
}

func TestCompileRejectsPCREOnlySyntax(t *testing.T) {
	for _, p := range []string{`(a)\1`, `a(?=b)`, `(?<!a)b`} {
		if _, err := Compile(p, false); err == nil {
			t.Errorf("Compile(%q) should fail", p)
		}
	}
}
