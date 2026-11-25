// Package thumb renders and caches thumbnails of images, videos (ffmpeg)
// and documents (ImageMagick).
package thumb

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
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
	"sync/atomic"
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
	maxWaiting        = 32 // renders waiting for a free slot
)

var (
	fileRe = regexp.MustCompile(`^(thumb|capture)-[0-9a-f]{40,56}(-\d+x\d+)?\.jpg$`)
	nameRe = regexp.MustCompile(`^thumb-[0-9a-f]{56}-\d+x\d+\.jpg$`)
)

var defaultCategoryTypes = map[string][]string{
	"img": {"img-avif", "img-bmp", "img-gif", "img-heic", "img-ico", "img-jp2", "img-jpg", "img-jxl", "img-png", "img-tga", "img-tiff", "img-webp", "x-psd"},
	"mov": {"vid-avi", "vid-flv", "vid-mkv", "vid-mov", "vid-mp4", "vid-mpg", "vid-webm"},
	"doc": {"x-pdf", "x-ps"},
}

// file type => ffmpeg demuxer
var movFormats = map[string]string{
	"vid-avi": "avi", "vid-flv": "flv", "vid-mkv": "matroska", "vid-mov": "mov",
	"vid-mp4": "mov", "vid-mpg": "mpeg", "vid-ts": "mpegts", "vid-vob": "mpeg",
	"vid-webm": "matroska", "vid-wmv": "asf",
}

// file type => ImageMagick coder, for images Go can't decode (the types Go
// decodes itself are bmp, gif, jpg, png and webp)
var magickImages = map[string]string{
	"img-avif": "avif", "img-heic": "heic", "img-ico": "ico", "img-jp2": "jp2",
	"img-jxl": "jxl", "img-tga": "tga", "img-tiff": "tiff", "x-psd": "psd",
}

// Limits for ImageMagick, which reads formats that are far less simple than
// the ones of Go: the size of the source and the resources of a conversion.
const maxMagickSourceBytes = 256 << 20

var magickLimits = []string{"-limit", "memory", "512MiB", "-limit", "map", "1GiB", "-limit", "disk", "2GiB", "-limit", "time", "50"}

// file type => ImageMagick/GraphicsMagick coder
var docFormats = map[string]string{"x-pdf": "pdf", "x-ps": "ps", "x-eps": "eps"}

// Service creates thumbnails in a cache folder.
type Service struct {
	tree   *tree.Tree
	cfg    *config.Config
	dir    string
	hasCmd func(string) bool
	log    *slog.Logger

	sem     chan struct{}
	waiting atomic.Int64 // renders and captures waiting for sem
	locks   [lockStripes]sync.Mutex

	usageMu    sync.Mutex
	usage      int64
	usageKnown bool

	// the file each served thumbnail was made of (the requested path), so a
	// thumbnail is only served while its source still may be: the cache
	// outlives deleted, hidden and no longer listed files
	sourcesMu sync.Mutex
	sources   map[string]thumbSource
}

type thumbSource struct {
	requested string
	real      string
	info      os.FileInfo
}

// maxSources bounds the sources that are remembered; the list refills as
// clients ask for thumbnails again.
const maxSources = 200000

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
	src, info, ok := s.tree.ResolveManagedFileInfo(requested)
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

	f, ok := s.tree.OpenChecked(src, info)
	if !ok {
		return "", false
	}
	defer f.Close()
	name, ok := s.thumb(src, f, info, fileType, category, width, height)
	if ok {
		s.remember(name, thumbSource{requested, src, info})
	}
	return name, ok
}

// remember notes the file a thumbnail was made of.
func (s *Service) remember(name string, source thumbSource) {
	s.sourcesMu.Lock()
	defer s.sourcesMu.Unlock()
	if s.sources == nil || len(s.sources) >= maxSources {
		s.sources = map[string]thumbSource{}
	}
	s.sources[name] = source
}

// forget drops the source of a thumbnail that was removed from the cache.
func (s *Service) forget(name string) {
	s.sourcesMu.Lock()
	delete(s.sources, name)
	s.sourcesMu.Unlock()
}

