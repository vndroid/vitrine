package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/tree"
	"github.com/vndroid/vitrine/web"
)

type fixtureOpts struct {
	options string // options.json override, empty for the defaults
	trusted string
	base    string // --base-path
}

func newTestServer(t *testing.T, o fixtureOpts) (*Server, string) {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "root")
	files := map[string]string{
		"outside/secret.txt":    "secret",
		"root/a.txt":            "hello world",
		"root/.secret":          "s",
		"root/sub/b.jpg":        "b",
		"root/site/index.html":  "<h1>site</h1>",
		"root/page.html":        "<script>alert(1)</script>",
		"root/code.php":         "<?php echo 1;",
		"root/my file#1.txt":    "1",
		"root/sub/.hidden/c.md": "c",
	}
	for name, content := range files {
		p := filepath.Join(base, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	os.Symlink("../outside/secret.txt", filepath.Join(root, "file-out"))

	confDir := ""
	if o.options != "" {
		confDir = filepath.Join(base, "conf")
		os.MkdirAll(confDir, 0o755)
		os.WriteFile(filepath.Join(confDir, "options.json"), []byte(o.options), 0o644)
	}
	cfg, err := config.Load(confDir, web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(base, "cache")
	os.MkdirAll(cache, 0o755)
	tr, err := tree.New(root, cfg, cache)
	if err != nil {
		t.Fatal(err)
	}
	if o.base != "" {
		base, err := tree.NormalizeBase(o.base)
		if err != nil {
			t.Fatal(err)
		}
		tr.SetBase(base)
	}
	trusted, err := ParseTrustedProxies(o.trusted)
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{
		Tree: tr, Config: cfg, Public: web.Public(), ConfigDir: confDir,
		CacheDir: cache, Version: "test", TrustedProxies: trusted,
	}), root
}

func do(s http.Handler, method, target string, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestIndexPage(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	rec := do(s, "GET", "/", "", nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		`<script src="/_vitrine/public/js/scripts.js" data-module="index">`,
		`<div id="fallback"><table>`,
		`<a href="/a.txt">a.txt</a>`,
		`<a href="/my%20file%231.txt">my file#1.txt</a>`,
		`family:"Ubuntu"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index page misses %s", want)
		}
	}
	if strings.Contains(body, ".secret") {
		t.Error("hidden file listed")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}

	// text browsers get the fallback without scripts
	rec = do(s, "GET", "/", "", map[string]string{"User-Agent": "curl/8.0"})
	if strings.Contains(rec.Body.String(), "scripts.js") || !strings.Contains(rec.Body.String(), "a.txt") {
		t.Error("curl should get the plain fallback listing")
	}
}

func TestSharedFiles(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	tests := []struct {
		target string
		code   int
	}{
		{"/a.txt", 200},
		{"/sub/b.jpg", 200},
		{"/my%20file%231.txt", 200},
		{"/.secret", 404},
		{"/sub/.hidden/c.md", 404},
		{"/sub/.hidden/", 404},
		{"/file-out", 404},
		{"/%2e%2e/outside/secret.txt", 404},
		{"/sub%2F..%2Fa.txt", 404},
		{"/missing", 404},
		{"/site/", 200},
	}
	for _, tt := range tests {
		if rec := do(s, "GET", tt.target, "", nil); rec.Code != tt.code {
			t.Errorf("GET %s = %d, want %d", tt.target, rec.Code, tt.code)
		}
	}
	if body := do(s, "GET", "/site/", "", nil).Body.String(); body != "<h1>site</h1>" {
		t.Errorf("unmanaged index = %q", body)
	}
}

func TestRangeAndConditional(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	rec := do(s, "GET", "/a.txt", "", map[string]string{"Range": "bytes=0-4"})
	if rec.Code != 206 || rec.Body.String() != "hello" || rec.Header().Get("Content-Range") != "bytes 0-4/11" {
		t.Errorf("range: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Range"))
	}
	lm := do(s, "GET", "/a.txt", "", nil).Header().Get("Last-Modified")
	if rec := do(s, "GET", "/a.txt", "", map[string]string{"If-Modified-Since": lm}); rec.Code != 304 {
		t.Errorf("If-Modified-Since = %d", rec.Code)
	}
	if rec := do(s, "HEAD", "/a.txt", "", nil); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD = %d, body %d", rec.Code, rec.Body.Len())
	}
}

func TestActiveContentIsSandboxed(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	if csp := do(s, "GET", "/page.html", "", nil).Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("html CSP = %q", csp)
	}
	if csp := do(s, "GET", "/a.txt", "", nil).Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("txt CSP = %q", csp)
	}
	if ct := do(s, "GET", "/code.php", "", nil).Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("php Content-Type = %q", ct)
	}
}

func TestRedirects(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	for target, want := range map[string]string{
		"/sub":             "/sub/",
		"/sub?x=1":         "/sub/?x=1",
		"/_vitrine/public": "/_vitrine/public/",
	} {
		rec := do(s, "GET", target, "", nil)
		if rec.Code != 301 || rec.Header().Get("Location") != want {
			t.Errorf("GET %s = %d %q, want 301 %q", target, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestReservedPaths(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	rec := do(s, "GET", "/_vitrine/public/js/scripts.js", "", nil)
	etag := rec.Header().Get("ETag")
	if rec.Code != 200 || etag == "" || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("asset = %d etag %q ct %q", rec.Code, etag, rec.Header().Get("Content-Type"))
	}
	if rec := do(s, "GET", "/_vitrine/public/js/scripts.js", "", map[string]string{"If-None-Match": etag}); rec.Code != 304 {
		t.Errorf("If-None-Match = %d", rec.Code)
	}
	if body := do(s, "GET", "/_vitrine/public/", "", nil).Body.String(); !strings.Contains(body, `data-module="info"`) {
		t.Error("info page expected")
	}
	for _, target := range []string{
		"/_vitrine/public/../../etc/passwd", "/_vitrine/public/%2e%2e/x", "/_vitrine/nope",
		"/_vitrine/public/ext/x.css", "/_vitrine/public/missing.js",
	} {
		if rec := do(s, "GET", target, "", nil); rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
	if rec := do(s, "PUT", "/a.txt", "", nil); rec.Code != 405 {
		t.Errorf("PUT = %d", rec.Code)
	}
}

func post(t *testing.T, s http.Handler, body string) map[string]any {
	t.Helper()
	rec := do(s, "POST", "/", body, map[string]string{"Content-Type": "application/json"})
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("POST %s: %v (%s)", body, err, rec.Body.String())
	}
	return res
}

func TestAPIGet(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	res := post(t, s, `{"action":"get","setup":true,"options":true,"types":true,"theme":true,"langs":true,"items":{"href":"/","what":1}}`)
	setup := res["setup"].(map[string]any)
	if setup["PUBLIC_HREF"] != PublicHref || setup["ROOT_HREF"] != "/" || setup["AS_ADMIN"] != false || setup["VERSION"] != nil {
		t.Errorf("setup = %v", setup)
	}
	opts := res["options"].(map[string]any)
	if _, ok := opts["passhash"]; ok {
		t.Error("passhash leaked")
	}
	if res["types"].(map[string]any)["img-jpg"] == nil {
		t.Error("types missing")
	}
	if res["theme"].(map[string]any)["txt-go"] != "comity/txt-go.svg" {
		t.Errorf("theme = %v", res["theme"])
	}
	if res["langs"].(map[string]any)["en"] != "english" {
		t.Error("langs missing")
	}
	var hrefs []string
	for _, it := range res["items"].([]any) {
		hrefs = append(hrefs, it.(map[string]any)["href"].(string))
	}
	if got := strings.Join(hrefs, ","); !strings.Contains(got, "/a.txt") || strings.Contains(got, "secret") {
		t.Errorf("items = %s", got)
	}
}

func TestAPIErrors(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	tests := map[string]string{
		`{"action":"get","items":{"href":"/"}}`:                errMissingParam,
		`{"action":"get","items":{"href":"/","what":"x"}}`:     errIllegalParam,
		`{"action":"get","items":{"href":[1],"what":1}}`:       errIllegalParam,
		`{"action":"get","search":{"href":"/","pattern":"a"}}`: errDisabled,
		`{"action":"nope"}`:                                    errUnsupported,
		`{}`:                                                   errMissingParam,
		`not json`:                                             errMissingParam,
	}
	for body, want := range tests {
		if got := post(t, s, body)["err"]; got != want {
			t.Errorf("POST %s: err = %v, want %s", body, got, want)
		}
	}
}

func TestAPIL10nAndCustom(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	res := post(t, s, `{"action":"get","l10n":["de","../../etc/passwd",""]}`)
	l10n := res["l10n"].(map[string]any)
	if len(l10n) != 1 || l10n["de"].(map[string]any)["isoCode"] != "de" {
		t.Errorf("l10n = %v", l10n)
	}
	res = post(t, s, `{"action":"get","custom":"/sub/"}`)
	if res["custom"].(map[string]any)["header"].(map[string]any)["content"] != nil {
		t.Error("no header expected")
	}
}

func TestAPISearchEnabled(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{options: `{"search": {"enabled": true}, "view": {"hidden": ["^\\."]}}`})
	res := post(t, s, `{"action":"get","search":{"href":"/","pattern":"b\\.jpg","ignorecase":true}}`)
	items := res["search"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["href"] != "/sub/b.jpg" {
		t.Errorf("search = %v", items)
	}
}

func TestFormParams(t *testing.T) {
	req := httptest.NewRequest("POST", "/?q=1", strings.NewReader("action=download&hrefs%5B1%5D=%2Fb&hrefs%5B0%5D=%2Fa&as=x.tar"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	p, err := parseParams(req)
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := p.str("action"); a != "download" {
		t.Errorf("action = %q", a)
	}
	hrefs, err := p.array("hrefs")
	if err != nil || len(hrefs) != 2 || hrefs[0] != "/a" || hrefs[1] != "/b" {
		t.Errorf("hrefs = %v, %v", hrefs, err)
	}
	if q, _ := p.str("q"); q != "1" {
		t.Errorf("query param = %q", q)
	}
}

func TestClientIP(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{trusted: "10.0.0.1, 172.16.0.0/12"})
	tests := []struct {
		remote string
		header map[string]string
		want   string
		https  bool
	}{
		{"203.0.113.9:1234", map[string]string{"X-Real-IP": "1.2.3.4", "X-Forwarded-Proto": "https"}, "203.0.113.9", false},
		{"10.0.0.1:1234", map[string]string{"X-Real-IP": "1.2.3.4", "X-Forwarded-Proto": "https"}, "1.2.3.4", true},
		{"172.18.0.1:1234", map[string]string{"X-Forwarded-For": "9.9.9.9, 5.6.7.8, 172.17.0.5"}, "5.6.7.8", false},
		{"172.18.0.1:1234", map[string]string{"X-Real-IP": "garbage"}, "172.18.0.1", false},
		{"[::ffff:10.0.0.1]:1234", map[string]string{"X-Real-IP": "2001:db8::1"}, "2001:db8::1", false},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tt.remote
		for k, v := range tt.header {
			req.Header.Set(k, v)
		}
		c := s.clientOf(req)
		if c.addr.String() != tt.want || c.https != tt.https {
			t.Errorf("clientOf(%s, %v) = %s https=%v, want %s https=%v", tt.remote, tt.header, c.addr, c.https, tt.want, tt.https)
		}
	}
	if id := ClientID(netip.MustParseAddr("2001:db8:1:2:3:4:5:6")); id != "2001:db8:1:2::/64" {
		t.Errorf("ClientID v6 = %s", id)
	}
}

func TestHeadTagsEscaping(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{options: `{"resources": {"styles": ["x.css\"><script>"], "scripts": ["https://cdn.example/a.js"]}, "view": {"fonts": ["A\"</style><script>"]}}`})
	body, _ := io.ReadAll(do(s, "GET", "/", "", nil).Body)
	if strings.Contains(string(body), `"><script>`) || strings.Contains(string(body), `</style><script>`) {
		t.Error("head tags not escaped")
	}
	if !strings.Contains(string(body), `href="/_vitrine/public/ext/x.css&#34;&gt;&lt;script&gt;"`) {
		t.Errorf("relative resource should point to ext")
	}
}

// A config directory with a partial options.json once dropped the default
// hidden patterns and served dotfiles (found on a real deployment).
func TestPartialOptionsKeepDotfilesPrivate(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{options: `{"search": {"enabled": true}}`})
	if rec := do(s, "GET", "/.secret", "", nil); rec.Code != 404 {
		t.Errorf("GET /.secret = %d", rec.Code)
	}
	if strings.Contains(do(s, "GET", "/", "", nil).Body.String(), ".secret") {
		t.Error(".secret listed")
	}
}

func TestFollowSymlinksServing(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	if rec := do(s, "GET", "/file-out", "", nil); rec.Code != 404 {
		t.Fatalf("default: GET /file-out = %d", rec.Code)
	}
	s.tree.SetFollowSymlinks(true)
	if rec := do(s, "GET", "/file-out", "", nil); rec.Code != 200 || rec.Body.String() != "secret" {
		t.Errorf("follow: GET /file-out = %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(do(s, "GET", "/", "", nil).Body.String(), `href="/file-out"`) {
		t.Error("followed link not listed")
	}
}
