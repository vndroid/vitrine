package server

import (
	"io"
	"net/http"
	"os"
	"time"
)

// readyTimeout bounds the check of the shared folder: a lost network mount
// may block instead of failing (a variable for tests).
var readyTimeout = 2 * time.Second

const (
	readyUnknown int32 = iota
	readyYes
	readyNo
)

// probe answers a health request: a fixed text without any details.
func (s *Server) probe(w http.ResponseWriter, r *http.Request, status int, text string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		io.WriteString(w, text)
	}
}

// checkRoot lists the first entry of the shared folder.
func (s *Server) checkRoot() error {
	if s.rootCheck != nil {
		return s.rootCheck()
	}
	f, err := os.Open(s.tree.Root())
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Readdirnames(1); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// isReady reports whether the shared folder is accessible. Only one check
// runs at a time, so a blocked mount can't pile up goroutines: while a
// check is still running the answer is "not ready". Changes of the state
// are logged, the probes themselves are not.
func (s *Server) isReady() bool {
	ok := false
	if s.readyBusy.CompareAndSwap(false, true) {
		done := make(chan error, 1)
		go func() {
			err := s.checkRoot()
			s.readyBusy.Store(false)
			done <- err
		}()
		select {
		case err := <-done:
			ok = err == nil
			if err != nil {
				s.noteReady(readyNo, err)
			}
		case <-time.After(readyTimeout):
			s.noteReady(readyNo, os.ErrDeadlineExceeded)
		}
	}
	if ok {
		s.noteReady(readyYes, nil)
	}
	return ok
}

func (s *Server) noteReady(state int32, err error) {
	prev := s.readyState.Swap(state)
	switch {
	case prev == state:
	case state == readyNo:
		s.log.Warn("shared folder not accessible, /-/ready answers 503", "err", err)
	case prev == readyNo:
		s.log.Info("shared folder accessible again, /-/ready answers 200")
	}
}

// isProbe reports whether the request is one of the health endpoints.
func (s *Server) isProbe(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	rel, ok := s.relToBase(r.URL.EscapedPath())
	return ok && (rel == HealthyPath || rel == ReadyPath)
}
