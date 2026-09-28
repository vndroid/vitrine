package server

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/vndroid/vitrine/internal/auth"
)

func (s *Server) isAdmin(r *http.Request) bool {
	return s.sessions.IsAdmin(r)
}

// requireJSON only accepts JSON requests for state changing actions:
// browsers can't send them cross-site without a CORS preflight, which
// protects against CSRF.
func requireJSON(r *http.Request) error {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/json" {
		return fail(errUnsupported, "JSON request required")
	}
	return nil
}

func (s *Server) onLogin(w http.ResponseWriter, r *http.Request, p params) {
	if err := requireJSON(r); err != nil {
		s.apiFail(w, err)
		return
	}
	pass, err := p.str("pass")
	if err != nil {
		s.apiFail(w, err)
		return
	}
	c := s.clientOf(r)
	// like h5fs, a login attempt first ends the current admin session
	s.sessions.Drop(r)

	hash := s.cfg.Passhash()
	if !auth.LoginEnabled(hash) || len(pass) > auth.MaxPasswordLength {
		writeJSON(w, http.StatusOK, map[string]any{"asAdmin": false})
		return
	}
	client := ClientID(c.addr)
	if wait := s.throttle.RetryAfter(client); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"asAdmin": false, "err": errLocked, "msg": "too many failed logins, try again later",
		})
		return
	}
	if !auth.Verify(hash, pass) {
		s.throttle.Failure(client)
		s.log.Warn("failed admin login", "client", client)
		writeJSON(w, http.StatusOK, map[string]any{"asAdmin": false})
		return
	}
	s.throttle.Reset(client)
	if err := s.sessions.Login(w, r, c.https); err != nil {
		s.apiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asAdmin": true})
}

func (s *Server) onLogout(w http.ResponseWriter, r *http.Request) {
	if err := requireJSON(r); err != nil {
		s.apiFail(w, err)
		return
	}
	s.sessions.Logout(w, r, s.clientOf(r).https)
	writeJSON(w, http.StatusOK, map[string]any{"asAdmin": false})
}
