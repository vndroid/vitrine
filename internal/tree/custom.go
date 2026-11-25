package tree

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// FilePrefixes are the prefixes of custom header and footer files.
var FilePrefixes = []string{"_vitrine"}

var customExtensions = []string{"html", "md"}

const maxCustomBytes = 1 << 20

// CustomPart is a custom header or footer.
type CustomPart struct {
	Content *string `json:"content"`
	Type    *string `json:"type"`
}

// Customizations holds the custom header and footer of a folder.
type Customizations struct {
	Header CustomPart `json:"header"`
	Footer CustomPart `json:"footer"`
}

// Custom finds the header and footer of the folder at href: "header" and
// "footer" files in the folder itself, then "headers" and "footers" files
// in the folder and its parents.
func (t *Tree) Custom(href string) Customizations {
	var c Customizations
	if !t.cfg.Bool("custom.enabled", false) {
		return c
	}
	path, err := t.ToPath(href)
	if err != nil {
		return c
	}
	if !t.IsManagedPath(path) {
		return c
	}
	// walk up the paths below the root, not the targets of links
	dir := path

	t.readCustom(dir, "header", &c.Header)
	t.readCustom(dir, "footer", &c.Footer)
	for c.Header.Content == nil || c.Footer.Content == nil {
		if c.Header.Content == nil {
			t.readCustom(dir, "headers", &c.Header)
		}
		if c.Footer.Content == nil {
			t.readCustom(dir, "footers", &c.Footer)
		}
		// vitrine never leaves the root, whatever "custom.stopSearchingAtRoot" says
		if dir == t.root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return c
}

func (t *Tree) readCustom(dir, name string, part *CustomPart) {
	for _, prefix := range FilePrefixes {
		for _, ext := range customExtensions {
			fileName := prefix + "." + name + "." + ext
			real, info, ok := t.resolveCustomFile(dir, fileName)
			if !ok {
				continue
			}
			if info.Size() > maxCustomBytes {
				t.warnLargeCustom(real, info.Size())
				continue
			}
			file, ok := t.OpenChecked(real, info)
			if !ok {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(file, maxCustomBytes+1))
			file.Close()
			if err != nil || len(data) > maxCustomBytes {
				continue
			}
			content, typ := string(data), ext
			part.Content, part.Type = &content, &typ
			return
		}
	}
}

// warnLargeCustom logs once per file that a custom file is ignored: it is
// read for every listing of its folder.
func (t *Tree) warnLargeCustom(path string, size int64) {
	if _, seen := t.largeCustom.LoadOrStore(path, true); !seen {
		slog.Warn("custom file ignored, it is too large", "file", path, "size", size, "limit", maxCustomBytes)
	}
}

// resolveCustomFile resolves a custom file (maybe a symbolic link). The
// target must be a custom file in a folder inside the root (those are
// usually hidden) or a regular visible file, so links can't expose files
// outside of the root or hidden files.
func (t *Tree) resolveCustomFile(dir, fileName string) (string, fs.FileInfo, bool) {
	real, err := filepath.EvalSymlinks(filepath.Join(dir, fileName))
	if err != nil || !within(real, t.root) {
		return "", nil, false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.Mode().IsRegular() {
		return "", nil, false
	}
	if filepath.Base(real) == fileName {
		if _, ok := t.ResolveManagedPath(filepath.Dir(real)); ok {
			return real, fi, true
		}
		return "", nil, false
	}
	target, checked, ok := t.ResolveManagedFileInfo(real)
	if !ok || !within(target, t.root) {
		return "", nil, false
	}
	return target, checked, true
}
