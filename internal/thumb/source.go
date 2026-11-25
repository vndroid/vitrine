package thumb

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

func sameSource(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && sameChangeTime(a, b)
}

// The versioned key prevents reuse of thumbnails generated before source
// validation. Include the resolved path and file identity as well as the
// size and time, so replacing a file invalidates its previous thumbnail.
func sourceID(path string, file *os.File, info os.FileInfo) (string, error) {
	h := sha256.New224()
	fmt.Fprintf(h, "checked-source-v1\x00%s\x00%d\x00%d\x00", path, info.Size(), info.ModTime().UnixNano())
	if err := addSourceIdentity(h, file, info); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
