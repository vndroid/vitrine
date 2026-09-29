// Package server is the vitrine HTTP server: it serves the frontend, the
// shared files and the h5fs compatible API.
package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/vndroid/vitrine/internal/archive"
	"github.com/vndroid/vitrine/internal/auth"
	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/thumb"
	"github.com/vndroid/vitrine/internal/tree"
)

// URL prefixes reserved by vitrine; the frontend reads PublicHref from the
// setup, so it works unchanged with the h5fs layout below it.
const (
	ReservedPrefix = "/_vitrine/"
	PublicHref     = "/_vitrine/public/"
	ThumbsHref     = "/_vitrine/thumbs/"
)

// Options configure a Server.
type Options struct {
	Tree           *tree.Tree
	Config         *config.Config
	Public         fs.FS  // frontend files
	ConfigDir      string // for the "ext" folder, may be empty
	CacheDir       string
	Version        string
	TrustedProxies []netip.Prefix
	Logger         *slog.Logger
}

// Server handles all requests.
type Server struct {
	tree     *tree.Tree
	cfg      *config.Config
	assets   *assets
	public   fs.FS
	extDir   string
	cacheDir string
	version  string
	trusted  []netip.Prefix
	log      *slog.Logger
	commands atomic.Pointer[map[string]bool]
	sessions *auth.Sessions
	throttle *auth.Throttle
	slots    *archive.Slots
	thumb    *thumb.Service
}

// New creates a server.
func New(o Options) *Server {
	s := &Server{
		tree:     o.Tree,
		cfg:      o.Config,
		assets:   &assets{fsys: o.Public, cache: map[string]*asset{}},
		public:   o.Public,
		cacheDir: o.CacheDir,
		version:  o.Version,
		trusted:  o.TrustedProxies,
		log:      o.Logger,
		sessions: auth.NewSessions(),
		throttle: auth.NewThrottle(),
		slots:    archive.NewSlots(),
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if o.ConfigDir != "" {
		s.extDir = filepath.Join(o.ConfigDir, "ext")
	}
	s.cfg.SetLoginEnabled(auth.LoginEnabled(s.cfg.Passhash()))
	s.detectCommands()
	if o.CacheDir != "" {
		ts, err := thumb.New(o.Tree, o.Config, o.CacheDir, s.hasCommand, s.log)
		if err != nil {
			s.log.Warn("thumbnails disabled", "err", err)
		} else {
			s.thumb = ts
		}
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method == http.MethodPost {
		s.handleAPI(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	href := r.URL.EscapedPath()
	if rest, ok := strings.CutPrefix(href, ReservedPrefix); ok {
		s.serveReserved(w, r, rest)
		return
	}
	s.serveShared(w, r, href)
}

// serveReserved serves the frontend, the info page and thumbnails.
func (s *Server) serveReserved(w http.ResponseWriter, r *http.Request, rest string) {
	name, err := unescape(rest)
	if err != nil {
		s.notFound(w)
		return
	}
	switch {
	case name == "public/" || name == "public" || name == "":
		if !strings.HasSuffix(r.URL.Path, "/") {
			redirectSlash(w, r)
			return
		}
		s.renderPage(w, r, "info", "")
	case strings.HasPrefix(name, "public/ext/"):
		s.serveExt(w, r, strings.TrimPrefix(name, "public/ext/"))
	case strings.HasPrefix(name, "public/"):
		s.serveAsset(w, r, strings.TrimPrefix(name, "public/"))
	case strings.HasPrefix(name, "thumbs/"):
		s.serveThumb(w, r, strings.TrimPrefix(name, "thumbs/"))
	default:
		s.notFound(w)
	}
}

// serveShared serves the index page of managed folders, the index file
// of unmanaged folders and shared files.
func (s *Server) serveShared(w http.ResponseWriter, r *http.Request, href string) {
	p, err := s.tree.ToPath(href)
	if err != nil {
		s.notFound(w)
		return
	}
	fi, err := os.Stat(p)
	if err != nil {
		s.notFound(w)
		return
	}
	if fi.IsDir() {
		if !strings.HasSuffix(href, "/") {
			redirectSlash(w, r)
			return
		}
		if real, ok := s.tree.ResolveManagedPath(p); ok {
			s.renderPage(w, r, "index", real)
			return
		}
		if index, ok := s.tree.ResolveUnmanagedIndex(p); ok {
			s.serveFile(w, r, index)
			return
		}
		s.notFound(w)
		return
	}
	if real, ok := s.tree.ResolveManagedFile(p); ok {
		s.serveFile(w, r, real)
		return
	}
	s.notFound(w)
}

// redirectSlash redirects folder requests without trailing slash. The
// relative Location keeps the scheme and host the client used, which
// matters behind TLS terminating proxies.
func redirectSlash(w http.ResponseWriter, r *http.Request) {
	target := r.URL.EscapedPath() + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusMovedPermanently)
}

func (s *Server) notFound(w http.ResponseWriter) {
	http.Error(w, "404 not found", http.StatusNotFound)
}

func unescape(p string) (string, error) {
	var b strings.Builder
	for _, seg := range strings.SplitAfter(p, "/") {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			return "", err
		}
		b.WriteString(dec)
	}
	return b.String(), nil
}

var checkedCommands = []string{"avconv", "convert", "du", "ffmpeg", "gm", "magick"}

func (s *Server) detectCommands() {
	cmds := map[string]bool{}
	for _, c := range checkedCommands {
		_, err := exec.LookPath(c)
		cmds[c] = err == nil
	}
	s.commands.Store(&cmds)
}

func (s *Server) hasCommand(name string) bool {
	return (*s.commands.Load())[name]
}

// setupInfo is the "setup" the client reads; the admin keys feed the
// checks of the info page.
func (s *Server) setupInfo(admin bool) map[string]any {
	setup := map[string]any{
		"AS_ADMIN":    admin,
		"PUBLIC_HREF": PublicHref,
		"ROOT_HREF":   "/",
	}
	if !admin {
		return setup
	}
	setup["VERSION"] = s.version
	setup["GO_VERSION"] = runtime.Version()
	setup["PLATFORM"] = runtime.GOOS + "/" + runtime.GOARCH
	setup["HAS_WRITABLE_CACHE"] = s.cacheDir != "" && dirWritable(s.cacheDir)
	for _, c := range checkedCommands {
		setup["HAS_CMD_"+strings.ToUpper(c)] = s.hasCommand(c)
	}
	return setup
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

var themeExtensions = map[string]bool{".svg": true, ".png": true, ".jpg": true}

// themeIcons maps icon names to "theme/file" for the configured theme.
func (s *Server) themeIcons() map[string]string {
	theme := s.cfg.String("view.theme", "")
	icons := map[string]string{}
	if theme == "" || strings.ContainsAny(theme, "/\\") || theme == "." || theme == ".." {
		return icons
	}
	entries, err := fs.ReadDir(s.public, "images/themes/"+theme)
	if err != nil {
		return icons
	}
	for _, e := range entries {
		ext := path.Ext(e.Name())
		if !e.IsDir() && themeExtensions[ext] {
			icons[strings.TrimSuffix(e.Name(), ext)] = theme + "/" + e.Name()
		}
	}
	return icons
}
