package thumb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/tree"
	"github.com/vndroid/vitrine/web"
)

// quadrants returns a w x h image: red top-left, green top-right, blue
// bottom-left, white bottom-right.
func quadrants(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 255, 255, 255}
			switch {
			case x < w/2 && y < h/2:
				c = color.RGBA{255, 0, 0, 255}
			case y < h/2:
				c = color.RGBA{0, 255, 0, 255}
			case x < w/2:
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// jpegWithOrientation encodes img as JPEG with an EXIF orientation tag.
func jpegWithOrientation(t *testing.T, img image.Image, o uint16, bo binary.ByteOrder) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	var tiff bytes.Buffer
	if bo == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}
	binary.Write(&tiff, bo, uint16(42))
	binary.Write(&tiff, bo, uint32(8))
	binary.Write(&tiff, bo, uint16(1))      // one entry
	binary.Write(&tiff, bo, uint16(0x0112)) // orientation
	binary.Write(&tiff, bo, uint16(3))      // SHORT
	binary.Write(&tiff, bo, uint32(1))
	binary.Write(&tiff, bo, o)
	binary.Write(&tiff, bo, uint16(0))
	binary.Write(&tiff, bo, uint32(0)) // no next IFD
	app1 := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(app1)+2))
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, app1...)
	return append(out, buf.Bytes()[2:]...)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pngBytes(t *testing.T, img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func near(c color.Color, r, g, b uint8) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(a uint32, b uint8) bool { return int(a>>8)-int(b) < 60 && int(b)-int(a>>8) < 60 }
	return d(cr, r) && d(cg, g) && d(cb, b)
}

func TestRenderSizes(t *testing.T) {
	dir := t.TempDir()
	land := filepath.Join(dir, "land.png")
	writeFile(t, land, pngBytes(t, quadrants(400, 200)))
	port := filepath.Join(dir, "port.png")
	writeFile(t, port, pngBytes(t, quadrants(200, 400)))
	small := filepath.Join(dir, "small.png")
	writeFile(t, small, pngBytes(t, quadrants(50, 20)))

	tests := []struct {
		path         string
		w, h         int
		wantW, wantH int
	}{
		{land, 240, 240, 240, 240},
		{land, 320, 240, 320, 240},
		{land, 100, 0, 100, 50},
		{port, 100, 0, 50, 100},
		{small, 100, 0, 50, 20},
	}
	for _, tt := range tests {
		img, err := render(tt.path, tt.w, tt.h, false)
		if err != nil {
			t.Fatalf("render %s: %v", tt.path, err)
		}
		if b := img.Bounds(); b.Dx() != tt.wantW || b.Dy() != tt.wantH {
			t.Errorf("render(%s, %d, %d) = %dx%d, want %dx%d", filepath.Base(tt.path), tt.w, tt.h, b.Dx(), b.Dy(), tt.wantW, tt.wantH)
		}
	}

	// square crop of a landscape image: centered, so the middle shows the
	// red/green border at the top
	img, _ := render(land, 100, 100, false)
	if !near(img.At(10, 10), 255, 0, 0) || !near(img.At(90, 10), 0, 255, 0) {
		t.Error("landscape crop not centered")
	}
	// portrait to landscape: top aligned like h5fs
	img, _ = render(port, 100, 50, false)
	if !near(img.At(10, 10), 255, 0, 0) || !near(img.At(10, 45), 255, 0, 0) {
		t.Error("portrait crop not top aligned")
	}
}

func TestRenderRejectsBadSources(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"empty.png": {},
		"text.jpg":  []byte("not an image"),
	} {
		p := filepath.Join(dir, name)
		writeFile(t, p, data)
		if _, err := render(p, 100, 100, false); err == nil {
			t.Errorf("render(%s) should fail", name)
		}
	}
	// huge dimensions are rejected from the header, before decoding
	huge := filepath.Join(dir, "huge.png")
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, 6000, 5000)))
	writeFile(t, huge, buf.Bytes())
	if _, err := render(huge, 100, 100, false); err == nil {
		t.Error("30 MP image should be rejected")
	}
}

