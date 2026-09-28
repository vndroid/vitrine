// Package tree maps hrefs to the shared directory and decides what may be
// listed and served. It ports the h5fs rules ("managed" folders, hidden
// entries) and is the security boundary of vitrine: every file access
// goes through ResolveManagedPath or ResolveManagedFile.
package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vndroid/vitrine/internal/config"
)

// Tree is the shared directory.
type Tree struct {
	root     string // absolute, symlinks resolved
	cfg      *config.Config
	excluded []string // resolved paths never managed (cache, config)

	sizeMu    sync.Mutex
	sizeCache map[string]sizeEntry
}

type sizeEntry struct {
	size    *int64
	expires time.Time
}

// New returns the tree rooted at root. Paths in excluded (e.g. the cache
// and config directories) are never listed or served, even inside root.
func New(root string, cfg *config.Config, excluded ...string) (*Tree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", root)
	}
	t := &Tree{root: real, cfg: cfg, sizeCache: map[string]sizeEntry{}}
	for _, e := range excluded {
		if e == "" {
			continue
		}
		if abs, err := filepath.Abs(e); err == nil {
			if r, err := filepath.EvalSymlinks(abs); err == nil {
				abs = r
			}
			t.excluded = append(t.excluded, abs)
		}
	}
	return t, nil
}

// Root returns the resolved root directory.
func (t *Tree) Root() string { return t.root }

// Config returns the configuration.
func (t *Tree) Config() *config.Config { return t.cfg }

// ToPath maps an absolute href to a path below the root.
func (t *Tree) ToPath(href string) (string, error) {
	segs, err := splitHref(href)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{t.root}, segs...)...), nil
}

// ToHref maps a path below the root (textually) to its href.
func (t *Tree) ToHref(path string, trailingSlash bool) (string, bool) {
	rel, ok := t.rel(path, t.root)
	if !ok {
		return "", false
	}
	return joinHref(rel, trailingSlash), true
}

// rel returns the path segments of path below base.
func (t *Tree) rel(path, base string) ([]string, bool) {
	if !within(path, base) {
		return nil, false
	}
	r, err := filepath.Rel(base, path)
	if err != nil {
		return nil, false
	}
	if r == "." {
		return nil, true
	}
	return strings.Split(filepath.ToSlash(r), "/"), true
}

func within(path, base string) bool {
	return path == base || strings.HasPrefix(path, strings.TrimSuffix(base, string(filepath.Separator))+string(filepath.Separator))
}

// IsHidden applies the "view.hidden" patterns to a single name.
func (t *Tree) IsHidden(name string) bool { return t.cfg.IsHidden(name) }

// isHiddenEntry applies the hidden rules to an entry of a resolved folder:
// the plain name and the href of the entry are both matched.
func (t *Tree) isHiddenEntry(resolvedParent, name string) bool {
	if t.cfg.IsHidden(name) {
		return true
	}
	if href, ok := t.ToHref(resolvedParent, true); ok {
		return t.cfg.IsHidden(href + RawURLEncode(name))
	}
	return false
}

// ReadDir lists the visible entry names of a folder, sorted.
func (t *Tree) ReadDir(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	href, hrefOK := t.ToHref(path, true)
	hideIf403 := t.cfg.Bool("view.hideIf403", false)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if t.cfg.IsHidden(name) || hrefOK && t.cfg.IsHidden(href+RawURLEncode(name)) {
			continue
		}
		if hideIf403 && !readable(filepath.Join(path, name)) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveManagedPath resolves a folder and returns its real path if it may
// be listed: it is inside the root, not excluded, contains none of the
// "view.unmanaged" files and neither it nor one of its ancestors is hidden.
func (t *Tree) ResolveManagedPath(path string) (string, bool) {
	real, ok := t.resolveDir(path)
	if !ok {
		return "", false
	}
	for _, name := range t.cfg.Strings("view.unmanaged") {
		if _, err := os.Lstat(filepath.Join(real, name)); err == nil {
			return "", false
		}
	}
	return real, true
}

// resolveDir resolves a folder inside the root that is not excluded and
// has no hidden ancestor, managed or not.
func (t *Tree) resolveDir(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() || !within(real, t.root) {
		return "", false
	}
	for _, e := range t.excluded {
		if within(real, e) {
			return "", false
		}
	}
	for p := real; p != t.root; {
		parent := filepath.Dir(p)
		if parent == p {
			return "", false
		}
		if t.isHiddenEntry(parent, filepath.Base(p)) {
			return "", false
		}
		p = parent
	}
	return real, true
}

// IsManagedPath reports whether a folder may be listed.
func (t *Tree) IsManagedPath(path string) bool {
	_, ok := t.ResolveManagedPath(path)
	return ok
}

// IsManagedHref reports whether the folder at href may be listed.
func (t *Tree) IsManagedHref(href string) bool {
	path, err := t.ToPath(href)
	return err == nil && t.IsManagedPath(path)
}

// ResolveManagedFile resolves a file and returns its real path if it may
// be served: a regular file in a managed folder that is not hidden.
func (t *Tree) ResolveManagedFile(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	parent, ok := t.ResolveManagedPath(filepath.Dir(real))
	if !ok || t.isHiddenEntry(parent, filepath.Base(real)) {
		return "", false
	}
	return real, true
}

// ResolveUnmanagedIndex returns the real path of the first
// "view.unmanaged" file of a folder (e.g. index.html), which is served
// instead of a listing. The folder and the file follow the same rules as
// managed ones, the file must be a regular file directly in the folder.
func (t *Tree) ResolveUnmanagedIndex(dir string) (string, bool) {
	realDir, ok := t.resolveDir(dir)
	if !ok {
		return "", false
	}
	for _, name := range t.cfg.Strings("view.unmanaged") {
		if name == "" || strings.ContainsAny(name, "/\\") || t.isHiddenEntry(realDir, name) {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(realDir, name))
		if err != nil || filepath.Dir(real) != realDir {
			continue
		}
		if fi, err := os.Stat(real); err == nil && fi.Mode().IsRegular() {
			return real, true
		}
	}
	return "", false
}
