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

// sourceID is the cache key of a source version. It includes the resolved
// path and the file identity as well as the size and time, so replacing a
// file invalidates its previous thumbnail. The device number is left out:
// it can change on a reboot or remount (NFS, FUSE, ...), which would throw
// the whole cache away. The versioned prefix prevents reuse of thumbnails
// generated before source validation.
func sourceID(path string, info os.FileInfo) string {
	h := sha256.New224()
	fmt.Fprintf(h, "checked-source-v2\x00%s\x00%d\x00%d\x00%s", path, info.Size(), info.ModTime().UnixNano(), fileIdentity(info))
	return hex.EncodeToString(h.Sum(nil))
}
