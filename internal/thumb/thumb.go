// Package thumb renders and caches thumbnails of images, videos (ffmpeg,
// avconv) and documents (ImageMagick, GraphicsMagick) like h5fs.
package thumb

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/tree"
)

const (
	maxThumbDimension = 4096
	maxThumbPixels    = 4096 * 4096
	defaultThumbSize  = 100 // same default as the client
	landscapeRatio    = 4.0 / 3.0
	touchInterval     = 24 * time.Hour
	captureTimeout    = 60 * time.Second
	defaultMaxCacheMB = 512
	cleanupRatio      = 0.8
	lockStripes       = 64
)

var (
	fileRe = regexp.MustCompile(`^(thumb|capture)-[0-9a-f]{40}(-\d+x\d+)?\.jpg$`)
	nameRe = regexp.MustCompile(`^thumb-[0-9a-f]{40}-\d+x\d+\.jpg$`)
)

var defaultCategoryTypes = map[string][]string{
	"img": {"img-bmp", "img-gif", "img-ico", "img-jpg", "img-png"},
	"mov": {"vid-avi", "vid-flv", "vid-mkv", "vid-mov", "vid-mp4", "vid-mpg", "vid-webm"},
	"doc": {"x-pdf", "x-ps"},
}

// file type => ffmpeg demuxer
var movFormats = map[string]string{
	"vid-avi": "avi", "vid-flv": "flv", "vid-mkv": "matroska", "vid-mov": "mov",
	"vid-mp4": "mov", "vid-mpg": "mpeg", "vid-ts": "mpegts", "vid-vob": "mpeg",
	"vid-webm": "matroska", "vid-wmv": "asf",
}

// file type => ImageMagick/GraphicsMagick coder
var docFormats = map[string]string{"x-pdf": "pdf", "x-ps": "ps", "x-eps": "eps"}

// Service creates thumbnails in a cache folder.
type Service struct {
	tree   *tree.Tree
	cfg    *config.Config
	dir    string
	hasCmd func(string) bool
	log    *slog.Logger

	sem   chan struct{}
	locks [lockStripes]sync.Mutex

	usageMu    sync.Mutex
	usage      int64
	usageKnown bool
}

// New creates the service; thumbnails are stored in cacheDir/thumbs.
func New(tr *tree.Tree, cfg *config.Config, cacheDir string, hasCmd func(string) bool, log *slog.Logger) (*Service, error) {
	dir := filepath.Join(cacheDir, "thumbs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		tree: tr, cfg: cfg, dir: dir, hasCmd: hasCmd, log: log,
		sem: make(chan struct{}, min(runtime.NumCPU(), 4)),
	}, nil
}

// Path returns the file of a thumbnail name, if it is one.
func (s *Service) Path(name string) (string, bool) {
	if !nameRe.MatchString(name) {
		return "", false
	}
	return filepath.Join(s.dir, name), true
}

// Thumb returns the name of the thumbnail of the file at href, creating
// it if needed. typ is the category the client expects ("img", "mov",
// "doc"); it must match the category derived on the server.
func (s *Service) Thumb(typ, href string, width, height int) (string, bool) {
	if !validDimensions(width, height) || !s.allowedSize(typ, width, height) {
		return "", false
	}
	requested, err := s.tree.ToPath(href)
	if err != nil || s.tree.IsHidden(filepath.Base(requested)) {
		return "", false
	}
	src, ok := s.tree.ResolveManagedFile(requested)
	if !ok {
		return "", false
	}
	// Never trust the client's type: derive it from the requested name and
	// the real file name (for symbolic links), both have to agree.
	fileType := s.cfg.FileType(filepath.Base(src))
	category := s.category(fileType)
	if category == "" || category != typ || s.cfg.FileType(filepath.Base(requested)) != fileType {
		return "", false
	}

	switch category {
	case "mov":
		src, ok = s.captureMov(fileType, src)
	case "doc":
		src, ok = s.captureDoc(fileType, src)
	}
	if !ok {
		return "", false
	}
	return s.thumb(src, width, height)
}

func validDimensions(w, h int) bool {
	if w <= 0 || h < 0 || w > maxThumbDimension || h > maxThumbDimension {
		return false
	}
	return h == 0 || w*h <= maxThumbPixels
}

// allowedSize only accepts the sizes the client requests with the current
// settings, so the number of cached thumbnails per file stays bounded.
func (s *Service) allowedSize(typ string, w, h int) bool {
	size := defaultThumbSize
	if _, present := s.cfg.Get("thumbnails.size"); present {
		size, _ = s.cfg.PositiveInt("thumbnails.size")
	}
	if size > 0 && h == size && (w == size || w == int(math.Round(float64(size)*landscapeRatio))) {
		return true
	}
	if typ == "img" && h == 0 && s.cfg.IsTrue("preview-img.enabled") {
		sample, ok := s.cfg.PositiveInt("preview-img.size")
		return ok && w == sample
	}
	return false
}

func (s *Service) category(fileType string) string {
	for _, cat := range []string{"img", "mov", "doc"} {
		if slices.Contains(s.cfg.StringsOr("thumbnails."+cat, defaultCategoryTypes[cat]), fileType) {
			return cat
		}
	}
	return ""
}

func sourceID(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:])
}

func (s *Service) lock(name string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(name))
	return &s.locks[h.Sum32()%lockStripes]
}

// fresh reports whether a cache file exists and is newer than its source,
// refreshing its time at most once a day for the LRU cleanup.
func fresh(cached string, src os.FileInfo) bool {
	fi, err := os.Stat(cached)
	if err != nil || !src.ModTime().Before(fi.ModTime()) {
		return false
	}
	if time.Since(fi.ModTime()) > touchInterval {
		now := time.Now()
		os.Chtimes(cached, now, now)
	}
	return true
}

