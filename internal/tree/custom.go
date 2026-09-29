package tree

import (
	"os"
	"path/filepath"
)

// FilePrefixes are the prefixes of custom header and footer files, the
// former h5fs prefix is still accepted.
var FilePrefixes = []string{"_vitrine", "_h5fs"}

var customExtensions = []string{"html", "md"}

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
			file, ok := t.resolveCustomFile(dir, fileName)
			if !ok {
				continue
			}
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			content, typ := string(data), ext
			part.Content, part.Type = &content, &typ
			return
		}
	}
}

// resolveCustomFile resolves a custom file (maybe a symbolic link). The
// target must be a custom file in a folder inside the root (those are
// usually hidden) or a regular visible file, so links can't expose files
// outside of the root or hidden files.
func (t *Tree) resolveCustomFile(dir, fileName string) (string, bool) {
	real, err := filepath.EvalSymlinks(filepath.Join(dir, fileName))
	if err != nil {
		return "", false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.Mode().IsRegular() || !readable(real) {
		return "", false
	}
	if filepath.Base(real) == fileName {
		if _, ok := t.ResolveManagedPath(filepath.Dir(real)); ok {
			return real, true
		}
		return "", false
	}
	return t.ResolveManagedFile(real)
}
