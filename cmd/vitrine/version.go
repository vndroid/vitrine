package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

// Version of vitrine, raised with every change worth a version (see
// CHANGELOG.md): patch for small features and fixes, minor for larger
// ones. Releases are tagged by hand.
const Version = "0.4.8"

// Build information, set with -ldflags "-X main.<name>=...". Revision and
// tags fall back to what the Go toolchain records in the binary.
var (
	version   = "" // overrides Version, e.g. a git tag
	buildDate = "" // RFC 3339, UTC
	branch    = ""
	revision  = ""
)

// buildVersion returns the version, without the "v" of a git tag.
func buildVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	return Version
}

// buildInfo is what "vitrine --version" prints.
type buildInfo struct {
	Version, Branch, Revision, Built, Platform, Runtime, Tags string
}

func currentBuild() buildInfo {
	b := buildInfo{
		Version:  buildVersion(),
		Branch:   branch,
		Revision: revision,
		Built:    buildDate,
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Runtime:  runtime.Version(),
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if b.Revision == "" {
					b.Revision = s.Value
				}
			case "-tags":
				b.Tags = s.Value
			}
		}
	}
	for _, v := range []*string{&b.Branch, &b.Revision, &b.Built} {
		if *v == "" {
			*v = "unknown"
		}
	}
	if b.Tags == "" {
		b.Tags = "none"
	}
	return b
}

// isVersionFlag reports whether the arguments ask for the version.
func isVersionFlag(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "-v", "--version", "-version", "version":
		return true
	}
	return false
}

// format renders the build information, with colored labels for a
// terminal.
func (b buildInfo) format(colored bool) string {
	head := fmt.Sprintf("vitrine, version %s (branch: %s, revision: %s)", b.Version, b.Branch, b.Revision)
	if colored {
		head = bold(head)
	}
	var sb strings.Builder
	sb.WriteString(head + "\n")
	for _, f := range [...][2]string{
		{"Built", b.Built},
		{"Platform", b.Platform},
		{"Runtime", b.Runtime},
		{"Tags", b.Tags},
	} {
		label, value := f[0]+":", f[1]
		if colored {
			label, value = gray(label), bold(value)
		}
		sb.WriteString("  " + label + " " + value + "\n")
	}
	return sb.String()
}

func gray(s string) string { return "\x1b[90m" + s + "\x1b[0m" }
func bold(s string) string { return "\x1b[1m" + s + "\x1b[0m" }

// colorEnabled reports whether f is a terminal that wants colors.
func colorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
