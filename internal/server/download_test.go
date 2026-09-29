package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func formDownload(s http.Handler, form string) *httptest.ResponseRecorder {
	return do(s, "POST", "/", form, map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

func TestDownloadTar(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	// the form the h5fs client posts
	rec := formDownload(s, "action=download&as=my%20files&type=tar&baseHref=%2F&hrefs=&hrefs%5B0%5D=%2Fa.txt&hrefs%5B1%5D=%2Fsub%2F")
	if rec.Code != 200 {
		t.Fatalf("download = %d %s", rec.Code, rec.Body.String())
	}
	cd := rec.Header().Get("Content-Disposition")
	if cd != `attachment; filename="my files.tar"; filename*=UTF-8''my%20files.tar` {
		t.Errorf("Content-Disposition = %s", cd)
	}
	if cl, _ := strconv.Atoi(rec.Header().Get("Content-Length")); cl != rec.Body.Len() {
		t.Errorf("Content-Length %d, body %d", cl, rec.Body.Len())
	}
	var names []string
	rd := tar.NewReader(bytes.NewReader(rec.Body.Bytes()))
	for {
		h, err := rd.Next()
		if err != nil {
			break
		}
		names = append(names, h.Name)
	}
	if strings.Join(names, ",") != "a.txt,sub/,sub/b.jpg" {
		t.Errorf("tar entries = %v", names)
	}
}

func TestDownloadZip(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	rec := formDownload(s, "action=download&as=pkg.ZIP&type=zip&baseHref=%2Fsub%2F&hrefs=")
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "b.jpg" {
		t.Errorf("zip entries = %v", zr.File)
	}
	rc, _ := zr.File[0].Open()
	if b, _ := io.ReadAll(rc); string(b) != "b" {
		t.Errorf("content = %q", b)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="pkg.ZIP"`) {
		t.Errorf("name = %s", rec.Header().Get("Content-Disposition"))
	}
}

func TestDownloadFailures(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	for form, want := range map[string]string{
		"action=download&as=x&type=evil&baseHref=%2F":                 errFailed,
		"action=download&as=x&type=tar&baseHref=%2F..%2F":             errFailed,
		"action=download&as=x&type=tar&baseHref=%2F&hrefs=%2F.secret": errFailed,
		"action=download&type=tar&baseHref=%2F":                       errMissingParam,
	} {
		if body := formDownload(s, form).Body.String(); !strings.Contains(body, want) {
			t.Errorf("%s => %s, want %s", form, body, want)
		}
	}
	off, _ := newTestServer(t, fixtureOpts{options: `{"download": {"enabled": false}}`})
	if body := formDownload(off, "action=download&as=x&type=tar&baseHref=%2F").Body.String(); !strings.Contains(body, errDisabled) {
		t.Errorf("disabled download = %s", body)
	}
}

func TestPackageName(t *testing.T) {
	for in, want := range map[string]string{
		"photos":                 "photos.tar",
		"a/b\\c":                 "a_b_c.tar",
		" .hidden. ":             "hidden.tar",
		"x‮gnp.exe":              "xgnp.exe.tar",
		"":                       "package.tar",
		"already.TAR":            "already.TAR",
		"bad\xff\x00name":        "badname.tar",
		strings.Repeat("é", 300): strings.Repeat("é", 200) + ".tar",
	} {
		if got := packageName(in, ".tar"); got != want {
			t.Errorf("packageName(%q) = %q, want %q", in, got, want)
		}
	}
	if cd := contentDisposition(`Ünïcode "q";%.tar`); cd != `attachment; filename="_n_code _q___.tar"; filename*=UTF-8''%C3%9Cn%C3%AFcode%20%22q%22%3B%25.tar` {
		t.Errorf("contentDisposition = %s", cd)
	}
}

func TestDownloadTypeNames(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	for typ, ext := range map[string]string{"tar": ".tar", "zip": ".zip"} {
		rec := formDownload(s, "action=download&as=p&type="+typ+"&baseHref=%2Fsub%2F&hrefs=")
		if !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="p`+ext+`"`) {
			t.Errorf("type %s: %s %s", typ, rec.Header().Get("Content-Disposition"), rec.Body.String())
		}
	}
}
