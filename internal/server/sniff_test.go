package server

import (
	"mime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const htmlPayload = "<!DOCTYPE html><html><body><script>alert(document.domain)</script></body></html>"

// A shared file must never be shown as HTML because of its content: the
// type of a file without a known extension is not sniffed into text/html,
// which would run a script of the file on the origin of vitrine.
func TestSharedFilesAreNotSniffedAsHTML(t *testing.T) {
	s, root := newTestServer(t, fixtureOpts{})
	names := []string{"note.md", "page.markdown", "README", "data.zzz", "x.log", "NOTES.TXT2"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte(htmlPayload), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		rec := do(s, "GET", "/"+name, "", nil)
		ct := rec.Header().Get("Content-Type")
		t.Logf("%-14s mime.TypeByExtension=%q  served as %q", name, mime.TypeByExtension(filepath.Ext(name)), ct)
		for _, active := range []string{"text/html", "application/xhtml", "text/xml", "application/xml", "image/svg"} {
			if strings.HasPrefix(strings.ToLower(ct), active) {
				t.Errorf("%s is served as %q, its content runs as a page of the origin", name, ct)
			}
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: nosniff missing", name)
		}
		if rec.Body.String() != htmlPayload {
			t.Errorf("%s: the body was changed", name)
		}
	}
}

// Other content keeps the type it is sniffed to be; documents stay
// documents only where the extension says so, and then they are sandboxed.
func TestSniffedTypesOfUnknownExtensions(t *testing.T) {
	s, root := newTestServer(t, fixtureOpts{})
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00"
	files := map[string]string{
		"xml.zzz":    `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		"svgtag.zzz": `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`,
		"html.zzz":   "  \n\t<HTML><script>1</script>",
		"img.zzz":    png,
		"text.zzz":   "just some text\nand more\n",
		"bin.zzz":    "\x00\x01\x02\x03binary\xff\xfe",
		"empty.zzz":  "",
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(root, name), []byte(content), 0o644)
	}
	for name, want := range map[string]string{
		"xml.zzz": "text/plain", "svgtag.zzz": "text/plain", "html.zzz": "text/plain",
		"img.zzz": "image/png", "text.zzz": "text/plain", "bin.zzz": "application/octet-stream",
	} {
		ct := do(s, "GET", "/"+name, "", nil).Header().Get("Content-Type")
		if !strings.HasPrefix(ct, want) {
			t.Errorf("%s served as %q, want %s", name, ct, want)
		}
	}
	if rec := do(s, "GET", "/empty.zzz", "", nil); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("empty file: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	// a Range request still gets the right part of a file that was sniffed
	rec := do(s, "GET", "/text.zzz", "", map[string]string{"Range": "bytes=5-8"})
	if rec.Code != 206 || rec.Body.String() != "some" {
		t.Errorf("range: %d %q", rec.Code, rec.Body.String())
	}
	// documents by extension: as they are, but sandboxed
	for _, name := range []string{"a.html", "b.svg", "c.xml"} {
		os.WriteFile(filepath.Join(root, name), []byte(htmlPayload), 0o644)
		rec := do(s, "GET", "/"+name, "", nil)
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") {
			t.Errorf("%s: CSP = %q", name, csp)
		}
	}
	if ct := do(s, "GET", "/a.html", "", nil).Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("a.html served as %q", ct)
	}
}

func TestIsActiveType(t *testing.T) {
	for ct, want := range map[string]bool{
		"text/html; charset=utf-8": true, "text/xml": true, "application/xml": true,
		"image/svg+xml": true, "application/xhtml+xml": true, "application/rss+xml": true,
		"text/plain; charset=utf-8": false, "image/png": false, "application/pdf": false,
		"application/octet-stream": false, "text/markdown": false, "": false, "garbage;;": false,
	} {
		if got := isActiveType(ct); got != want {
			t.Errorf("isActiveType(%q) = %v, want %v", ct, got, want)
		}
	}
}