func TestEXIFOrientation(t *testing.T) {
	dir := t.TempDir()
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		p := filepath.Join(dir, "rot.jpg")
		// stored landscape, displayed portrait (rotate 90 degrees clockwise)
		writeFile(t, p, jpegWithOrientation(t, quadrants(200, 100), 6, bo))
		f, _ := os.Open(p)
		if o := jpegExif(f).orientation; o != 6 {
			t.Fatalf("orientation = %d", o)
		}
		f.Close()

		img, err := render(p, 100, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 50 || b.Dy() != 100 {
			t.Fatalf("oriented sample = %dx%d, want 50x100", b.Dx(), b.Dy())
		}
		// rotating clockwise moves the stored top-left (red) to the top-right
		if !near(img.At(45, 5), 255, 0, 0) || !near(img.At(5, 5), 0, 0, 255) {
			t.Errorf("rotation wrong: top-right %v, top-left %v", img.At(45, 5), img.At(5, 5))
		}
	}
	// all orientations produce the displayed size
	for o := uint16(1); o <= 8; o++ {
		p := filepath.Join(dir, "o.jpg")
		writeFile(t, p, jpegWithOrientation(t, quadrants(200, 100), o, binary.BigEndian))
		img, err := render(p, 240, 240, false)
		if err != nil || img.Bounds().Dx() != 240 || img.Bounds().Dy() != 240 {
			t.Errorf("orientation %d: %v %v", o, img.Bounds(), err)
		}
	}
}

func service(t *testing.T, options string) (*Service, string) {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "root")
	writeFile(t, filepath.Join(root, "a.png"), pngBytes(t, quadrants(400, 300)))
	writeFile(t, filepath.Join(root, ".hidden.png"), pngBytes(t, quadrants(40, 30)))
	writeFile(t, filepath.Join(base, "outside.png"), pngBytes(t, quadrants(40, 30)))
	writeFile(t, filepath.Join(root, "fake.png"), []byte("x"))
	os.Symlink("../outside.png", filepath.Join(root, "out.png"))
	os.Symlink("a.png", filepath.Join(root, "renamed.jpg")) // type differs from target
	confDir := ""
	if options != "" {
		confDir = filepath.Join(base, "conf")
		writeFile(t, filepath.Join(confDir, "options.json"), []byte(options))
	}
	cfg, err := config.Load(confDir, web.Conf())
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := tree.New(root, cfg)
	s, err := New(tr, cfg, filepath.Join(base, "cache"), func(string) bool { _, err := exec.LookPath("ffmpeg"); return err == nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestServiceThumb(t *testing.T) {
	s, root := service(t, "")
	name, ok := s.Thumb("img", "/a.png", 240, 240)
	if !ok || !nameRe.MatchString(name) || !strings.HasSuffix(name, "-240x240.jpg") {
		t.Fatalf("Thumb = %q, %v", name, ok)
	}
	p, ok := s.Path(name)
	if !ok {
		t.Fatal("Path rejected a thumb name")
	}
	fi, _ := os.Stat(p)
	if _, ok := s.Thumb("img", "/a.png", 320, 240); !ok {
		t.Error("landscape size must be allowed")
	}

	// cached: not rewritten
	time.Sleep(10 * time.Millisecond)
	s.Thumb("img", "/a.png", 240, 240)
	if fi2, _ := os.Stat(p); !fi2.ModTime().Equal(fi.ModTime()) {
		t.Error("fresh thumbnail was regenerated")
	}
	// source changed: regenerated
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(root, "a.png"), future, future)
	s.Thumb("img", "/a.png", 240, 240)
	if fi2, _ := os.Stat(p); fi2.ModTime().Equal(fi.ModTime()) {
		t.Error("stale thumbnail not regenerated")
	}

	for _, tt := range []struct {
		typ, href string
		w, h      int
	}{
		{"img", "/a.png", 100, 100}, // size not configured
		{"img", "/a.png", 240, 0},   // preview-img.size is disabled
		{"mov", "/a.png", 240, 240}, // client type mismatch
		{"img", "/.hidden.png", 240, 240},
		{"img", "/out.png", 240, 240},     // symlink outside the root
		{"img", "/renamed.jpg", 240, 240}, // link name and target types differ
		{"img", "/fake.png", 240, 240},    // not an image
		{"img", "/../root/a.png", 240, 240},
		{"img", "/missing.png", 240, 240},
		{"img", "/a.png", 5000, 240},
	} {
		if name, ok := s.Thumb(tt.typ, tt.href, tt.w, tt.h); ok {
			t.Errorf("Thumb(%s, %s, %d, %d) = %s, want rejection", tt.typ, tt.href, tt.w, tt.h, name)
		}
	}
	for _, n := range []string{"../x", "capture-" + strings.Repeat("a", 40) + ".jpg", "thumb-x-1x1.jpg"} {
		if _, ok := s.Path(n); ok {
			t.Errorf("Path(%q) accepted", n)
		}
	}
}

func TestPreviewSample(t *testing.T) {
	s, _ := service(t, `{"preview-img": {"enabled": true, "size": 200}, "thumbnails": {"size": 240}}`)
	name, ok := s.Thumb("img", "/a.png", 200, 0)
	if !ok || !strings.HasSuffix(name, "-200x0.jpg") {
		t.Fatalf("sample = %q %v", name, ok)
	}
	p, _ := s.Path(name)
	f, _ := os.Open(p)
	defer f.Close()
	cfg, _ := jpeg.DecodeConfig(f)
	if cfg.Width != 200 || cfg.Height != 150 {
		t.Errorf("sample = %dx%d", cfg.Width, cfg.Height)
	}
}

func TestCacheCleanup(t *testing.T) {
	s, _ := service(t, `{"thumbnails": {"size": 240, "maxCacheSize": 1}}`)
	// fill the cache with old files (1.5 MiB)
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		p := filepath.Join(s.dir, "thumb-"+strings.Repeat(string(rune('a'+i)), 40)+"-240x240.jpg")
		writeFile(t, p, make([]byte, 512<<10))
		os.Chtimes(p, old.Add(time.Duration(i)*time.Minute), old.Add(time.Duration(i)*time.Minute))
	}
	if _, ok := s.Thumb("img", "/a.png", 240, 240); !ok {
		t.Fatal("thumb after cleanup")
	}
	var total int64
	for _, f := range s.cacheFiles() {
		total += f.size
	}
	if total > 1<<20 {
		t.Errorf("cache not cleaned up: %d bytes", total)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "thumb-"+strings.Repeat("a", 40)+"-240x240.jpg")); err == nil {
		t.Error("the least recently used file must go first")
	}
}

