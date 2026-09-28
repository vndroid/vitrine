package config

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/vndroid/vitrine/web"
)

func loadDefaults(t *testing.T) *Config {
	t.Helper()
	c, err := Load("", web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoadEmbeddedDefaults(t *testing.T) {
	c := loadDefaults(t)

	if c.Passhash() != "" {
		t.Errorf("default passhash = %q, want empty", c.Passhash())
	}
	if _, ok := c.Options()["passhash"]; ok {
		t.Error("passhash must not be sent to the client")
	}
	if !c.Bool("download.enabled", false) || c.Bool("search.enabled", true) {
		t.Error("unexpected download/search defaults")
	}
	if got := c.Int("download.minRate", 0); got != 32768 {
		t.Errorf("download.minRate = %d", got)
	}
	if got, ok := c.PositiveInt("thumbnails.size"); !ok || got != 240 {
		t.Errorf("thumbnails.size = %d, %v", got, ok)
	}
	if _, ok := c.PositiveInt("preview-img.size"); ok {
		t.Error("preview-img.size is false by default")
	}
	if got := c.String("view.theme", ""); got != "comity" {
		t.Errorf("view.theme = %q", got)
	}
	if len(c.Langs()) < 30 || c.Langs()["zh-cn"] == "" {
		t.Errorf("langs = %v", c.Langs())
	}
	if tr, ok := c.L10n("de"); !ok || tr["isoCode"] != "de" {
		t.Error("l10n de missing isoCode")
	}
}

func TestHidden(t *testing.T) {
	c := loadDefaults(t)
	for _, name := range []string{".", "..", ".git", "_vitrine.header.md", "_h5fs"} {
		if !c.IsHidden(name) {
			t.Errorf("%q should be hidden", name)
		}
	}
	for _, name := range []string{"a.txt", "x_vitrine", "movies"} {
		if c.IsHidden(name) {
			t.Errorf("%q should not be hidden", name)
		}
	}
}

func TestFileType(t *testing.T) {
	c := loadDefaults(t)
	tests := map[string]string{
		"a.jpg":      "img-jpg",
		"B.JPEG":     "img-jpg",
		"movie.mp4":  "vid-mp4",
		"doc.pdf":    "x-pdf",
		"Notes.MD":   "txt-md",
		"unknown.xy": "file",
	}
	for name, want := range tests {
		if got := c.FileType(name); got != want {
			t.Errorf("FileType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTypesKeepOrder(t *testing.T) {
	conf := fstest.MapFS{
		"options.json": {Data: []byte(`{}`)},
		"types.json":   {Data: []byte("{\n\"z\": [\"*.x\"], // first\n\"a\": [\"*.x\"]\n}")},
		"l10n/en.json": {Data: []byte(`{"lang": "english"}`)},
	}
	c, err := Load("", conf)
	if err != nil {
		t.Fatal(err)
	}
	// the last matching type wins, in file order (not alphabetical)
	if got := c.FileType("f.x"); got != "a" {
		t.Errorf("FileType = %q, want a", got)
	}
	var order []string
	dec := json.NewDecoder(jsonReader(c.Types()))
	dec.Token()
	for dec.More() {
		tok, _ := dec.Token()
		order = append(order, tok.(string))
		var skip any
		dec.Decode(&skip)
	}
	if len(order) != 2 || order[0] != "z" {
		t.Errorf("types order = %v", order)
	}
}

func TestOverrideDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "options.json"), []byte(`{
		// comment
		"passhash": " $2y$10$abc ",
		"view": {"hidden": ["^secret", "(a)\\1"]},
		"download": {"enabled": 1, "maxConcurrent": "8"}
	}`), 0o600)
	os.MkdirAll(filepath.Join(dir, "l10n"), 0o755)
	os.WriteFile(filepath.Join(dir, "l10n", "xx.json"), []byte(`{"lang": "test"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "l10n", "../evil.json"), []byte(`{}`), 0o600)

	c, err := Load(dir, web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	if c.Passhash() != "$2y$10$abc" {
		t.Errorf("passhash = %q", c.Passhash())
	}
	if !c.IsHidden("secret.txt") || c.IsHidden(".git") {
		t.Error("override options must replace the default hidden patterns")
	}
	if !c.Bool("download.enabled", false) || c.Int("download.maxConcurrent", 0) != 8 {
		t.Error("PHP-like truthiness / digit strings not handled")
	}
	if c.Langs()["xx"] != "test" || c.Langs()["de"] == "" {
		t.Error("l10n override must be added to the defaults")
	}
	// types.json not overridden: embedded default
	if c.FileType("a.png") != "img-png" {
		t.Error("types.json should fall back to the default")
	}
}

func jsonReader(b []byte) *bytesReader { return &bytesReader{b: b} }

type bytesReader struct{ b []byte }

func (r *bytesReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func TestPartialOverrideKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "options.json"), []byte(`{"passhash": "x", "search": {"enabled": true}}`), 0o600)
	c, err := Load(dir, web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsHidden(".env") || !c.IsHidden("_vitrine.header.md") {
		t.Error("a partial options.json must keep the default hidden patterns")
	}
	if !c.Bool("search.enabled", false) || c.Int("search.debounceTime", 0) != 300 {
		t.Error("objects must be merged key by key")
	}
	if c.String("view.theme", "") != "comity" {
		t.Error("untouched defaults must stay")
	}
}
