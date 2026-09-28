package server

import "net/http"

// Placeholders for features implemented in follow-up changes.

func (s *Server) onDownload(w http.ResponseWriter, _ *http.Request, _ params) {
	s.apiFail(w, fail(errDisabled, "download disabled"))
}

func (s *Server) thumbs(reqs []any) []*string { return make([]*string, len(reqs)) }

func (s *Server) serveThumb(w http.ResponseWriter, _ *http.Request, _ string) { s.notFound(w) }
