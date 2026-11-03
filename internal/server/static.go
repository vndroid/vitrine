package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// asset is an embedded frontend file with a content hash as ETag.
type asset struct {
	data []byte
	etag string
}

type assets struct {
	fsys  fs.FS
	mu    sync.Mutex
	cache map[string]*asset
}

func (a *assets) get(name string) (*asset, bool) {
	if !fs.ValidPath(name) {
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if as, ok := a.cache[name]; ok {
		return as, true
	}
	data, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		return nil, false
	}
	sum := sha256.Sum256(data)
	as := &asset{data: data, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
	a.cache[name] = as
	return as, true
}

// serveAsset serves an embedded frontend file. The files are not
// fingerprinted, so clients revalidate them with the ETag.
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	as, ok := s.assets.get(name)
	if !ok {
		s.notFound(w)
		return
	}
	h := w.Header()
	h.Set("ETag", as.etag)
	h.Set("Cache-Control", "no-cache")
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		h.Set("Content-Type", ct)
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(as.data))
}

// serveExt serves user resources ("resources" option) from the ext folder
// of the config directory.
func (s *Server) serveExt(w http.ResponseWriter, r *http.Request, name string) {
	if s.extDir == "" || !fs.ValidPath(name) {
		s.notFound(w)
		return
	}
	// OpenInRoot also keeps symbolic links from leaving the folder
	f, err := os.OpenInRoot(s.extDir, name)
	if err != nil {
		s.notFound(w)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		s.notFound(w)
		return
	}
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// activeTypes can run scripts when opened; shared files of these types
// are sandboxed, so they can't act on behalf of the vitrine origin.
var activeTypes = map[string]bool{
	".html": true, ".htm": true, ".xhtml": true, ".shtml": true,
	".svg": true, ".svgz": true, ".xml": true, ".xsl": true, ".xslt": true,
}

// plainTypes are served as text instead of being handed to a browser
// plugin or offered as executable content.
var plainTypes = map[string]bool{".php": true, ".phar": true}

// serveFile serves a shared file (opened by the tree, which checked it)
// with Range, If-Modified-Since and HEAD support, and closes it.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, f *os.File) {
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		s.notFound(w)
		return
	}
	ext := strings.ToLower(filepath.Ext(f.Name()))
	h := w.Header()
	ct := mime.TypeByExtension(ext)
	switch {
	case plainTypes[ext]:
		ct = "text/plain; charset=utf-8"
	case ct == "":
		// no known type (.md, no extension, ...): ServeContent would sniff
		// the content, and a file starting with "<!DOCTYPE html>" would be
		// a page of the origin of vitrine. The type is set here instead.
		ct = sniffedType(f)
	}
	h.Set("Content-Type", ct)
	if activeTypes[ext] || isActiveType(ct) {
		h.Set("Content-Security-Policy", "sandbox; "+frameAncestors)
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// isActiveType reports whether a browser opens the type as a document that
// can run scripts: HTML, XHTML, SVG and the XML types.
func isActiveType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "text/html" || mt == "text/xml" || mt == "text/xsl" ||
		mt == "application/xml" || strings.HasSuffix(mt, "+xml")
}

// sniffedType is the type of the content of a file without a known
// extension, like http.ServeContent finds it, except that content which a
// browser would open as a document (HTML, XML) is plain text. The file is
// left at its start.
func sniffedType(f *os.File) string {
	var head [512]byte
	n, _ := io.ReadFull(f, head[:])
	f.Seek(0, io.SeekStart)
	ct := http.DetectContentType(head[:n])
	if isActiveType(ct) {
		return "text/plain; charset=utf-8"
	}
	return ct
}
