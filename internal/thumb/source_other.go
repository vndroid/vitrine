//go:build !linux && !darwin

package thumb

import "os"

// os.FileInfo does not expose a portable file ID on these systems: the
// path, size and time identify a version.
func fileIdentity(os.FileInfo) string { return "" }

func sameChangeTime(_, _ os.FileInfo) bool { return true }
