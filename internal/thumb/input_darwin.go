package thumb

import "os"

// converterInput returns how a converter reads the checked source: through
// the open descriptor, which it inherits as descriptor 3. Whatever happens
// to the path in the share, it reads the file that was checked. The offset
// of the descriptor is shared with the converter.
func converterInput(source *os.File, _ string) (path string, extra []*os.File) {
	return "/dev/fd/3", []*os.File{source}
}