// ForgetSources drops all remembered sources, as a restart does: the
// thumbnails of the cache are served again once a client asked for them.
func (s *Service) ForgetSources() {
	s.sourcesMu.Lock()
	s.sources = nil
	s.sourcesMu.Unlock()
}

// Allowed reports whether a cached thumbnail may be served: a client asked
// for it since the start and its source still passes the checks that were
// made when the thumbnail was created.
func (s *Service) Allowed(name string) bool {
	s.sourcesMu.Lock()
	source, ok := s.sources[name]
	s.sourcesMu.Unlock()
	if !ok || s.tree.IsHidden(filepath.Base(source.requested)) {
		return false
	}
	real, info, ok := s.tree.ResolveManagedFileInfo(source.requested)
	return ok && real == source.real && sameSource(info, source.info)
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

func (s *Service) lock(name string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(name))
	return &s.locks[h.Sum32()%lockStripes]
}

// fresh checks a cache entry named for a specific source version, and
// refreshes its time at most once a day for the LRU cleanup.
func fresh(cached string) bool {
	fi, err := os.Stat(cached)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	if time.Since(fi.ModTime()) > touchInterval {
		now := time.Now()
		os.Chtimes(cached, now, now)
	}
	return true
}

func (s *Service) thumb(real string, source *os.File, info os.FileInfo, fileType, category string, width, height int) (string, bool) {
	id := sourceID(real, info)
	src := checkedSource{id, real, source, info}
	name := fmt.Sprintf("thumb-%s-%dx%d.jpg", id, width, height)
	path := filepath.Join(s.dir, name)
	mu := s.lock(name)
	mu.Lock()
	if fresh(path) {
		mu.Unlock()
		return name, true
	}
	mu.Unlock()

	// Finish any conversion before taking the thumbnail lock. A capture and
	// a thumbnail can hash to the same lock stripe.
	var captured string
	ok := true
	switch category {
	case "img":
		if format, convert := magickImages[fileType]; convert {
			captured, ok = s.captureImage(src, format)
		}
	case "mov":
		captured, ok = s.captureMov(src, fileType)
	case "doc":
		captured, ok = s.captureDoc(src, fileType)
	}
	if !ok {
		return "", false
	}

	mu.Lock()
	defer mu.Unlock()
	if fresh(path) {
		return name, true
	}
	if !s.reserve() || !s.acquire() {
		return "", false
	}
	defer func() { <-s.sem }()
	input := source
	if captured != "" {
		var err error
		input, err = os.Open(captured)
		if err != nil {
			return "", false
		}
		defer input.Close()
	}
	// Embedded EXIF thumbnails are used only for thumbnails, not samples.
	useExif := s.cfg.IsTrue("thumbnails.exif") && height != 0
	img, err := render(input, width, height, useExif)
	if err != nil {
		return "", false
	}
	current, err := source.Stat()
	if err != nil || !sameSource(current, info) {
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

// checkedSource is a source that was opened through the tree.
type checkedSource struct {
	id   string
	real string
	file *os.File
	info os.FileInfo
}

func (s *Service) captureMov(src checkedSource, fileType string) (string, bool) {
	format, ok := movFormats[fileType]
	if !ok || !s.hasCmd("ffmpeg") {
		return "", false
	}
	return s.capture(src, func(in, dest string) [][]string {
		// Force the demuxer and only allow the file protocol.
		command := func(at string) []string {
			return []string{"ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
				"-protocol_whitelist", "file", "-format_whitelist", format, "-f", format,
				"-ss", at, "-i", in, "-an", "-frames:v", "1", "-f", "image2", "-update", "1", dest}
		}
		return [][]string{command("0:00:10"), command("0:00:00")}
	})
}

// captureImage converts images Go can't decode (AVIF, HEIC, ICO, JPEG 2000,
// JPEG XL, TGA, TIFF, PSD) to JPEG with ImageMagick. Transparent areas
// become white, CMYK is converted to sRGB.
func (s *Service) captureImage(src checkedSource, format string) (string, bool) {
	if src.info.Size() > maxMagickSourceBytes || !s.hasCmd("magick") {
		return "", false
	}
	return s.capture(src, func(path, dest string) [][]string {
		// an explicit coder: the input is never auto-detected from its content
		in := format + ":" + path + "[0]"
		resize := fmt.Sprintf("%dx%d>", maxThumbDimension, maxThumbDimension)
		args := append([]string{"magick"}, magickLimits...)
		args = append(args, in, "-auto-orient", "-colorspace", "sRGB", "-background", "white", "-alpha", "remove", "-alpha", "off",
			"-resize", resize, "-quality", "90", "-strip", "jpg:"+dest)
		return [][]string{args}
	})
}

func (s *Service) captureDoc(src checkedSource, fileType string) (string, bool) {
	format, ok := docFormats[fileType]
	if !ok || !s.hasCmd("magick") {
		return "", false
	}
	return s.capture(src, func(path, dest string) [][]string {
		// an explicit coder: the input is never auto-detected from its content
		in := format + ":" + path + "[0]"
		args := append([]string{"magick"}, magickLimits...)
		args = append(args, "-density", "200", in, "-quality", "100", "-strip", "jpg:"+dest)
		return [][]string{args}
	})
}

// capture converts an already opened source with the first command line
// that produces an image. The converter reads the checked file without a
// copy, see converterInput; nothing is kept if it changed meanwhile.
func (s *Service) capture(src checkedSource, commands func(in, dest string) [][]string) (string, bool) {
	if src.info.Size() <= 0 {
		return "", false
	}
	name := "capture-" + src.id + ".jpg"
	dest := filepath.Join(s.dir, name)
	mu := s.lock(name)
	mu.Lock()
	defer mu.Unlock()
	if fresh(dest) {
		return dest, true
	}
	if !s.reserve() || !s.acquire() {
		return "", false
	}
	defer func() { <-s.sem }()

	old := fileSize(dest)
	os.Remove(dest)
	in, extra := converterInput(src.file, src.real)
	for _, args := range commands(in, dest) {
		if _, err := src.file.Seek(0, io.SeekStart); err != nil {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.ExtraFiles = extra
		cmd.WaitDelay = 5 * time.Second
		err := cmd.Run()
		cancel()
		if err == nil && fileSize(dest) > 0 {
			break
		}
		os.Remove(dest)
	}
	if current, err := src.file.Stat(); err != nil || !sameSource(current, src.info) {
		os.Remove(dest)
	}
	s.add(fileSize(dest) - old)
	if fileSize(dest) == 0 {
		return "", false
	}
	return dest, true
}

// acquire takes a render slot. It refuses instead of queueing without
// bound: at most maxWaiting renders wait for a slot.
func (s *Service) acquire() bool {
	if s.waiting.Add(1) > maxWaiting {
		s.waiting.Add(-1)
		return false
	}
	s.sem <- struct{}{}
	s.waiting.Add(-1)
	return true
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

// maxAge is the time after which an unused thumbnail is removed
// ("thumbnails.maxCacheTime" days), 0 if thumbnails do not expire.
func (s *Service) maxAge() time.Duration {
	if days, ok := s.cfg.PositiveInt("thumbnails.maxCacheTime"); ok {
		return time.Duration(days) * 24 * time.Hour
	}
	return 0
}

// Expire removes the thumbnails that were not used for "thumbnails.maxCacheTime"
// days and returns how many. The modification time of a file is refreshed
// when it is used (at most once a day), so it is the time of the last use.
// Nothing is removed if the option is not set.
func (s *Service) Expire(now time.Time) int {
	age := s.maxAge()
	if age == 0 {
		return 0
	}
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	s.loadUsage()
	cutoff := now.Add(-age)
	removed := 0
	for _, f := range s.cacheFiles() {
		if f.mtime.Before(cutoff) && os.Remove(f.path) == nil {
			s.usage = max(0, s.usage-f.size)
			s.forget(filepath.Base(f.path))
			removed++
		}
	}
	if removed > 0 {
		s.log.Info("expired thumbnails removed", "count", removed)
	}
	return removed
}

// RunExpiry removes the expired thumbnails now and then every interval,
// until ctx ends. The option is read on every run, so changes of the
// configuration apply without a restart.
func (s *Service) RunExpiry(ctx context.Context, interval time.Duration) {
	s.Expire(time.Now())
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.Expire(now)
		}
	}
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
			s.forget(filepath.Base(f.path))
		}
	}
	return usage
}
