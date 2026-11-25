//go:build !linux && !darwin

package thumb

import (
	"io"
	"os"
)

func addSourceIdentity(w io.Writer, file *os.File, info os.FileInfo) error {
	// os.FileInfo does not expose a portable file ID on these systems.
	if info.Size() > maxExternalSourceBytes {
		return errSource
	}
	_, err := io.Copy(w, io.NewSectionReader(file, 0, info.Size()))
	return err
}

func sameChangeTime(_, _ os.FileInfo) bool { return true }
