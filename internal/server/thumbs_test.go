package server

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThumbsAPI(t *testing.T) {
	s, root := newTestServer(t, fixtureOpts{})
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 48)))
	os.WriteFile(filepath.Join(root, "pic.png"), buf.Bytes(), 0o644)

	res := post(t, s, `{"action":"get","thumbs":[
		{"type":"img","href":"/pic.png","width":240,"height":240},
		{"type":"img","href":"/pic.png","width":"320","height":"240"},
		{"type":"img","href":"/.secret","width":240,"height":240},
		{"type":"img","href":"/pic.png","width":1,"height":1},
		"garbage"]}`)
	thumbs := res["thumbs"].([]any)
	if len(thumbs) != 5 || thumbs[0] == nil || thumbs[1] == nil || thumbs[2] != nil || thumbs[3] != nil || thumbs[4] != nil {
		t.Fatalf("thumbs = %v", thumbs)
	}
	href := thumbs[0].(string)
	if !strings.HasPrefix(href, ThumbsHref+"thumb-") {
		t.Fatalf("href = %s", href)
	}
	rec := do(s, "GET", href, "", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("GET thumb = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := do(s, "GET", ThumbsHref+"..%2F..%2Fetc%2Fpasswd", "", nil); rec.Code != 404 {
		t.Errorf("bad thumb name = %d", rec.Code)
	}

	many := `{"action":"get","thumbs":[` + strings.TrimSuffix(strings.Repeat(`{"type":"img","href":"/pic.png","width":240,"height":240},`, 41), ",") + `]}`
	if got := post(t, s, many)["thumbs"].([]any); len(got) != 0 {
		t.Errorf("more than 40 requests must return [], got %d", len(got))
	}

	off, _ := newTestServer(t, fixtureOpts{options: `{"thumbnails": {"enabled": false}}`})
	if res := post(t, off, `{"action":"get","thumbs":[]}`); res["err"] != nil {
		t.Errorf("empty thumbs must be ignored: %v", res)
	}
	if res := post(t, off, `{"action":"get","thumbs":[{"type":"img"}]}`); res["err"] != errDisabled {
		t.Errorf("disabled thumbs = %v", res)
	}
}
