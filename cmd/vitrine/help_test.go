package main

import (
	"flag"
	"strings"
	"testing"
)

func TestHelpFlags(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"-help"}, {"help"}} {
		if !isHelpFlag(args) {
			t.Errorf("help flag rejected: %v", args)
		}
	}
	for _, args := range [][]string{{}, {"-x"}, {"-h", "extra"}, {"-root", "-h"}, {"validate", "-h"}} {
		if isHelpFlag(args) {
			t.Errorf("non-help args accepted: %v", args)
		}
	}
}

// The help lists every flag of the server with its environment variable,
// the commands and the flags of validate.
func TestHelpListsEverything(t *testing.T) {
	fset, _ := newServerFlags()
	var out strings.Builder
	printHelp(&out, fset)
	help := out.String()

	n := 0
	fset.VisitAll(func(f *flag.Flag) {
		n++
		if !strings.Contains(help, "\n  -"+f.Name) {
			t.Errorf("flag -%s missing", f.Name)
		}
		if !strings.Contains(help, envName(f.Name)) {
			t.Errorf("environment variable of -%s missing", f.Name)
		}
	})
	if n < 9 {
		t.Errorf("only %d flags registered", n)
	}
	for _, want := range []string{
		"vitrine validate", "vitrine passwd", "-v, --version", "-h, --help",
		"-strict", "VITRINE_ROOT", "VITRINE_TRUSTED_PROXY", `default ":8080"`,
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}
