//go:build !linux && !darwin

package thumb

import "os"

// converterInput returns how a converter reads the source. There is no
// portable way to hand it the open descriptor here, so it opens the
// resolved path; the caller discards the result if the checked file
// changed in the meantime, which does not catch a swap that is undone.
func converterInput(_ *os.File, real string) (path string, extra []*os.File) {
	return real, nil
}
