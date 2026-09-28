package auth

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

func init() { bcryptCost = bcrypt.MinCost }

func TestLoginEnabled(t *testing.T) {
	for hash, want := range map[string]bool{
		"":                                  false,
		emptySHA512:                         false,
		strings.ToUpper(emptySHA512):        false,
		"plain":                             false,
		"$2y$10$abc":                        false, // malformed bcrypt
		"$2y$10$" + strings.Repeat("a", 53): true,
		strings.Repeat("ab", 64):            true,
		"$argon2id$v=19$m=65536,t=4,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo": true,
	} {
		if got := LoginEnabled(hash); got != want {
			t.Errorf("LoginEnabled(%q) = %v, want %v", hash, got, want)
		}
	}
}

func TestVerifyBcrypt(t *testing.T) {
	hash, err := Hash("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	// PHP's password_hash writes the "$2y$" variant of the same algorithm
	for _, h := range []string{hash, "$2y$" + hash[4:]} {
		if !Verify(h, "s3cret") || Verify(h, "wrong") {
			t.Errorf("bcrypt verify failed for %s", h[:4])
		}
	}
	long := strings.Repeat("x", 80)
	h, _ := Hash(long[:72])
	if !Verify(h, long) {
		t.Error("bcrypt must only use the first 72 bytes, like PHP")
	}
	if Verify(h, strings.Repeat("x", MaxPasswordLength+1)) {
		t.Error("overlong passwords must be rejected")
	}
}

func TestVerifyArgon2(t *testing.T) {
	salt := []byte("0123456789abcdef")
	enc := base64.RawStdEncoding.EncodeToString
	id := argon2.IDKey([]byte("pw"), salt, 2, 1024, 1, 32)
	i := argon2.Key([]byte("pw"), salt, 2, 1024, 1, 32)
	for _, h := range []string{
		fmt.Sprintf("$argon2id$v=19$m=1024,t=2,p=1$%s$%s", enc(salt), enc(id)),
		fmt.Sprintf("$argon2i$v=19$m=1024,t=2,p=1$%s$%s", enc(salt), enc(i)),
	} {
		if !Verify(h, "pw") || Verify(h, "px") {
			t.Errorf("argon2 verify failed for %s", h[:10])
		}
	}
}

func TestVerifySHA512(t *testing.T) {
	sum := sha512.Sum512([]byte("legacy"))
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	if !Verify(h, "legacy") || Verify(h, "other") {
		t.Error("sha512 verify failed")
	}
	if Verify(emptySHA512, "") {
		t.Error("the former preset must not log in")
	}
}

func TestThrottle(t *testing.T) {
	now := time.Unix(1000, 0)
	th := NewThrottle()
	th.now = func() time.Time { return now }

	for i := 0; i < 4; i++ {
		th.Failure("a")
	}
	if th.RetryAfter("a") != 0 {
		t.Fatal("locked too early")
	}
	th.Failure("a")
	if th.RetryAfter("a") != 15*time.Minute || th.RetryAfter("b") != 0 {
		t.Fatal("5th failure must lock only that client")
	}
	now = now.Add(15*time.Minute + time.Second)
	if th.RetryAfter("a") != 0 {
		t.Fatal("lock must expire")
	}
	// failures outside the window start a new count
	th.Failure("c")
	now = now.Add(16 * time.Minute)
	for i := 0; i < 4; i++ {
		th.Failure("c")
	}
	if th.RetryAfter("c") != 0 {
		t.Fatal("old failures must not count")
	}
	th.Reset("c")
	if _, ok := th.clients["c"]; ok {
		t.Fatal("reset must forget the client")
	}

	th.MaxEntries = 3
	for i := 0; i < 10; i++ {
		th.Failure(fmt.Sprint("x", i))
	}
	if len(th.clients) > 3 {
		t.Fatalf("entries not bounded: %d", len(th.clients))
	}
}

func TestSessions(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewSessions()
	s.now = func() time.Time { return now }

	rec := httptest.NewRecorder()
	if err := s.Login(rec, httptest.NewRequest("POST", "/", nil), true); err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || len(cookie.Value) < 40 {
		t.Fatalf("cookie = %+v", cookie)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	if !s.IsAdmin(req) {
		t.Fatal("session not recognized")
	}
	forged := httptest.NewRequest("GET", "/", nil)
	forged.AddCookie(&http.Cookie{Name: CookieName, Value: "guess"})
	if s.IsAdmin(forged) {
		t.Fatal("unknown session accepted")
	}

	now = now.Add(sessionIdle + time.Second)
	if s.IsAdmin(req) {
		t.Fatal("idle session must expire")
	}

	rec = httptest.NewRecorder()
	s.Login(rec, httptest.NewRequest("POST", "/", nil), false)
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	rec = httptest.NewRecorder()
	s.Logout(rec, req, false)
	if s.IsAdmin(req) || rec.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("logout must end the session and clear the cookie")
	}
}