func (s *Service) thumb(src string, width, height int) (string, bool) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", false
	}
	name := fmt.Sprintf("thumb-%s-%dx%d.jpg", sourceID(src), width, height)
	path := filepath.Join(s.dir, name)

	mu := s.lock(name)
	mu.Lock()
	defer mu.Unlock()
	if fresh(path, srcInfo) {
		return name, true
	}
	if !s.reserve() {
		return "", false
	}
	s.sem <- struct{}{}
	img, err := render(src, width, height)
	<-s.sem
	if err != nil {
		return "", false
	}
	old := fileSize(path)
	if err := writeJPEG(path, img); err != nil {
		s.log.Warn("write thumbnail", "err", err)
		return "", false
	}
	s.add(fileSize(path) - old)
	return name, true
}

func (s *Service) captureMov(fileType, src string) (string, bool) {
	format, ok := movFormats[fileType]
	if !ok {
		return "", false
	}
	var cmd func(at, dest string) []string
	switch {
	case s.hasCmd("ffmpeg"):
		// forcing the demuxer (and only allowing it and the file protocol)
		// keeps ffmpeg from probing other formats or protocols (e.g. HLS)
		cmd = func(at, dest string) []string {
			return []string{"ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
				"-protocol_whitelist", "file", "-format_whitelist", format, "-f", format,
				"-ss", at, "-i", src, "-an", "-frames:v", "1", "-f", "image2", "-update", "1", dest}
		}
	case s.hasCmd("avconv"):
		cmd = func(at, dest string) []string {
			return []string{"avconv", "-nostdin", "-y", "-f", format, "-ss", at, "-i", src, "-an", "-vframes", "1", dest}
		}
	default:
		return "", false
	}
	// videos shorter than 10 seconds: fall back to the first frame
	return s.capture(src, cmd("0:00:10", ""), cmd("0:00:00", ""))
}

func (s *Service) captureDoc(fileType, src string) (string, bool) {
	format, ok := docFormats[fileType]
	if !ok {
		return "", false
	}
	// an explicit coder: the input is never auto-detected from its content
	in := format + ":" + src + "[0]"
	switch {
	case s.hasCmd("magick"): // ImageMagick 7, where "convert" is deprecated
		// operators like -strip have to follow the input in ImageMagick 7
		return s.capture(src, []string{"magick", "-density", "200", in, "-quality", "100", "-strip", "jpg:"})
	case s.hasCmd("convert"):
		return s.capture(src, []string{"convert", "-density", "200", "-quality", "100", "-strip", in, "jpg:"})
	case s.hasCmd("gm"):
		return s.capture(src, []string{"gm", "convert", "-density", "200", "-quality", "100", in, "jpg:"})
	}
	return "", false
}

// capture runs the first command line that produces a capture image. The
// destination is appended to the last argument.
func (s *Service) capture(src string, cmdlines ...[]string) (string, bool) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", false
	}
	name := "capture-" + sourceID(src) + ".jpg"
	dest := filepath.Join(s.dir, name)

	mu := s.lock(name)
	mu.Lock()
	defer mu.Unlock()
	if fresh(dest, srcInfo) {
		return dest, true
	}
	if !s.reserve() {
		return "", false
	}
	old := fileSize(dest)
	os.Remove(dest)
	for _, args := range cmdlines {
		args = slices.Clone(args)
		args[len(args)-1] += dest
		s.sem <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.WaitDelay = 5 * time.Second
		err := cmd.Run()
		cancel()
		<-s.sem
		if err == nil && fileSize(dest) > 0 {
			break
		}
		os.Remove(dest)
	}
	s.add(fileSize(dest) - old)
	if fileSize(dest) == 0 {
		return "", false
	}
	return dest, true
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func (s *Service) maxBytes() int64 {
	mb := defaultMaxCacheMB
	if v, ok := s.cfg.PositiveInt("thumbnails.maxCacheSize"); ok {
		mb = v
	}
	return int64(mb) << 20
}

// reserve reports whether a new cache file may be written, cleaning up
// the least recently used files once the cache is full.
func (s *Service) reserve() bool {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	s.loadUsage()
	if s.usage >= s.maxBytes() {
		s.usage = s.cleanup()
	}
	return s.usage < s.maxBytes()
}

func (s *Service) add(delta int64) {
	if delta == 0 {
		return
	}
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	s.loadUsage()
	s.usage = max(0, s.usage+delta)
	if s.usage > s.maxBytes() {
		s.usage = s.cleanup()
	}
}

func (s *Service) loadUsage() {
	if s.usageKnown {
		return
	}
	s.usage = 0
	for _, f := range s.cacheFiles() {
		s.usage += f.size
	}
	s.usageKnown = true
}

type cacheFile struct {
	path  string
	mtime time.Time
	size  int64
}

func (s *Service) cacheFiles() []cacheFile {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	files := make([]cacheFile, 0, len(entries))
	for _, e := range entries {
		if !fileRe.MatchString(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil {
			files = append(files, cacheFile{filepath.Join(s.dir, e.Name()), fi.ModTime(), fi.Size()})
		}
	}
	return files
}

// cleanup removes the least recently used files until the usage is below
// the cleanup target and returns the new usage.
func (s *Service) cleanup() int64 {
	files := s.cacheFiles()
	var usage int64
	for _, f := range files {
		usage += f.size
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
	target := int64(float64(s.maxBytes()) * cleanupRatio)
	for _, f := range files {
		if usage <= target {
			break
		}
		if os.Remove(f.path) == nil {
			usage -= f.size
		}
	}
	return usage
}