func TestVideoCapture(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, root := service(t, "")
	// a 2 second video: shorter than the 10 second capture offset
	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=10", filepath.Join(root, "v.mp4"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	if _, ok := s.Thumb("mov", "/v.mp4", 240, 240); !ok {
		t.Error("video thumbnail failed")
	}
}

func TestDocCapture(t *testing.T) {
	tool := ""
	for _, c := range []string{"magick"} {
		if _, err := exec.LookPath(c); err == nil {
			tool = c
			break
		}
	}
	if tool == "" {
		t.Skip("ImageMagick not installed")
	}
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "root")
	os.MkdirAll(root, 0o755)
	if out, err := exec.Command(tool, "-size", "300x400", "gradient:white-black", filepath.Join(root, "d.pdf")).CombinedOutput(); err != nil {
		t.Skipf("cannot create a pdf: %v %s", err, out)
	}
	cfg, _ := config.Load("", web.Conf())
	tr, _ := tree.New(root, cfg)
	s, _ := New(tr, cfg, filepath.Join(base, "cache"), func(c string) bool { _, err := exec.LookPath(c); return err == nil }, nil)
	if _, ok := s.Thumb("doc", "/d.pdf", 240, 240); !ok {
		t.Error("pdf thumbnail failed")
	}
}

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// jpegWithThumb encodes photo as JPEG with an EXIF orientation (IFD0) and
// an embedded thumbnail (IFD1).
func jpegWithThumb(t *testing.T, photo, thumb image.Image, o uint16) []byte {
	t.Helper()
	var main, small bytes.Buffer
	jpeg.Encode(&main, photo, &jpeg.Options{Quality: 90})
	jpeg.Encode(&small, thumb, &jpeg.Options{Quality: 60})
	bo := binary.BigEndian
	var tiff bytes.Buffer
	tiff.WriteString("MM")
	binary.Write(&tiff, bo, uint16(42))
	binary.Write(&tiff, bo, uint32(8))
	// IFD0 at 8: 1 entry, next IFD at 8+2+12+4 = 26
	binary.Write(&tiff, bo, uint16(1))
	binary.Write(&tiff, bo, []uint16{0x0112, 3})
	binary.Write(&tiff, bo, uint32(1))
	binary.Write(&tiff, bo, []uint16{o, 0})
	binary.Write(&tiff, bo, uint32(26))
	// IFD1 at 26: 2 entries, thumbnail data at 26+2+24+4 = 56
	binary.Write(&tiff, bo, uint16(2))
	binary.Write(&tiff, bo, []uint16{0x0201, 4})
	binary.Write(&tiff, bo, []uint32{1, 56})
	binary.Write(&tiff, bo, []uint16{0x0202, 4})
	binary.Write(&tiff, bo, []uint32{1, uint32(small.Len())})
	binary.Write(&tiff, bo, uint32(0))
	tiff.Write(small.Bytes())
	app1 := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(app1)+2))
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, app1...)
	return append(out, main.Bytes()[2:]...)
}

