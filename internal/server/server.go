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

// The first path segment below the base path (--base-path) that belongs to
// vitrine is tree.ReservedName: "/-/admin" is the admin page, everything
// else below "/-/" is not found. The tree hides a shared entry of this
// name, so it can't be listed or served.

// AdminPath is the admin page, below the base path.
const AdminPath = "/-/admin"

// Health endpoints, below the base path, like the ones of Prometheus:
// HealthyPath answers 200 while vitrine serves requests, ReadyPath only
// while the shared folder is accessible (503 otherwise). Both are public,
// carry no details and are left out of the access log.
const (
	HealthyPath = "/-/healthy"
	ReadyPath   = "/-/ready"
)

// URL prefixes of the frontend and thumbnails, below the base path; the
// frontend reads the public href from the setup.
const (
	ReservedPrefix = "/_vitrine/"
	PublicHref     = "/_vitrine/public/"
	ThumbsHref     = "/_vitrine/thumbs/"
)

// frameAncestors is the CSP that only lets vitrine frame itself.
const frameAncestors = "frame-ancestors 'self'"

// poweredBy is the value of the X-Powered-By header, which follows the
// version of the program.
func poweredBy(version string) string {
	if version == "" {
		return "vitrine"
	}
	return "vitrine/" + version
}

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
	// poweredBy is the value of the X-Powered-By header: "vitrine/<version>"
	poweredBy string
	trusted   []netip.Prefix
	log       *slog.Logger
	commands  atomic.Pointer[map[string]bool]
	// rootCheck tests that the shared folder is accessible; readyBusy is
	// set while a check runs, readyState remembers the last answer
	rootCheck  func() error
	readyBusy  atomic.Bool
	readyState atomic.Int32
	sessions   *auth.Sessions
	throttle   *auth.Throttle
	slots      *archive.Slots
	// thumbCalls limits concurrent thumbnail requests
	thumbCalls *archive.Slots
	thumb      *thumb.Service
}

// New creates a server.
func New(o Options) *Server {
	s := &Server{
		tree:       o.Tree,
		cfg:        o.Config,
		assets:     &assets{fsys: o.Public, cache: map[string]*asset{}},
		public:     o.Public,
		cacheDir:   o.CacheDir,
		version:    o.Version,
		poweredBy:  poweredBy(o.Version),
		trusted:    o.TrustedProxies,
		log:        o.Logger,
		sessions:   auth.NewSessions(),
		throttle:   auth.NewThrottle(),
		slots:      archive.NewSlots(),
		thumbCalls: archive.NewSlots(),
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if o.ConfigDir != "" {
		s.extDir = filepath.Join(o.ConfigDir, "ext")
	}
	s.cfg.SetLoginCheck(auth.LoginEnabled)
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
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Powered-By", s.poweredBy)
	// no framing by other sites (clickjacking); the frontend itself frames
	// same-origin previews
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Content-Security-Policy", frameAncestors)

	if r.Method == http.MethodPost {
		if _, ok := s.relToBase(r.URL.EscapedPath()); !ok {
			s.notFound(w)
			return
		}
		s.handleAPI(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	href := r.URL.EscapedPath()
	rel, ok := s.relToBase(href)
	if !ok {
		if href == s.tree.Base() {
			redirectSlash(w, r)
		} else {
			s.notFound(w)
		}
		return
	}
	if isReservedName(rel) {
		s.serveBuiltin(w, r, rel)
		return
	}
	if rest, ok := strings.CutPrefix(rel, ReservedPrefix); ok {
		s.serveReserved(w, r, rest)
		return
	}
	s.serveShared(w, r, href)
}

// relToBase strips the base path, which every request has to start with.
func (s *Server) relToBase(href string) (string, bool) {
	base := s.tree.Base()
	if base == "" {
		return href, true
	}
	rest, ok := strings.CutPrefix(href, base)
	if !ok || !strings.HasPrefix(rest, "/") {
		return "", false
	}
	return rest, true
}

func (s *Server) publicHref() string { return s.tree.Base() + PublicHref }

func (s *Server) thumbsHref() string { return s.tree.Base() + ThumbsHref }

// isReservedName reports whether the path (below the base) starts with the
// reserved segment, however it is escaped.
func isReservedName(rel string) bool {
	seg, _, _ := strings.Cut(strings.TrimPrefix(rel, "/"), "/")
	name, err := url.PathUnescape(seg)
	return err == nil && name == tree.ReservedName
}

// serveBuiltin serves the pages below the reserved name: the admin page
// and the health endpoints.
func (s *Server) serveBuiltin(w http.ResponseWriter, r *http.Request, rel string) {
	name, err := unescape(rel)
	if err != nil {
		s.notFound(w)
		return
	}
	switch name {
	case AdminPath, AdminPath + "/":
		s.renderPage(w, r, "info", "")
	case HealthyPath:
		s.probe(w, r, http.StatusOK, "vitrine is healthy.\n")
	case ReadyPath:
		if s.isReady() {
			s.probe(w, r, http.StatusOK, "vitrine is ready.\n")
		} else {
			s.probe(w, r, http.StatusServiceUnavailable, "vitrine is not ready.\n")
		}
	default:
		s.notFound(w)
	}
}

// serveReserved serves the frontend and thumbnails.
func (s *Server) serveReserved(w http.ResponseWriter, r *http.Request, rest string) {
	name, err := unescape(rest)
	if err != nil {
		s.notFound(w)
		return
	}
	switch {
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
		if s.tree.IsManagedPath(p) {
			// the path below the root, so links keep their hrefs
			s.renderPage(w, r, "index", p)
			return
		}
		if f, ok := s.tree.OpenUnmanagedIndex(p); ok {
			s.serveFile(w, r, f)
			return
		}
		s.notFound(w)
		return
	}
	if f, ok := s.tree.OpenManagedFile(p); ok {
		s.serveFile(w, r, f)
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

var checkedCommands = []string{"du", "ffmpeg", "magick"}

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
		"PUBLIC_HREF": s.publicHref(),
		"ROOT_HREF":   s.tree.Base() + "/",
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
