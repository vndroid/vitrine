package server

import (
	"io"
	"log/slog"
	"net/http"
	"time"
)

// AccessLog logs every request of next, except the health probes: the client (after trusted
// proxies), method, path, status, bytes sent, duration and user agent.
func (s *Server) AccessLog(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// health probes come every few seconds: not logged
		if s.isProbe(r) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		defer func() {
			// aborted downloads panic with http.ErrAbortHandler: log them too
			p := recover()
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			attrs := []any{
				"client", s.clientOf(r).addr.String(),
				"method", r.Method,
				"path", r.URL.EscapedPath(),
				"status", status,
				"bytes", rec.bytes,
				"ms", time.Since(start).Milliseconds(),
				"ua", r.UserAgent(),
			}
			if p != nil {
				attrs = append(attrs, "aborted", true)
			}
			log.Info("request", attrs...)
			if p != nil {
				panic(p)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

// recorder captures status and size. It keeps io.ReaderFrom (sendfile for
// file downloads) and unwraps for http.ResponseController (flushing).
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) ReadFrom(src io.Reader) (int64, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	var n int64
	var err error
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(src)
	} else {
		n, err = io.Copy(r.ResponseWriter, src)
	}
	r.bytes += n
	return n, err
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
