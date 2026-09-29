// Package tree maps hrefs to the shared directory and decides what may be
// listed and served. It ports the h5fs rules ("managed" folders, hidden
// entries) and is the security boundary of vitrine: every file access
// goes through ResolveManagedPath or ResolveManagedFile.
package tree

import (
	"fmt"
	"io/fs"
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
	base     string   // href prefix, "" for the site root
	baseSegs []string
	follow   bool // serve symlinks leaving the root

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

// SetBase serves the tree below an href prefix (see NormalizeBase).
func (t *Tree) SetBase(base string) error {
	segs, err := splitHref(base + "/")
	if err != nil {
		return err
	}
	t.base, t.baseSegs = strings.TrimSuffix(base, "/"), segs
	return nil
}

// SetFollowSymlinks allows symbolic links whose target is outside the
// root. The rules still apply to the path below the root, and to the
// target if it is inside the root; excluded folders stay excluded.
func (t *Tree) SetFollowSymlinks(follow bool) { t.follow = follow }

// FollowSymlinks reports whether links leaving the root are followed.
func (t *Tree) FollowSymlinks() bool { return t.follow }

// Base returns the href prefix, "" for the site root.
func (t *Tree) Base() string { return t.base }

// Root returns the resolved root directory.
func (t *Tree) Root() string { return t.root }

// Config returns the configuration.
func (t *Tree) Config() *config.Config { return t.cfg }

// ToPath maps an absolute href (below the base) to a path below the root.
func (t *Tree) ToPath(href string) (string, error) {
	segs, err := splitHref(href)
	if err != nil {
		return "", err
	}
	if len(segs) < len(t.baseSegs) {
		return "", ErrBadHref
	}
	for i, b := range t.baseSegs {
		if segs[i] != b {
			return "", ErrBadHref
		}
	}
	return filepath.Join(append([]string{t.root}, segs[len(t.baseSegs):]...)...), nil
}

// ToHref maps a path below the root (textually) to its href.
func (t *Tree) ToHref(path string, trailingSlash bool) (string, bool) {
	rel, ok := t.rel(path, t.root)
	if !ok {
		return "", false
	}
	return t.base + joinHref(rel, trailingSlash), true
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
		// links leaving the root can't be served (unless followed), don't
		// list them (and the size and time of their targets) either
		if e.Type()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(filepath.Join(path, name))
			if err != nil || t.isExcluded(real) || !t.follow && !within(real, t.root) {
				continue
			}
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

// resolveDir resolves a folder that is not excluded and has no hidden
// ancestor, managed or not. The rules apply to the path below the root
// the client asked for and to the real target if it is inside the root;
// targets outside the root are only allowed when following symlinks.
func (t *Tree) resolveDir(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() || t.isExcluded(real) || !t.visiblePath(path) {
		return "", false
	}
	if !within(real, t.root) {
		return real, t.follow
	}
	if real != path && !t.visiblePath(real) {
		return "", false
	}
	return real, true
}

// visiblePath reports whether a path is below the root and neither it nor
// one of its ancestors (below the root) is hidden.
func (t *Tree) visiblePath(path string) bool {
	path = filepath.Clean(path)
	if !within(path, t.root) {
		return false
	}
	for p := path; p != t.root; {
		parent := filepath.Dir(p)
		if parent == p || t.isHiddenEntry(parent, filepath.Base(p)) {
			return false
		}
		p = parent
	}
	return true
}

// isAliasLink reports whether path is a link to a folder inside the root,
// which walks reach under its real path anyway.
func (t *Tree) isAliasLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	return err == nil && within(real, t.root)
}

func (t *Tree) isExcluded(real string) bool {
	for _, e := range t.excluded {
		if within(real, e) {
			return true
		}
	}
	return false
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
// be served: a regular file in a managed folder that is not hidden, both
// for the path the client asked for and, inside the root, for the target.
func (t *Tree) ResolveManagedFile(path string) (string, bool) {
	real, _, ok := t.resolveFile(path)
	return real, ok
}

// resolveFile is ResolveManagedFile that also returns the FileInfo the
// checks were made on.
func (t *Tree) resolveFile(path string) (string, fs.FileInfo, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, false
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.Mode().IsRegular() || t.isExcluded(real) {
		return "", nil, false
	}
	if !t.visiblePath(path) || !t.IsManagedPath(filepath.Dir(path)) {
		return "", nil, false
	}
	if !within(real, t.root) {
		return real, fi, t.follow
	}
	parent, ok := t.ResolveManagedPath(filepath.Dir(real))
	if !ok || t.isHiddenEntry(parent, filepath.Base(real)) {
		return "", nil, false
	}
	return real, fi, true
}

// OpenManagedFile opens a file that ResolveManagedFile accepts. The file
// is opened once and has to be the very file the checks were made on, so
// replacing it or one of its folders with a symbolic link between the
// checks and the open can't serve anything else. Inside the root the open
// is confined to the root.
func (t *Tree) OpenManagedFile(path string) (*os.File, bool) {
	real, fi, ok := t.resolveFile(path)
	if !ok {
		return nil, false
	}
	return t.openChecked(real, fi)
}

// openChecked opens real and verifies it is the file described by checked.
func (t *Tree) openChecked(real string, checked fs.FileInfo) (*os.File, bool) {
	var f *os.File
	var err error
	if rel, ok := t.relToRoot(real); ok {
		f, err = os.OpenInRoot(t.root, rel)
	} else {
		f, err = os.Open(real)
	}
	if err != nil {
		return nil, false
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(fi, checked) {
		f.Close()
		return nil, false
	}
	return f, true
}

// relToRoot returns the path of a resolved file below the root.
func (t *Tree) relToRoot(real string) (string, bool) {
	if !within(real, t.root) || real == t.root {
		return "", false
	}
	rel, err := filepath.Rel(t.root, real)
	return rel, err == nil
}

// ResolveUnmanagedIndex returns the real path of the first
// "view.unmanaged" file of a folder (e.g. index.html), which is served
// instead of a listing. The folder and the file follow the same rules as
// managed ones, the file must be a regular file directly in the folder.
func (t *Tree) ResolveUnmanagedIndex(dir string) (string, bool) {
	real, _, ok := t.resolveIndex(dir)
	return real, ok
}

// OpenUnmanagedIndex opens the file ResolveUnmanagedIndex finds, with the
// guarantees of OpenManagedFile.
func (t *Tree) OpenUnmanagedIndex(dir string) (*os.File, bool) {
	real, fi, ok := t.resolveIndex(dir)
	if !ok {
		return nil, false
	}
	return t.openChecked(real, fi)
}

func (t *Tree) resolveIndex(dir string) (string, fs.FileInfo, bool) {
	realDir, ok := t.resolveDir(dir)
	if !ok {
		return "", nil, false
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
			return real, fi, true
		}
	}
	return "", nil, false
}
