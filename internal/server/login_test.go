package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func loginServer(t *testing.T, trusted string) *Server {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	hash := string(h)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newTestServer(t, fixtureOpts{options: `{"passhash": "` + hash + `"}`, trusted: trusted})
	return s
}

func postAs(s http.Handler, body string, header map[string]string) (int, map[string]any, []*http.Cookie) {
	if header == nil {
		header = map[string]string{}
	}
	if _, ok := header["Content-Type"]; !ok {
		header["Content-Type"] = "application/json"
	}
	rec := do(s, "POST", "/", body, header)
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	return rec.Code, res, rec.Result().Cookies()
}

func TestLoginFlow(t *testing.T) {
	s := loginServer(t, "10.0.0.1")
	if s.cfg.Options()["hasCustomPasshash"] != true {
		t.Fatal("hasCustomPasshash must be true with a password")
	}

	_, res, _ := postAs(s, `{"action":"login","pass":"pw"}`, map[string]string{"Content-Type": "text/plain"})
	if res["err"] != errUnsupported {
		t.Errorf("non-JSON login = %v", res)
	}

	_, res, cookies := postAs(s, `{"action":"login","pass":"nope"}`, nil)
	if res["asAdmin"] != false || len(cookies) != 0 {
		t.Errorf("wrong password = %v %v", res, cookies)
	}

	hdr := map[string]string{"X-Forwarded-Proto": "https"}
	code, res, cookies := postAs(s, `{"action":"login","pass":"pw"}`, hdr)
	if code != 200 || res["asAdmin"] != true || len(cookies) != 1 {
		t.Fatalf("login = %d %v", code, res)
	}
	// httptest peers are 192.0.2.1, not a trusted proxy: no Secure flag
	if cookies[0].Secure {
		t.Error("Secure must follow the real scheme, not an untrusted header")
	}

	cookie := cookies[0].Name + "=" + cookies[0].Value
	_, res, _ = postAs(s, `{"action":"get","setup":true}`, map[string]string{"Cookie": cookie})
	setup := res["setup"].(map[string]any)
	if setup["AS_ADMIN"] != true || setup["VERSION"] != "test" || setup["HAS_CMD_DU"] == nil {
		t.Errorf("admin setup = %v", setup)
	}

	_, res, cookies = postAs(s, `{"action":"logout"}`, map[string]string{"Cookie": cookie})
	if res["asAdmin"] != false || cookies[0].MaxAge >= 0 {
		t.Errorf("logout = %v", res)
	}
	_, res, _ = postAs(s, `{"action":"get","setup":true}`, map[string]string{"Cookie": cookie})
	if res["setup"].(map[string]any)["AS_ADMIN"] != false {
		t.Error("session still valid after logout")
	}
}

func TestLoginSecureBehindProxy(t *testing.T) {
	s := loginServer(t, "192.0.2.0/24") // httptest's RemoteAddr
	_, _, cookies := postAs(s, `{"action":"login","pass":"pw"}`, map[string]string{"X-Forwarded-Proto": "https"})
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Errorf("cookie behind trusted TLS proxy must be Secure: %+v", cookies)
	}
}

func TestLoginLockout(t *testing.T) {
	s := loginServer(t, "192.0.2.0/24")
	from := func(ip string) map[string]string { return map[string]string{"X-Real-IP": ip} }
	for i := 0; i < 5; i++ {
		postAs(s, `{"action":"login","pass":"bad"}`, from("2001:db8::1"))
	}
	// same /64, correct password: still locked
	code, res, _ := postAs(s, `{"action":"login","pass":"pw"}`, from("2001:db8::ffff"))
	if code != http.StatusTooManyRequests || res["err"] != errLocked {
		t.Errorf("locked login = %d %v", code, res)
	}
	code, res, _ = postAs(s, `{"action":"login","pass":"pw"}`, from("203.0.113.5"))
	if code != 200 || res["asAdmin"] != true {
		t.Errorf("other client = %d %v", code, res)
	}
}

func TestLoginDisabledWithoutPasshash(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	if s.cfg.Options()["hasCustomPasshash"] != false {
		t.Error("hasCustomPasshash must be false")
	}
	_, res, cookies := postAs(s, `{"action":"login","pass":""}`, nil)
	if res["asAdmin"] != false || len(cookies) != 0 {
		t.Errorf("login without passhash = %v", res)
	}
	if strings.Contains(s.cfg.Passhash(), "$") {
		t.Error("unexpected passhash")
	}
}
