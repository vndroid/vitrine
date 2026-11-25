package thumb

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

func addSourceIdentity(w io.Writer, _ *os.File, info os.FileInfo) error {
	stat := info.Sys().(*syscall.Stat_t)
	_, err := fmt.Fprintf(w, "%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec)
	return err
}

func sameChangeTime(a, b os.FileInfo) bool {
	return a.Sys().(*syscall.Stat_t).Ctim == b.Sys().(*syscall.Stat_t).Ctim
}