func TestEmbeddedEXIFThumb(t *testing.T) {
	dir := t.TempDir()
	blue, red := color.RGBA{0, 0, 255, 255}, color.RGBA{255, 0, 0, 255}
	tests := []struct {
		name         string
		photo, thumb image.Image
		o            uint16
		useExif      bool
		w, h         int
		wantRed      bool
	}{
		{"used", solid(640, 480, blue), solid(320, 240, red), 1, true, 240, 240, true},
		{"option off", solid(640, 480, blue), solid(320, 240, red), 1, false, 240, 240, false},
		{"too small", solid(640, 480, blue), solid(160, 120, red), 1, true, 240, 240, false},
		{"black bars (other ratio)", solid(640, 360, blue), solid(320, 240, red), 1, true, 240, 240, false},
		{"samples decode the photo", solid(640, 480, blue), solid(320, 240, red), 1, true, 240, 0, false},
		{"rotated", solid(640, 480, blue), solid(320, 240, red), 6, true, 240, 240, true},
	}
	for _, tt := range tests {
		p := filepath.Join(dir, "p.jpg")
		writeFile(t, p, jpegWithThumb(t, tt.photo, tt.thumb, tt.o))
		img, err := render(p, tt.w, tt.h, tt.useExif)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := near(img.At(5, 5), 255, 0, 0); got != tt.wantRed {
			t.Errorf("%s: embedded thumb used = %v, want %v", tt.name, got, tt.wantRed)
		}
	}
	// the rotated embedded thumbnail is turned like the photo
	p := filepath.Join(dir, "r.jpg")
	writeFile(t, p, jpegWithThumb(t, solid(640, 480, blue), solid(320, 240, red), 6))
	img, _ := render(p, 180, 240, true)
	if b := img.Bounds(); b.Dx() != 180 || b.Dy() != 240 || !near(img.At(5, 5), 255, 0, 0) {
		t.Errorf("rotated embedded thumb: %v", b)
	}
}

func TestServiceUsesEXIFOption(t *testing.T) {
	s, root := service(t, `{"thumbnails": {"size": 240, "exif": true}}`)
	writeFile(t, filepath.Join(root, "e.jpg"), jpegWithThumb(t, solid(640, 480, color.RGBA{0, 0, 255, 255}), solid(320, 240, color.RGBA{255, 0, 0, 255}), 1))
	name, ok := s.Thumb("img", "/e.jpg", 240, 240)
	if !ok {
		t.Fatal("thumb failed")
	}
	p, _ := s.Path(name)
	f, _ := os.Open(p)
	defer f.Close()
	img, _ := jpeg.Decode(f)
	if !near(img.At(5, 5), 255, 0, 0) {
		t.Error("thumbnails.exif must use the embedded thumbnail")
	}
}

// a 1x1 lossy WebP
const tinyWebP = "UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAwA0JaQAA3AA/vuUAAA="

func TestWebPThumb(t *testing.T) {
	s, root := service(t, "")
	data, _ := base64.StdEncoding.DecodeString(tinyWebP)
	writeFile(t, filepath.Join(root, "w.webp"), data)
	if _, ok := s.Thumb("img", "/w.webp", 240, 240); !ok {
		t.Error("webp thumbnail failed")
	}
}

func TestMagickImagesNeedImageMagick(t *testing.T) {
	s, root := service(t, "")
	s.hasCmd = func(string) bool { return false }
	writeFile(t, filepath.Join(root, "p.heic"), []byte("not really heic"))
	if _, ok := s.Thumb("img", "/p.heic", 240, 240); ok {
		t.Error("heic without ImageMagick must fail")
	}
}

