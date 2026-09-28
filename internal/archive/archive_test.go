package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/tree"
	"github.com/vndroid/vitrine/web"
)

func fixture(t *testing.T) *tree.Tree {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "root")
	long := strings.Repeat("n", 120) + ".txt"
	files := map[string]string{
		"outside/secret.txt":   "secret",
		"root/a.txt":           "hello",
		"root/.env":            "hidden",
		"root/dir/b.txt":       strings.Repeat("b", 1000),
		"root/dir/sub/c.txt":   "c",
		"root/dir/.git/config": "hidden",
		"root/dir/" + long:     "long name",
		"root/site/index.html": "unmanaged",
	}
	for name, content := range files {
		p := filepath.Join(base, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	os.Symlink("../outside/secret.txt", filepath.Join(root, "dir", "out.txt"))
	os.Symlink("a.txt", filepath.Join(root, "link.txt"))
	cfg, _ := config.Load("", web.Conf())
	tr, err := tree.New(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func names(p *Plan) string {
	var n []string
	for _, e := range p.Entries {
		if e.IsDir {
			n = append(n, e.Name+"/")
		} else {
			n = append(n, e.Name)
		}
	}
	sort.Strings(n)
	return strings.Join(n, ",")
}

func TestCollectWholeFolder(t *testing.T) {
	tr := fixture(t)
	p, err := Collect(tr, "/dir/", nil, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := "b.txt," + strings.Repeat("n", 120) + ".txt,sub/,sub/c.txt"
	if got := names(p); got != want {
		t.Errorf("entries = %s\nwant      %s", got, want)
	}
	if p.Bytes != 1000+9+1 {
		t.Errorf("bytes = %d", p.Bytes)
	}
}

func TestCollectSelection(t *testing.T) {
	tr := fixture(t)
	p, err := Collect(tr, "/", []string{"/a.txt", "/dir/sub/", "/.env", "/link.txt", "/dir/out.txt", "/../outside/secret.txt", "/site/index.html", "", "/a.txt"}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(p); got != "a.txt,dir/sub/,dir/sub/c.txt" {
		t.Errorf("entries = %s", got)
	}
}

func TestCollectRejects(t *testing.T) {
	tr := fixture(t)
	for _, tt := range []struct {
		base  string
		hrefs []string
	}{
		{"/../", nil},
		{"/site/", nil},               // unmanaged
		{"/dir/.git/", nil},           // hidden
		{"/", []string{"/.env"}},      // nothing left
		{"/dir/", []string{"/a.txt"}}, // outside the base
	} {
		if _, err := Collect(tr, tt.base, tt.hrefs, DefaultLimits); !errors.Is(err, ErrRejected) {
			t.Errorf("Collect(%s, %v) = %v, want ErrRejected", tt.base, tt.hrefs, err)
		}
	}
	limits := DefaultLimits
	limits.MaxFiles = 2
	if _, err := Collect(tr, "/", nil, limits); !errors.Is(err, ErrRejected) {
		t.Error("file limit not enforced")
	}
	limits = DefaultLimits
	limits.MaxBytes = 100
	if _, err := Collect(tr, "/", nil, limits); !errors.Is(err, ErrRejected) {
		t.Error("byte limit not enforced")
	}
}

func TestTarRoundTrip(t *testing.T) {
	tr := fixture(t)
	p, err := Collect(tr, "/", nil, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := p.WriteTar(&buf); err != nil {
		t.Fatal(err)
	}
	size, err := p.TarSize()
	if err != nil || size != int64(buf.Len()) {
		t.Fatalf("TarSize = %d, written %d (%v)", size, buf.Len(), err)
	}
	got := map[string]string{}
	rd := tar.NewReader(&buf)
	for {
		h, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rd)
		got[h.Name] = string(b)
	}
	long := "dir/" + strings.Repeat("n", 120) + ".txt"
	if got["a.txt"] != "hello" || got[long] != "long name" || got["dir/sub/"] != "" {
		t.Errorf("tar content = %v", got)
	}
	if _, ok := got[".env"]; ok {
		t.Error("hidden file packaged")
	}
}

func TestZipRoundTrip(t *testing.T) {
	tr := fixture(t)
	p, _ := Collect(tr, "/dir/", nil, DefaultLimits)
	var buf bytes.Buffer
	if err := p.WriteZip(&buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "b.txt" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			if len(b) != 1000 {
				t.Errorf("b.txt = %d bytes", len(b))
			}
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "b.txt,"+strings.Repeat("n", 120)+".txt,sub/,sub/c.txt" {
		t.Errorf("zip entries = %v", names)
	}
}

func TestFileShrinkingAfterPlanFails(t *testing.T) {
	tr := fixture(t)
	p, _ := Collect(tr, "/dir/", []string{"/dir/b.txt"}, DefaultLimits)
	os.WriteFile(p.Entries[0].Real, []byte("short"), 0o644)
	if err := p.WriteTar(io.Discard); err == nil {
		t.Error("a shrunken file must fail the download")
	}
}

func TestSlots(t *testing.T) {
	s := NewSlots()
	r1, ok := s.Acquire("a", 2, 1)
	if !ok {
		t.Fatal("first slot")
	}
	if _, ok := s.Acquire("a", 2, 1); ok {
		t.Fatal("per client limit")
	}
	r2, ok := s.Acquire("b", 2, 1)
	if !ok {
		t.Fatal("second client")
	}
	if _, ok := s.Acquire("c", 2, 1); ok {
		t.Fatal("global limit")
	}
	r1()
	r1() // idempotent
	if _, ok := s.Acquire("c", 2, 1); !ok {
		t.Fatal("slot not released")
	}
	r2()
}

func TestRateWriter(t *testing.T) {
	now := time.Unix(0, 0)
	w := &RateWriter{W: io.Discard, MaxDuration: time.Hour, MinRate: 1000, Grace: 10 * time.Second,
		now: func() time.Time { return now }}
	w.Write(make([]byte, 100))
	now = now.Add(5 * time.Second)
	if _, err := w.Write(make([]byte, 100)); err != nil {
		t.Fatal("within grace")
	}
	now = now.Add(10 * time.Second)
	if _, err := w.Write(make([]byte, 100)); !errors.Is(err, ErrTooSlow) {
		t.Fatal("too slow after grace")
	}

	now = time.Unix(0, 0)
	w = &RateWriter{W: io.Discard, MaxDuration: time.Minute, now: func() time.Time { return now }}
	w.Write([]byte("x"))
	now = now.Add(2 * time.Minute)
	if _, err := w.Write([]byte("x")); !errors.Is(err, ErrTooSlow) {
		t.Fatal("max duration")
	}
}
