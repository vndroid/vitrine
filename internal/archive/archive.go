// Package archive builds tar and zip packages of shared folders and files
// for the packaged download, with the limits of h5fs.
package archive

import (
	"archive/tar"
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/vndroid/vitrine/internal/tree"
)

// Limits bound what a single package may contain.
type Limits struct {
	MaxFiles     int
	MaxDirs      int
	MaxDepth     int
	MaxBytes     int64
	ScanDuration time.Duration
}

// DefaultLimits are the h5fs limits.
var DefaultLimits = Limits{
	MaxFiles:     5000,
	MaxDirs:      1000,
	MaxDepth:     32,
	MaxBytes:     2 << 30,
	ScanDuration: 2 * time.Second,
}

// ErrRejected means the selection is invalid or exceeds the limits.
var ErrRejected = errors.New("archive: selection rejected")

// Entry is a folder or file of a package.
type Entry struct {
	Real    string // resolved path on disk
	Name    string // name inside the package, relative, "/" separated
	ModTime time.Time
	Size    int64
	IsDir   bool
}

// Plan lists the entries of a package.
type Plan struct {
	Entries []Entry
	Bytes   int64
}

type collector struct {
	tr       *tree.Tree
	limits   Limits
	base     string // textual base path
	plan     Plan
	dirs     map[string]bool
	files    map[string]bool
	nDirs    int
	nFiles   int
	deadline time.Time
	aborted  bool
}

// Collect resolves the selected hrefs below the folder at baseHref. With
// no selection the whole base folder is packaged. Entries are named
// relative to the base folder; anything outside of it, hidden, unmanaged
// or reached through a file symlink is left out.
func Collect(tr *tree.Tree, baseHref string, hrefs []string, limits Limits) (*Plan, error) {
	base, err := tr.ToPath(baseHref)
	if err != nil || !tr.IsManagedPath(base) {
		return nil, ErrRejected
	}
	c := &collector{
		tr: tr, limits: limits, base: base,
		dirs: map[string]bool{}, files: map[string]bool{},
		deadline: time.Now().Add(limits.ScanDuration),
	}

	requested := 0
	for _, href := range hrefs {
		if strings.TrimSpace(href) == "" {
			continue
		}
		requested++
		c.addHref(href)
	}
	if requested == 0 && !c.aborted {
		c.addDir(base, "", 0)
	}
	if c.aborted || len(c.plan.Entries) == 0 {
		return nil, ErrRejected
	}
	return &c.plan, nil
}

func (c *collector) addHref(href string) {
	real, err := c.tr.ToPath(strings.TrimRight(href, "/"))
	if err != nil {
		return
	}
	parent, name := filepath.Dir(real), filepath.Base(real)
	if !c.tr.IsManagedPath(parent) || c.tr.IsHidden(name) {
		return
	}
	rel, err := filepath.Rel(c.base, real)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return
	}
	archived := filepath.ToSlash(rel)
	fi, err := os.Stat(real)
	if err != nil {
		return
	}
	if fi.IsDir() {
		c.addDir(real, archived, 0)
	} else {
		c.addFile(real, archived)
	}
}

func (c *collector) quotaExceeded() bool {
	if c.aborted || time.Now().After(c.deadline) {
		c.aborted = true
	}
	return c.aborted
}

func (c *collector) addFile(real, archived string) {
	if c.quotaExceeded() {
		return
	}
	// like h5fs, links to files are only packaged when following symlinks
	if fi, err := os.Lstat(real); err != nil || fi.Mode()&os.ModeSymlink != 0 && !c.tr.FollowSymlinks() {
		return
	}
	src, ok := c.tr.ResolveManagedFile(real)
	if !ok || c.files[src] {
		return
	}
	fi, err := os.Stat(src)
	if err != nil {
		return
	}
	if c.nFiles >= c.limits.MaxFiles || fi.Size() > c.limits.MaxBytes-c.plan.Bytes {
		c.aborted = true
		return
	}
	f, err := os.Open(src)
	if err != nil {
		return // not readable
	}
	f.Close()
	c.files[src] = true
	c.nFiles++
	c.plan.Bytes += fi.Size()
	c.plan.Entries = append(c.plan.Entries, Entry{
		Real: src, Name: archived, ModTime: fi.ModTime(), Size: fi.Size(),
	})
}

func (c *collector) addDir(real, archived string, depth int) {
	if c.quotaExceeded() || depth > c.limits.MaxDepth {
		c.aborted = true
		return
	}
	resolved, ok := c.tr.ResolveManagedPath(real)
	if !ok || c.dirs[resolved] {
		return
	}
	if c.nDirs >= c.limits.MaxDirs {
		c.aborted = true
		return
	}
	c.dirs[resolved] = true
	c.nDirs++
	if archived != "" {
		var mtime time.Time
		if fi, err := os.Stat(resolved); err == nil {
			mtime = fi.ModTime()
		}
		c.plan.Entries = append(c.plan.Entries, Entry{Real: resolved, Name: archived, ModTime: mtime, IsDir: true})
	}
	// continue below the path, not the link target, so the rules for
	// followed links apply to the entries as well
	for _, name := range c.tr.ReadDir(real) {
		if c.quotaExceeded() {
			return
		}
		child := filepath.Join(real, name)
		childName := path.Join(archived, name)
		if fi, err := os.Stat(child); err == nil && fi.IsDir() {
			c.addDir(child, childName, depth+1)
		} else {
			c.addFile(child, childName)
		}
	}
}

func tarHeader(e Entry) *tar.Header {
	h := &tar.Header{
		Name:    e.Name,
		ModTime: e.ModTime.Truncate(time.Second),
		Mode:    0o644,
		Size:    e.Size,
	}
	if e.IsDir {
		h.Name += "/"
		h.Typeflag = tar.TypeDir
		h.Mode = 0o755
		h.Size = 0
	} else {
		h.Typeflag = tar.TypeReg
	}
	return h
}

// TarSize returns the exact size of the tar WriteTar produces, so the
// download can announce a Content-Length.
func (p *Plan) TarSize() (int64, error) {
	var total int64
	for _, e := range p.Entries {
		cw := &countWriter{}
		if err := tar.NewWriter(cw).WriteHeader(tarHeader(e)); err != nil {
			return 0, err
		}
		total += cw.n
		if !e.IsDir {
			total += (e.Size + 511) / 512 * 512
		}
	}
	return total + 1024, nil // end of archive: two zero blocks
}

// WriteTar writes the package as tar.
func (p *Plan) WriteTar(w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, e := range p.Entries {
		if err := tw.WriteHeader(tarHeader(e)); err != nil {
			return err
		}
		if !e.IsDir {
			if err := copyFile(tw, e); err != nil {
				return err
			}
		}
	}
	return tw.Close()
}

// WriteZip writes the package as zip (stored, media rarely compresses).
func (p *Plan) WriteZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, e := range p.Entries {
		h := &zip.FileHeader{Name: e.Name, Method: zip.Store, Modified: e.ModTime}
		if e.IsDir {
			h.Name += "/"
			h.SetMode(0o755 | os.ModeDir)
		} else {
			h.SetMode(0o644)
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if !e.IsDir {
			if err := copyFile(fw, e); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

// copyFile copies exactly the planned size, so a file that changed since
// planning can't break the announced length.
func copyFile(w io.Writer, e Entry) error {
	f, err := os.Open(e.Real)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.CopyN(w, f, e.Size)
	return err
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
