package server

import (
	"archive/tar"
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBasePath(t *testing.T) {
	s, root := newTestServer(t, fixtureOpts{base: "/my files/"})
	const base = "/my%20files"

	for target, code := range map[string]int{
		base:                                    301,
		base + "/":                              200,
		base + "/a.txt":                         200,
		base + "/_vitrine/public/js/scripts.js": 200,
		base + "/-/admin":                       200,
		base + "/_vitrine/public/":              404,
		base + "/.secret":                       404,
		"/":                                     404,
		"/a.txt":                                404,
		"/_vitrine/public/js/scripts.js":        404,
		"/-/admin":                              404,
		"/my%20filesX/a.txt":                    404,
	} {
		if rec := do(s, "GET", target, "", nil); rec.Code != code {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, code)
		}
	}
	if loc := do(s, "GET", base, "", nil).Header().Get("Location"); loc != base+"/" {
		t.Errorf("redirect to %q", loc)
	}
	if loc := do(s, "GET", base+"/sub", "", nil).Header().Get("Location"); loc != base+"/sub/" {
		t.Errorf("folder redirect to %q", loc)
	}

	page := do(s, "GET", base+"/", "", nil).Body.String()
	for _, want := range []string{
		`<script src="` + base + `/_vitrine/public/js/scripts.js"`,
		`<a href="` + base + `/a.txt">a.txt</a>`,
		`src="` + base + `/_vitrine/public/images/fallback/`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page misses %s", want)
		}
	}

	rec := do(s, "POST", base+"/", `{"action":"get","setup":true,"items":{"href":"`+base+`/","what":1}}`, map[string]string{"Content-Type": "application/json"})
	body := rec.Body.String()
	for _, want := range []string{`"ROOT_HREF":"` + base + `/"`, `"PUBLIC_HREF":"` + base + `/_vitrine/public/"`, `"href":"` + base + `/a.txt"`} {
		if !strings.Contains(body, want) {
			t.Errorf("api response misses %s: %s", want, body)
		}
	}
	if rec := do(s, "POST", "/", `{"action":"get","setup":true}`, map[string]string{"Content-Type": "application/json"}); rec.Code != 404 {
		t.Errorf("POST outside the base = %d", rec.Code)
	}
	// hrefs without the base are outside the tree
	rec = do(s, "POST", base+"/", `{"action":"get","items":{"href":"/","what":1}}`, map[string]string{"Content-Type": "application/json"})
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("hrefs without the base must list nothing: %s", rec.Body.String())
	}

	rec = do(s, "POST", base+"/", "action=download&as=p&type=tar&baseHref="+base+"%2F&hrefs=&hrefs%5B0%5D="+base+"%2Fa.txt",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	h, err := tar.NewReader(bytes.NewReader(rec.Body.Bytes())).Next()
	if err != nil || h.Name != "a.txt" {
		t.Errorf("download below base: %v %v %s", h, err, rec.Body.String())
	}

	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 32, 32)))
	os.WriteFile(filepath.Join(root, "pic.png"), buf.Bytes(), 0o644)
	rec = do(s, "POST", base+"/", `{"action":"get","thumbs":[{"type":"img","href":"`+base+`/pic.png","width":240,"height":240}]}`, map[string]string{"Content-Type": "application/json"})
	if !strings.Contains(rec.Body.String(), `"`+base+`/_vitrine/thumbs/thumb-`) {
		t.Errorf("thumb href = %s", rec.Body.String())
	}
}
