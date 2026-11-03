package tree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/web"
)

// "du -sb" with symbolic links followed would add the size of what they
// point to, also outside of the root, to the size everybody sees.
func TestShellDuDoesNotFollowLinks(t *testing.T) {
	if out, err := exec.Command("du", "-sb", t.TempDir()).CombinedOutput(); err != nil {
		t.Skipf("du has no -b here: %s", out)
	}
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "root")
	os.MkdirAll(filepath.Join(root, "dir"), 0o755)
	os.MkdirAll(filepath.Join(base, "outside"), 0o755)
	os.WriteFile(filepath.Join(root, "dir", "a.txt"), []byte("small"), 0o644)
	os.WriteFile(filepath.Join(base, "outside", "huge.bin"), []byte(strings.Repeat("x", 3<<20)), 0o644)
	os.Symlink("../../outside", filepath.Join(root, "dir", "link-out")) // a folder outside of the root
	os.Symlink("../../outside/huge.bin", filepath.Join(root, "dir", "file-out"))
	// the folder itself is a link to a folder inside of the root
	os.Symlink("dir", filepath.Join(root, "alias"))

	confDir := filepath.Join(base, "conf")
	os.MkdirAll(confDir, 0o755)
	os.WriteFile(filepath.Join(confDir, "options.json"), []byte(`{"foldersize": {"enabled": true, "type": "shell-du"}}`), 0o644)
	cfg, err := config.Load(confDir, web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	tr, err := New(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, href := range []string{"/", "/dir/", "/alias/"} {
		size := tr.Items(href, 1)[0].Size
		if size == nil {
			t.Errorf("%s: no size", href)
			continue
		}
		if *size >= 1<<20 {
			t.Errorf("%s: size %d includes what the links point to (3 MiB outside of the root)", href, *size)
		}
	}
	if size := tr.Items("/alias/", 1)[0].Size; size == nil || *size < 5 {
		t.Errorf("/alias/: the size of a folder that is a link must count its files, got %v", size)
	}
}