func TestAVIFCapture(t *testing.T) {
	tool := ""
	for _, c := range []string{"magick"} {
		if _, err := exec.LookPath(c); err == nil {
			tool = c
			break
		}
	}
	if tool == "" {
		t.Skip("ImageMagick not installed")
	}
	s, root := service(t, "")
	s.hasCmd = func(c string) bool { _, err := exec.LookPath(c); return err == nil }
	p := filepath.Join(root, "a.avif")
	if out, err := exec.Command(tool, "-size", "64x48", "xc:red", p).CombinedOutput(); err != nil {
		t.Skipf("ImageMagick can't write AVIF here: %s", out)
	}
	if _, ok := s.Thumb("img", "/a.avif", 240, 240); !ok {
		t.Error("avif thumbnail failed")
	}
}

func TestRenderQueueBound(t *testing.T) {
	s, _ := service(t, "")
	s.waiting.Store(maxWaiting)
	if s.acquire() {
		t.Fatal("a full queue must refuse")
	}
	s.waiting.Store(0)
	if !s.acquire() {
		t.Fatal("slot expected")
	}
	<-s.sem
	if _, ok := s.Thumb("img", "/a.png", 240, 240); !ok {
		t.Error("thumb after the queue drained")
	}
}

// fakeCache returns the path of a fake cache file, aged as given.
func fakeCache(t *testing.T, s *Service, letter string, size int, age time.Duration) string {
	t.Helper()
	p := filepath.Join(s.dir, "thumb-"+strings.Repeat(letter, 40)+"-240x240.jpg")
	writeFile(t, p, make([]byte, size))
	when := time.Now().Add(-age)
	os.Chtimes(p, when, when)
	return p
}

func TestExpire(t *testing.T) {
	s, _ := service(t, `{"thumbnails": {"maxCacheTime": 7}}`)
	old := fakeCache(t, s, "a", 1000, 8*24*time.Hour)
	older := fakeCache(t, s, "b", 500, 30*24*time.Hour)
	recent := fakeCache(t, s, "c", 2000, 6*24*time.Hour)
	capture := filepath.Join(s.dir, "capture-"+strings.Repeat("d", 40)+".jpg")
	writeFile(t, capture, []byte("frame"))
	os.Chtimes(capture, time.Now().Add(-40*24*time.Hour), time.Now().Add(-40*24*time.Hour))
	foreign := filepath.Join(s.dir, "notes.txt")
	writeFile(t, foreign, []byte("not a thumbnail"))
	os.Chtimes(foreign, time.Now().Add(-90*24*time.Hour), time.Now().Add(-90*24*time.Hour))

	// the usage is known before, like after the first request
	s.usageMu.Lock()
	s.loadUsage()
	before := s.usage
	s.usageMu.Unlock()

	if n := s.Expire(time.Now()); n != 3 {
		t.Errorf("Expire removed %d files, want 3", n)
	}
	for _, p := range []string{old, older, capture} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s should have expired", filepath.Base(p))
		}
	}
	for _, p := range []string{recent, foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s must stay: %v", filepath.Base(p), err)
		}
	}
	s.usageMu.Lock()
	after := s.usage
	s.usageMu.Unlock()
	if want := before - 1000 - 500 - int64(len("frame")); after != want {
		t.Errorf("usage = %d, want %d", after, want)
	}
	// the time is the one of the given "now"
	if n := s.Expire(time.Now().Add(2 * 24 * time.Hour)); n != 1 {
		t.Errorf("2 days later Expire removed %d files, want 1", n)
	}
}

func TestExpireIsOffByDefault(t *testing.T) {
	for _, options := range []string{"", `{"thumbnails": {"maxCacheTime": 0}}`} {
		s, _ := service(t, options)
		p := fakeCache(t, s, "a", 100, 3650*24*time.Hour)
		if n := s.Expire(time.Now()); n != 0 {
			t.Errorf("options %q: Expire removed %d files", options, n)
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("options %q: a thumbnail expired: %v", options, err)
		}
	}
}

// Thumbnails that are used are renewed, so they do not expire.
func TestUsedThumbnailsDoNotExpire(t *testing.T) {
	s, _ := service(t, `{"thumbnails": {"size": 240, "maxCacheTime": 7}}`)
	name, ok := s.Thumb("img", "/a.png", 240, 240)
	if !ok {
		t.Fatal("thumbnail failed")
	}
	p, _ := s.Path(name)
	// created 10 days ago, but used now: the request renews it
	when := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(p, when, when)
	if _, ok := s.Thumb("img", "/a.png", 240, 240); !ok {
		t.Fatal("second request failed")
	}
	if n := s.Expire(time.Now()); n != 0 {
		t.Errorf("a used thumbnail expired (%d removed)", n)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("thumbnail gone: %v", err)
	}
}

