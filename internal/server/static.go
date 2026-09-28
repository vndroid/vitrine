package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// serveFile serves a shared file (already resolved and checked by the
// tree) with Range, If-Modified-Since and HEAD support.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, realPath string) {
	f, err := os.Open(realPath)
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
	ext := strings.ToLower(filepath.Ext(realPath))
	h := w.Header()
	switch {
	case plainTypes[ext]:
		h.Set("Content-Type", "text/plain; charset=utf-8")
	case activeTypes[ext]:
		h.Set("Content-Security-Policy", "sandbox")
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}
