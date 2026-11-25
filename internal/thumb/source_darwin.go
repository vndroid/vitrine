package thumb

import (
	"fmt"
	"os"
	"syscall"
)

// fileIdentity is the inode and the change time of a file.
func fileIdentity(info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d:%d", stat.Ino, stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
}

func sameChangeTime(a, b os.FileInfo) bool {
	return a.Sys().(*syscall.Stat_t).Ctimespec == b.Sys().(*syscall.Stat_t).Ctimespec
}