func TestRunExpiry(t *testing.T) {
	s, _ := service(t, `{"thumbnails": {"maxCacheTime": 1}}`)
	first := fakeCache(t, s, "a", 100, 5*24*time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunExpiry(ctx, 20*time.Millisecond); close(done) }()

	waitGone := func(p string) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(p); err != nil {
				return
			}
		}
		t.Errorf("%s did not expire", filepath.Base(p))
	}
	waitGone(first) // the run at the start
	second := fakeCache(t, s, "b", 100, 5*24*time.Hour)
	waitGone(second) // a later run

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("RunExpiry did not stop with the context")
	}
}

// Types without a Go decoder have to go through ImageMagick: a type that is
// in the default list but in neither place never gets a thumbnail (ICO was
// once like that).
func TestDefaultImageTypesAreDecodable(t *testing.T) {
	goDecoded := map[string]bool{"img-bmp": true, "img-gif": true, "img-jpg": true, "img-png": true, "img-webp": true}
	for _, typ := range defaultCategoryTypes["img"] {
		_, magick := magickImages[typ]
		if !goDecoded[typ] && !magick {
			t.Errorf("%s is a default image type, but neither Go nor ImageMagick converts it", typ)
		}
		if goDecoded[typ] && magick {
			t.Errorf("%s is decoded by Go, it must not go through ImageMagick", typ)
		}
	}
	for typ := range magickImages {
		if !slices.Contains(defaultCategoryTypes["img"], typ) {
			t.Errorf("%s is converted, but not in the default image types", typ)
		}
	}
}

// ImageMagick reads far more complex formats: a source above the limit is
// refused without starting it.
func TestMagickSourceLimit(t *testing.T) {
	s, root := service(t, "")
	started := false
	s.hasCmd = func(string) bool { started = true; return true }
	big := filepath.Join(root, "big.tiff")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxMagickSourceBytes + 1); err != nil {
		t.Skipf("no sparse file here: %v", err)
	}
	f.Close()
	if _, ok := s.Thumb("img", "/big.tiff", 240, 240); ok {
		t.Error("a source above the limit got a thumbnail")
	}
	if started {
		t.Error("ImageMagick was asked about a source above the limit")
	}
}

func TestMagickFormats(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not installed")
	}
	s, root := service(t, "")
	s.hasCmd = func(c string) bool { _, err := exec.LookPath(c); return err == nil }
	for _, name := range []string{"a.tiff", "b.tif", "c.ico", "d.psd", "e.tga", "f.jxl", "g.jp2"} {
		p := filepath.Join(root, name)
		if out, err := exec.Command("magick", "-size", "64x48", "gradient:red-blue", p).CombinedOutput(); err != nil {
			t.Logf("ImageMagick can't write %s here: %s", name, out)
			continue
		}
		thumb, ok := s.Thumb("img", "/"+name, 240, 240)
		if !ok {
			t.Errorf("%s: no thumbnail", name)
			continue
		}
		file, _ := s.Path(thumb)
		f, err := os.Open(file)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		cfg, err := jpeg.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width != 240 || cfg.Height != 240 {
			t.Errorf("%s: thumbnail %dx%d, %v", name, cfg.Width, cfg.Height, err)
		}
	}
}

// Transparent areas become white, like for the images Go decodes.
func TestMagickTransparencyIsWhite(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not installed")
	}
	s, root := service(t, "")
	s.hasCmd = func(c string) bool { _, err := exec.LookPath(c); return err == nil }
	p := filepath.Join(root, "clear.tiff")
	if out, err := exec.Command("magick", "-size", "64x48", "xc:none", p).CombinedOutput(); err != nil {
		t.Skipf("ImageMagick can't write TIFF here: %s", out)
	}
	capture, ok := s.captureImage("img-tiff", p)
	if !ok {
		t.Fatal("conversion failed")
	}
	f, err := os.Open(capture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(10, 10).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent pixel is (%d,%d,%d), want white", r>>8, g>>8, b>>8)
	}
}
