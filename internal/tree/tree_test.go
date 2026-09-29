package tree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/web"
)

// fixture builds:
//
//	outside/secret.txt, outside/dir/x.txt
//	root/a.txt, root/.secret, root/my file#1.txt
//	root/_vitrine.headers.md
//	root/sub/b.jpg, root/sub/_h5fs.footer.html, root/sub/.hidden/c.txt
//	root/site/index.html
//	root/cache/t.jpg                  (excluded)
//	root/link-out -> ../outside/dir   (folder outside the root)
//	root/file-out -> ../outside/secret.txt
//	root/link-hidden -> .secret       (visible name, hidden target)
//	root/link-in -> sub               (folder inside the root)
//	root/.hl -> sub                   (hidden name)
//	root/link-cache -> cache          (excluded target)
func fixture(t *testing.T) (*Tree, string) {
	t.Helper()
	base := t.TempDir()
	base, _ = filepath.EvalSymlinks(base)
	root := filepath.Join(base, "root")
	files := map[string]string{
		"outside/secret.txt":         "secret",
		"outside/dir/x.txt":          "x",
		"outside/dir/.dot":           "d",
		"outside/dir/sub2/y.txt":     "y",
		"root/a.txt":                 "aaa",
		"root/.secret":               "s",
		"root/my file#1.txt":         "1",
		"root/_vitrine.headers.md":   "# header",
		"root/sub/b.jpg":             "b",
		"root/sub/_h5fs.footer.html": "<p>footer</p>",
		"root/sub/.hidden/c.txt":     "c",
		"root/site/index.html":       "<html>",
		"root/cache/t.jpg":           "t",
	}
	for name, content := range files {
		p := filepath.Join(base, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{
		"root/link-out":    "../outside/dir",
		"root/file-out":    "../outside/secret.txt",
		"root/link-hidden": ".secret",
		"root/link-in":     "sub",
		"root/.hl":         "sub",   // hidden name, visible target
		"root/link-cache":  "cache", // excluded target
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(base, name)); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load("", web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	tr, err := New(root, cfg, filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	return tr, root
}

func TestToPathRejectsTraversal(t *testing.T) {
	tr, root := fixture(t)
	for _, href := range []string{
		"/../outside/secret.txt", "/%2e%2e/outside", "/sub/./b.jpg", "/a%2Fb",
		"/a%5Cb", "relative", "/a%00", "/%zz",
	} {
		if p, err := tr.ToPath(href); err == nil {
			t.Errorf("ToPath(%q) = %q, want error", href, p)
		}
	}
	if p, err := tr.ToPath("/my%20file%231.txt"); err != nil || p != filepath.Join(root, "my file#1.txt") {
		t.Errorf("ToPath = %q, %v", p, err)
	}
	if p, _ := tr.ToPath("//sub//"); p != filepath.Join(root, "sub") {
		t.Errorf("ToPath(//sub//) = %q", p)
	}
}

func TestToHref(t *testing.T) {
	tr, root := fixture(t)
	tests := map[string]string{
		root:                                  "/",
		filepath.Join(root, "my file#1.txt"):  "/my%20file%231.txt",
		filepath.Join(root, "sub", "ä+(x)!~"): "/sub/%C3%A4%2B%28x%29%21~",
	}
	for p, want := range tests {
		if got, ok := tr.ToHref(p, false); !ok || got != want {
			t.Errorf("ToHref(%q) = %q, want %q", p, got, want)
		}
	}
	if got, _ := tr.ToHref(filepath.Join(root, "sub"), true); got != "/sub/" {
		t.Errorf("folder href = %q", got)
	}
	if _, ok := tr.ToHref("/etc", false); ok {
		t.Error("paths outside the root have no href")
	}
}

func TestManagedFolders(t *testing.T) {
	tr, _ := fixture(t)
	tests := map[string]bool{
		"/":             true,
		"/sub/":         true,
		"/link-in/":     true,  // link to a folder inside the root
		"/sub/.hidden/": false, // hidden
		"/site/":        false, // contains index.html
		"/link-out/":    false, // resolves outside the root
		"/cache/":       false, // excluded
		"/missing/":     false,
		"/a.txt":        false, // no folder
	}
	for href, want := range tests {
		if got := tr.IsManagedHref(href); got != want {
			t.Errorf("IsManagedHref(%q) = %v, want %v", href, got, want)
		}
	}
}

func TestResolveManagedFile(t *testing.T) {
	tr, root := fixture(t)
	tests := map[string]bool{
		"a.txt":             true,
		"sub/b.jpg":         true,
		"link-in/b.jpg":     true,
		".secret":           false, // hidden
		"sub/.hidden/c.txt": false, // hidden ancestor
		"file-out":          false, // outside the root
		"link-out/x.txt":    false, // outside the root
		"link-hidden":       false, // target is hidden
		"cache/t.jpg":       false, // excluded
		"site/index.html":   false, // unmanaged folder
		"sub":               false, // no file
	}
	for rel, want := range tests {
		_, got := tr.ResolveManagedFile(filepath.Join(root, rel))
		if got != want {
			t.Errorf("ResolveManagedFile(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestUnmanagedIndex(t *testing.T) {
	tr, root := fixture(t)
	if p, ok := tr.ResolveUnmanagedIndex(filepath.Join(root, "site")); !ok || filepath.Base(p) != "index.html" {
		t.Errorf("ResolveUnmanagedIndex(site) = %q, %v", p, ok)
	}
	if _, ok := tr.ResolveUnmanagedIndex(filepath.Join(root, "sub")); ok {
		t.Error("sub has no index")
	}
}

func TestReadDir(t *testing.T) {
	tr, root := fixture(t)
	got := strings.Join(tr.ReadDir(root), ",")
	// links leaving the root (file-out, link-out) are not listed
	want := "a.txt,cache,link-hidden,link-in,my file#1.txt,site,sub"
	if got != want {
		t.Errorf("ReadDir = %s\nwant      %s", got, want)
	}
}

func TestItems(t *testing.T) {
	tr, _ := fixture(t)
	items := tr.Items("/sub/", 1)
	byHref := map[string]*Item{}
	for _, it := range items {
		byHref[it.Href] = it
	}
	for _, href := range []string{"/", "/sub/", "/sub/b.jpg", "/a.txt", "/site/"} {
		if byHref[href] == nil {
			t.Errorf("missing item %s", href)
		}
	}
	if byHref["/sub/.hidden/"] != nil {
		t.Error("hidden folder listed")
	}
	if !byHref["/"].Fetched || !byHref["/sub/"].Fetched || byHref["/site/"].Fetched {
		t.Error("fetched flags wrong")
	}
	if byHref["/site/"].Managed || !byHref["/sub/"].Managed {
		t.Error("managed flags wrong")
	}
	if !items[0].IsFolder {
		t.Error("folders must come first")
	}

	b, _ := json.Marshal(byHref["/a.txt"])
	var file map[string]any
	json.Unmarshal(b, &file)
	if file["size"] != float64(3) || file["time"].(float64) <= 0 || file["managed"] != nil {
		t.Errorf("file json = %s", b)
	}
	b, _ = json.Marshal(byHref["/site/"])
	if !strings.Contains(string(b), `"managed":false`) || !strings.Contains(string(b), `"fetched":false`) {
		t.Errorf("folder json = %s", b)
	}

	if items := tr.Items("/sub/.hidden/", 1); len(items) != 0 {
		t.Error("items of a hidden folder must be empty")
	}
	deep := tr.Items("/", 2)
	found := false
	for _, it := range deep {
		found = found || it.Href == "/sub/b.jpg"
	}
	if !found {
		t.Error("what=2 must include the content of subfolders")
	}
}

func TestFolderSize(t *testing.T) {
	tr, _ := fixture(t)
	for _, it := range tr.Items("/", 1) {
		if it.Href == "/sub/" {
			// b.jpg (1) + _h5fs.footer.html (13) + .hidden/c.txt (1)
			if it.Size == nil || *it.Size != 15 {
				t.Errorf("sub size = %v", it.Size)
			}
			return
		}
	}
	t.Error("sub not listed")
}

func TestSearch(t *testing.T) {
	tr, _ := fixture(t)
	hrefs := func(items []*Item) string {
		var s []string
		for _, it := range items {
			s = append(s, it.Href)
		}
		return strings.Join(s, ",")
	}
	if got := hrefs(tr.Search("/", `\.TXT$`, true)); got != "/a.txt,/my%20file%231.txt" {
		t.Errorf("search txt = %s", got)
	}
	if got := hrefs(tr.Search("/", `b\.jpg`, false)); got != "/sub/b.jpg" { // link-in resolves to sub, walked once
		t.Errorf("search jpg = %s", got)
	}
	if got := tr.Search("/", `(a)\1`, false); len(got) != 0 {
		t.Error("unsupported pattern must return nothing")
	}
	if got := tr.Search("/sub/.hidden/", `c`, false); len(got) != 0 {
		t.Error("search in hidden folder")
	}
}

func TestCustom(t *testing.T) {
	tr, _ := fixture(t)
	c := tr.Custom("/sub/")
	if c.Header.Content == nil || *c.Header.Content != "# header" || *c.Header.Type != "md" {
		t.Errorf("header = %+v", c.Header)
	}
	if c.Footer.Content == nil || *c.Footer.Type != "html" {
		t.Errorf("footer = %+v", c.Footer)
	}
	if c := tr.Custom("/"); c.Footer.Content != nil {
		t.Error("root has no footer")
	}
}

func TestNormalizeBase(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "/": "", "files": "/files", "/files/": "/files", "//a//b/": "/a/b", "/my files": "/my%20files",
	} {
		if got, err := NormalizeBase(in); err != nil || got != want {
			t.Errorf("NormalizeBase(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"/..", "/a/./b", "/a?b", "/a#b", "/a\\b"} {
		if _, err := NormalizeBase(in); err == nil {
			t.Errorf("NormalizeBase(%q) should fail", in)
		}
	}
}

func TestBaseHrefs(t *testing.T) {
	tr, root := fixture(t)
	tr.SetBase("/files")
	if h, _ := tr.ToHref(filepath.Join(root, "sub"), true); h != "/files/sub/" {
		t.Errorf("ToHref = %s", h)
	}
	if h, _ := tr.ToHref(root, true); h != "/files/" {
		t.Errorf("root href = %s", h)
	}
	if p, err := tr.ToPath("/files/sub/b.jpg"); err != nil || p != filepath.Join(root, "sub", "b.jpg") {
		t.Errorf("ToPath = %s %v", p, err)
	}
	if p, err := tr.ToPath("/files"); err != nil || p != root {
		t.Errorf("ToPath(base) = %s %v", p, err)
	}
	for _, href := range []string{"/sub/b.jpg", "/filesX/sub", "/"} {
		if _, err := tr.ToPath(href); err == nil {
			t.Errorf("ToPath(%s) outside the base must fail", href)
		}
	}
	if !tr.IsManagedHref("/files/sub/") || tr.IsManagedHref("/sub/") {
		t.Error("managed hrefs must include the base")
	}
}

func TestHiddenLinkName(t *testing.T) {
	tr, root := fixture(t)
	if tr.IsManagedHref("/.hl/") {
		t.Error("a link with a hidden name must not be served")
	}
	if _, ok := tr.ResolveManagedFile(filepath.Join(root, ".hl", "b.jpg")); ok {
		t.Error("files below a hidden link must not be served")
	}
}

func TestFollowSymlinks(t *testing.T) {
	tr, root := fixture(t)
	tr.SetFollowSymlinks(true)

	for href, want := range map[string]bool{
		"/link-out/":      true,
		"/link-out/sub2/": true,
		"/.hl/":           false, // hidden name
		"/link-cache/":    false, // excluded target
	} {
		if got := tr.IsManagedHref(href); got != want {
			t.Errorf("IsManagedHref(%s) = %v, want %v", href, got, want)
		}
	}
	for rel, want := range map[string]bool{
		"file-out":         true,
		"link-out/x.txt":   true,
		"link-out/.dot":    false, // hidden in the target
		"link-hidden":      false, // target inside the root is hidden
		"link-cache/t.jpg": false, // excluded
		".hl/b.jpg":        false,
	} {
		if _, got := tr.ResolveManagedFile(filepath.Join(root, rel)); got != want {
			t.Errorf("ResolveManagedFile(%s) = %v, want %v", rel, got, want)
		}
	}
	if got := strings.Join(tr.ReadDir(root), ","); got != "a.txt,cache,file-out,link-hidden,link-in,link-out,my file#1.txt,site,sub" {
		t.Errorf("ReadDir = %s", got)
	}
	if got := strings.Join(tr.ReadDir(filepath.Join(root, "link-out")), ","); got != "sub2,x.txt" {
		t.Errorf("ReadDir(link-out) = %s", got)
	}

	var hrefs []string
	for _, it := range tr.Items("/link-out/", 1) {
		hrefs = append(hrefs, it.Href)
	}
	if !strings.Contains(strings.Join(hrefs, ","), "/link-out/x.txt") {
		t.Errorf("items = %v", hrefs)
	}
	var found []string
	for _, it := range tr.Search("/", `^[xy]\.txt$`, false) {
		found = append(found, it.Href)
	}
	if strings.Join(found, ",") != "/link-out/sub2/y.txt,/link-out/x.txt" {
		t.Errorf("search = %v", found)
	}
}

func TestLinkedFolderSize(t *testing.T) {
	tr, _ := fixture(t)
	for _, it := range tr.Items("/", 1) {
		if it.Href == "/link-in/" && (it.Size == nil || *it.Size != 15) {
			t.Errorf("size of a linked folder = %v, want 15", it.Size)
		}
	}
}
