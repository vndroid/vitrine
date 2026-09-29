package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	// maxThumbRequests per API request, like h5fs.
	maxThumbRequests = 40
	// thumbnail requests in progress, globally and per client (the client
	// sends them one after another)
	maxThumbCalls          = 16
	maxThumbCallsPerClient = 2
)

// thumbCallTimeout bounds one request: later thumbnails are answered with
// null (a variable for tests).
var thumbCallTimeout = 30 * time.Second

// thumbs answers the "thumbs" request of the client: one thumbnail href
// (or null) per request.
func (s *Server) thumbs(r *http.Request, reqs []any) ([]*string, error) {
	if len(reqs) > maxThumbRequests {
		return []*string{}, nil
	}
	out := make([]*string, len(reqs))
	if s.thumb == nil {
		return out, nil
	}
	release, ok := s.thumbCalls.Acquire(ClientID(s.clientOf(r).addr), maxThumbCalls, maxThumbCallsPerClient)
	if !ok {
		return nil, &apiError{errBusy, "too many thumbnail requests", http.StatusTooManyRequests}
	}
	defer release()
	deadline := time.Now().Add(thumbCallTimeout)
	for i, raw := range reqs {
		if time.Now().After(deadline) {
			break
		}
		req, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, ok1 := req["type"].(string)
		href, ok2 := req["href"].(string)
		width, ok3 := numeric(req["width"])
		height, ok4 := numeric(req["height"])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		if name, ok := s.thumb.Thumb(typ, href, width, height); ok {
			h := s.thumbsHref() + name
			out[i] = &h
		}
	}
	return out, nil
}

func numeric(v any) (int, bool) {
	var s string
	switch t := v.(type) {
	case json.Number:
		s = t.String()
	case string:
		s = t
	default:
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f != f || f > 1<<31 || f < -(1<<31) {
		return 0, false
	}
	return int(f), true
}

// serveThumb serves a cached thumbnail. Names are derived from the source
// path, so the content changes in place and is only cached briefly.
func (s *Server) serveThumb(w http.ResponseWriter, r *http.Request, name string) {
	if s.thumb == nil {
		s.notFound(w)
		return
	}
	p, ok := s.thumb.Path(name)
	if !ok {
		s.notFound(w)
		return
	}
	f, err := os.Open(p)
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
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=300")
	http.ServeContent(w, r, name, fi.ModTime(), f)
}
