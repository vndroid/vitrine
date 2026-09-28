//go:build unix

package tree

import "syscall"

// readable reports whether the server process may read path.
func readable(path string) bool {
	return syscall.Access(path, 4 /* R_OK */) == nil
}
