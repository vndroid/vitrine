//go:build !unix

package tree

import "os"

// readable reports whether the server process may read path.
func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
