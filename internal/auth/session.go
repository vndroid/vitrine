package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

// CookieName is the session cookie.
const CookieName = "vitrine_session"

const (
	sessionIdle   = 2 * time.Hour
	sessionMax    = 24 * time.Hour
	maxSessions   = 10000
	sessionIDSize = 32
)

type session struct {
	created  time.Time
	lastSeen time.Time
}

// Sessions keeps the admin sessions in memory; a restart logs admins out.
// Only logged in admins have a session.
type Sessions struct {
	mu  sync.Mutex
	ids map[string]*session
	now func() time.Time
}

// NewSessions creates an empty session store.
func NewSessions() *Sessions {
	return &Sessions{ids: map[string]*session{}, now: time.Now}
}

// IsAdmin reports whether the request carries a valid admin session.
func (s *Sessions) IsAdmin(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.ids[c.Value]
	if !ok {
		return false
	}
	now := s.now()
	if now.Sub(sess.lastSeen) > sessionIdle || now.Sub(sess.created) > sessionMax {
		delete(s.ids, c.Value)
		return false
	}
	sess.lastSeen = now
	return true
}

// Login drops the current session and starts a new one (a new id on
// every privilege change prevents session fixation).
func (s *Sessions) Login(w http.ResponseWriter, r *http.Request, secure bool) error {
	s.Drop(r)
	b := make([]byte, sessionIDSize)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	now := s.now()

	s.mu.Lock()
	s.prune(now)
	s.ids[id] = &session{created: now, lastSeen: now}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: id, Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Logout ends the session and clears the cookie.
func (s *Sessions) Logout(w http.ResponseWriter, r *http.Request, secure bool) {
	s.Drop(r)
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// Drop ends the session of the request server side, without touching the
// cookie.
func (s *Sessions) Drop(r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		s.mu.Lock()
		delete(s.ids, c.Value)
		s.mu.Unlock()
	}
}

// prune removes expired sessions and, if still too many, the oldest.
func (s *Sessions) prune(now time.Time) {
	if len(s.ids) < maxSessions {
		return
	}
	var oldestID string
	var oldest time.Time
	for id, sess := range s.ids {
		if now.Sub(sess.lastSeen) > sessionIdle || now.Sub(sess.created) > sessionMax {
			delete(s.ids, id)
			continue
		}
		if oldestID == "" || sess.lastSeen.Before(oldest) {
			oldestID, oldest = id, sess.lastSeen
		}
	}
	if len(s.ids) >= maxSessions {
		delete(s.ids, oldestID)
	}
}
