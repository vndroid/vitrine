package main

import (
	"strings"
	"testing"
)

func TestVersionFlags(t *testing.T) {
	for _, args := range [][]string{{"-v"}, {"--version"}, {"-version"}, {"version"}} {
		if !isVersionFlag(args) {
			t.Errorf("version flag rejected: %v", args)
		}
	}
	for _, args := range [][]string{{}, {"-x"}, {"-v", "extra"}, {"-root", "-v"}} {
		if isVersionFlag(args) {
			t.Errorf("non-version args accepted: %v", args)
		}
	}
}

func TestVersionFormat(t *testing.T) {
	b := buildInfo{
		Version: "0.1.0", Branch: "HEAD", Revision: "74661ef",
		Date: "2026-09-07T08:00:24Z", Platform: "linux/arm64", Runtime: "go1.26.8", Tags: "netgo",
	}
	want := "vitrine, version 0.1.0 (branch: HEAD, revision: 74661ef)\n" +
		"  Date: 2026-09-07T08:00:24Z\n" +
		"  Platform: linux/arm64\n" +
		"  Runtime: go1.26.8\n" +
		"  Tags: netgo\n"
	if got := b.format(false); got != want {
		t.Errorf("plain output:\n%s\nwant:\n%s", got, want)
	}
	colored := b.format(true)
	for _, part := range []string{"\x1b[1mvitrine, version 0.1.0 ", "  \x1b[90mDate:\x1b[0m \x1b[1m2026-09-07T08:00:24Z\x1b[0m", "\x1b[90mTags:\x1b[0m"} {
		if !strings.Contains(colored, part) {
			t.Errorf("colored output misses %q: %q", part, colored)
		}
	}
}

func TestCurrentBuildDefaults(t *testing.T) {
	b := currentBuild()
	if b.Version != Version || b.Branch == "" || b.Revision == "" || b.Date == "" || b.Tags == "" {
		t.Errorf("incomplete build info: %+v", b)
	}
}
