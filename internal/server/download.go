package server

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vndroid/vitrine/internal/archive"
	"github.com/vndroid/vitrine/internal/tree"
)

func (s *Server) onDownload(w http.ResponseWriter, r *http.Request, p params) {
	if !s.cfg.Bool("download.enabled", false) {
		s.apiFail(w, fail(errDisabled, "download disabled"))
		return
	}
	as, err := p.str("as")
	if err != nil {
		s.apiFail(w, err)
		return
	}
	typ, err := p.str("type")
	if err != nil {
		s.apiFail(w, err)
		return
	}
	baseHref, err := p.str("baseHref")
	if err != nil {
		s.apiFail(w, err)
		return
	}
	var hrefs []string
	if raw, ok := p.get("hrefs"); ok {
		switch v := raw.(type) {
		case string:
			hrefs = []string{v}
		default:
			arr, err := toArray(v, "hrefs")
			if err != nil {
				s.apiFail(w, err)
				return
			}
			for _, h := range arr {
				if hs, ok := h.(string); ok {
					hrefs = append(hrefs, hs)
				}
			}
		}
	}

	var ext string
	switch typ {
	case "tar", "php-tar", "shell-tar": // h5fs names still accepted
		ext = ".tar"
	case "zip", "shell-zip":
		ext = ".zip"
	default:
		s.apiFail(w, fail(errFailed, "packaging failed"))
		return
	}

	client := ClientID(s.clientOf(r).addr)
	release, ok := s.slots.Acquire(client,
		s.cfg.Int("download.maxConcurrent", 4),
		s.cfg.Int("download.maxConcurrentPerClient", 1))
	if !ok {
		s.apiFail(w, fail(errFailed, "packaging failed"))
		return
	}
	defer release()

	plan, err := archive.Collect(s.tree, baseHref, hrefs, archive.DefaultLimits)
	if err != nil {
		s.apiFail(w, fail(errFailed, "packaging failed"))
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", contentDisposition(packageName(as, ext)))
	h.Set("Cache-Control", "no-store")
	if ext == ".tar" {
		if size, err := plan.TarSize(); err == nil {
			h.Set("Content-Length", strconv.FormatInt(size, 10))
		}
	}
	rw := &archive.RateWriter{
		W:           w,
		MaxDuration: time.Duration(max(s.cfg.Int("download.maxDuration", 3600), 1)) * time.Second,
		MinRate:     int64(max(s.cfg.Int("download.minRate", 32768), 0)),
		Grace:       time.Duration(max(s.cfg.Int("download.minRateGrace", 30), 0)) * time.Second,
	}
	if ext == ".tar" {
		err = plan.WriteTar(rw)
	} else {
		err = plan.WriteZip(rw)
	}
	if err != nil {
		// Once data was sent an error can't be reported anymore: abort the
		// connection, so the client sees a failed instead of a short download.
		s.log.Info("download aborted", "client", client, "err", err)
		panic(http.ErrAbortHandler)
	}
}

// packageName cleans the requested name and makes sure it has the
// extension of the package type.
func packageName(as, ext string) string {
	name := sanitizeFilename(as, "package")
	if !strings.HasSuffix(strings.ToLower(name), ext) {
		name += ext
	}
	return name
}

// sanitizeFilename cleans an untrusted file name like h5fs: invalid
// UTF-8, control and format characters (e.g. RTL overrides) are removed,
// path separators replaced, dots and spaces trimmed, the length limited.
func sanitizeFilename(name, fallback string) string {
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\':
			return '_'
		case unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
			unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .\t")
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	if name == "" {
		return fallback
	}
	return name
}

var asciiUnsafe = regexp.MustCompile(`[^\x20-\x7E]|["\\%;]`)

// contentDisposition builds an attachment header with a plain ASCII
// fallback and the UTF-8 name (RFC 6266/5987).
func contentDisposition(name string) string {
	name = sanitizeFilename(name, "download")
	ascii := asciiUnsafe.ReplaceAllString(name, "_")
	return `attachment; filename="` + ascii + `"; filename*=UTF-8''` + tree.RawURLEncode(name)
}
