package validate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vndroid/vitrine/web"
)

func check(t *testing.T, files map[string]string) []Issue {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	return Config(Options{Dir: dir, Defaults: web.Conf(), Themes: []string{"comity", "default"}})
}

// short renders issues without the folder, one per line.
func short(issues []Issue) string {
	var b strings.Builder
	for _, i := range issues {
		i.File = filepath.Base(i.File)
		b.WriteString(i.String() + "\n")
	}
	return b.String()
}

func TestDefaultsAreValid(t *testing.T) {
	files := map[string]string{}
	fs.WalkDir(web.Conf(), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(web.Conf(), path)
			files[path] = string(b)
		}
		return nil
	})
	if issues := check(t, files); len(issues) != 0 {
		t.Errorf("the built-in config has issues:\n%s", short(issues))
	}
}

func TestNoConfigFolder(t *testing.T) {
	if issues := Config(Options{Defaults: web.Conf()}); issues != nil {
		t.Errorf("issues = %v", issues)
	}
}

func TestSyntaxErrorPosition(t *testing.T) {
	got := short(check(t, map[string]string{"options.json": "{\n  /* ok */\n  \"a\": 1\n  \"b\": 2\n}"}))
	if !strings.HasPrefix(got, "options.json:4:3: error: invalid JSON") {
		t.Errorf("got %s", got)
	}
	got = short(check(t, map[string]string{"options.json": "{\"search\": "}))
	if !strings.Contains(got, "options.json:1:12: error: invalid JSON: unexpected end of file") {
		t.Errorf("got %s", got)
	}
	got = short(check(t, map[string]string{"options.json": "[]"}))
	if !strings.Contains(got, "must contain an object") {
		t.Errorf("got %s", got)
	}
}

func TestOptions(t *testing.T) {
	issues := check(t, map[string]string{
		"options.json": `{
  "passhash": "nope",
  "serach": {},
  "hasCustomPasshash": true,
  "view": {"hidden": ["(?=x)"], "modes": [], "theme": "x", "unmanaged": ["a/b"]},
  "download": {"enabled": 1, "type": "7z", "minRate": "32768"},
  "thumbnails": {"size": 0, "img": ["img-nope"], "doc": ["x-pdf", "img-jpg"]},
  "preview-img": {"size": 5000},
  "preview-txt": {"styles": {"txt": "1"}},
  "sort": {"folders": 3},
  "l10n": {"lang": "zz"},
  "tree": {"maxSubfolders": -1}
}`})
	got := short(issues)
	for _, want := range []string{
		`options.json:2:15: error: passhash: not a bcrypt, argon2 or SHA512 hash`,
		`options.json:3:3: warning: unknown option "serach" (did you mean "search"?), it is ignored`,
		`options.json:4:3: warning: hasCustomPasshash: set by vitrine`,
		`error: view.hidden[0]: pattern "(?=x)" is not supported`,
		`error: view.modes: must not be empty`,
		`error: view.theme: unknown theme "x", available: comity, default`,
		`warning: view.unmanaged[0]: "a/b" is no plain file name`,
		`warning: download.enabled: expected boolean, got number (treated as true)`,
		`error: download.type: "7z" is not one of tar, zip`,
		`warning: download.minRate: expected number, got string "32768"`,
		`error: thumbnails.size: 0 is less than 1`,
		`warning: thumbnails.img[0]: file type "img-nope" is not defined in types.json`,
		`warning: thumbnails.doc[1]: no thumbnails for file type "img-jpg"`,
		`error: preview-img.size: 5000 is more than 4096`,
		`error: preview-txt.styles.txt: expected a style number 0-3, got string`,
		`error: sort.folders: 3 is more than 2`,
		`error: l10n.lang: no translations for "zz"`,
		`error: tree.maxSubfolders: -1 is less than 0`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if Errors(issues) != 11 || len(issues) != 18 {
		t.Errorf("%d errors, %d issues:\n%s", Errors(issues), len(issues), got)
	}
}

func TestPasshashes(t *testing.T) {
	for hash, want := range map[string]string{
		"":                                  "",
		"$2a$12$" + strings.Repeat("a", 53): "",
		"cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e": "warning: passhash: hash of the empty password",
	} {
		got := short(check(t, map[string]string{"options.json": `{"passhash": "` + hash + `"}`}))
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("passhash %q: got %q, want %q", hash, got, want)
		}
	}
}

func TestReferencesUseTheConfigFolder(t *testing.T) {
	issues := check(t, map[string]string{
		"types.json":     `{"txt-new": ["*.new"], "empty": [], "bad": "x"}`,
		"l10n/xx.json":   `{"lang": "test"}`,
		"l10n/yy.json":   `{}`,
		"l10n/bad!.json": `{}`,
		"options.json":   `{"l10n": {"lang": "xx"}, "preview-txt": {"styles": {"txt-new": 1, "txt": 1}}}`,
	})
	got := short(issues)
	for _, want := range []string{
		`types.json:1:44: error: bad: expected an array of patterns`,
		`yy.json:1:1: warning: lang: missing the language name`,
		`bad!.json: warning: file name is no language code`,
		// "txt" is not in the folder's types.json, which replaces the default
		`warning: preview-txt.styles.txt: file type "txt" is not defined`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "txt-new") || strings.Contains(got, "l10n.lang") {
		t.Errorf("types and languages of the folder must be known:\n%s", got)
	}
}

func TestBrokenTypesFallBackToDefaults(t *testing.T) {
	got := short(check(t, map[string]string{
		"types.json":   `{"x": [`,
		"options.json": `{"thumbnails": {"img": ["img-jpg"]}}`,
	}))
	if !strings.Contains(got, "types.json") || strings.Contains(got, "img-jpg") {
		t.Errorf("got %s", got)
	}
}

func TestSuggest(t *testing.T) {
	keys := []string{"search", "sort", "select", "thumbnails"}
	for in, want := range map[string]string{"serach": "search", "thumbnail": "thumbnails", "Sort": "sort", "zzzzzz": ""} {
		if got := suggest(in, keys); got != want {
			t.Errorf("suggest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaxCacheTime(t *testing.T) {
	for options, want := range map[string]string{
		`{"thumbnails": {"maxCacheTime": 30}}`:  "",
		`{"thumbnails": {"maxCacheTime": 0}}`:   "",
		`{"thumbnails": {"maxCacheTime": -1}}`:  "error: thumbnails.maxCacheTime: -1 is less than 0",
		`{"thumbnails": {"maxCacheTime": 1.5}}`: "error: thumbnails.maxCacheTime: expected a whole number, got 1.5",
		`{"thumbnails": {"maxCacheTime": "7"}}`: "thumbnails.maxCacheTime",
	} {
		got := short(check(t, map[string]string{"options.json": options}))
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", options, got, want)
		}
	}
}
