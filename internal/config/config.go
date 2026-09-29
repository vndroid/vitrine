// Package config loads the h5fs-compatible configuration: options.json,
// types.json and the l10n translations. Files in the config directory
// override the embedded defaults file by file. Like h5fs, which read the
// files on every request, changes are picked up while running (Watch,
// Reload); requests keep using the snapshot they started with.
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vndroid/vitrine/internal/jsonc"
	"github.com/vndroid/vitrine/internal/pattern"
)

var isoCodeRe = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2})?$`)

// Config is the configuration. Its content is an immutable snapshot that
// Reload replaces atomically.
type Config struct {
	dir      string
	defaults fs.FS

	mu         sync.Mutex // serializes loading
	loginCheck func(passhash string) bool
	cur        atomic.Pointer[snapshot]
}

type snapshot struct {
	options  map[string]any
	passhash string
	types    json.RawMessage
	typeREs  []typeRE
	hidden   []*regexp.Regexp
	l10n     map[string]map[string]any
	langs    map[string]string
}

type typeRE struct {
	name string
	re   *regexp.Regexp
}

// Load reads the configuration from dir (may be empty), falling back to
// the embedded defaults for every file missing there.
func Load(dir string, defaults fs.FS) (*Config, error) {
	c := &Config{dir: dir, defaults: defaults}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	c.cur.Store(s)
	return c, nil
}

// Reload reads the configuration again. On errors the current one stays.
func (c *Config) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, err := c.load()
	if err != nil {
		return err
	}
	c.cur.Store(s)
	return nil
}

// SetLoginCheck sets how the passhash is validated; the client reads the
// result as "hasCustomPasshash" (name kept for compatibility).
func (c *Config) SetLoginCheck(check func(passhash string) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loginCheck = check
	old := c.snap()
	s := *old
	s.options = make(map[string]any, len(old.options)+1)
	for k, v := range old.options {
		s.options[k] = v
	}
	s.options["hasCustomPasshash"] = check(s.passhash)
	c.cur.Store(&s)
}

func (c *Config) snap() *snapshot { return c.cur.Load() }

func (c *Config) load() (*snapshot, error) {
	src := layered{dir: c.dir, defaults: c.defaults}
	s := &snapshot{}

	// options.json of the config directory is merged over the defaults, so
	// a partial file can't drop defaults like the hidden patterns
	raw, err := fs.ReadFile(c.defaults, "options.json")
	if err != nil {
		return nil, err
	}
	if err := decode(raw, &s.options); err != nil {
		return nil, fmt.Errorf("default options.json: %w", err)
	}
	if s.options == nil {
		s.options = map[string]any{}
	}
	if raw, ok, err := src.readOverride("options.json"); err != nil {
		return nil, err
	} else if ok {
		var override map[string]any
		if err := decode(raw, &override); err != nil {
			return nil, fmt.Errorf("options.json: %w", err)
		}
		merge(s.options, override)
	}
	if p, ok := s.options["passhash"].(string); ok {
		s.passhash = strings.TrimSpace(p)
	}
	delete(s.options, "passhash")
	if c.loginCheck != nil {
		s.options["hasCustomPasshash"] = c.loginCheck(s.passhash)
	}

	if s.types, s.typeREs, err = loadTypes(src); err != nil {
		return nil, err
	}

	for _, p := range stringList(s, "view.hidden") {
		re, err := pattern.Compile(p, false)
		if err != nil {
			slog.Warn("ignoring unsupported view.hidden pattern", "pattern", p, "err", err)
			continue
		}
		s.hidden = append(s.hidden, re)
	}

	if s.l10n, s.langs, err = loadL10n(src); err != nil {
		return nil, err
	}
	return s, nil
}

// Watch reloads the configuration whenever the files of the config
// directory change, checking every interval until ctx is done.
func (c *Config) Watch(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if c.dir == "" {
		return
	}
	last := c.fingerprint()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		fp := c.fingerprint()
		if fp == last {
			continue
		}
		last = fp
		if err := c.Reload(); err != nil {
			log.Error("config reload failed, keeping the current config", "err", err)
		} else {
			log.Info("config reloaded", "dir", c.dir)
		}
	}
}

// fingerprint summarizes name, size and time of the config files.
func (c *Config) fingerprint() string {
	var b strings.Builder
	add := func(p string) {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", p, fi.Size(), fi.ModTime().UnixNano())
		} else {
			fmt.Fprintf(&b, "%s:-;", p)
		}
	}
	add(filepath.Join(c.dir, "options.json"))
	add(filepath.Join(c.dir, "types.json"))
	l10n := filepath.Join(c.dir, "l10n")
	if entries, err := os.ReadDir(l10n); err == nil {
		for _, e := range entries {
			add(filepath.Join(l10n, e.Name()))
		}
	}
	return b.String()
}

// Passhash returns the configured admin password hash (may be empty).
func (c *Config) Passhash() string { return c.snap().passhash }

// Options returns the options as sent to the client (without passhash).
func (c *Config) Options() map[string]any { return c.snap().options }

// Types returns types.json as sent to the client, in its original order.
func (c *Config) Types() json.RawMessage { return c.snap().types }

// Langs maps the available iso codes to the language names.
func (c *Config) Langs() map[string]string { return c.snap().langs }

// L10n returns the translations of an iso code.
func (c *Config) L10n(isoCode string) (map[string]any, bool) {
	t, ok := c.snap().l10n[isoCode]
	return t, ok
}

// Get returns the option at a dot separated key path.
func (c *Config) Get(keypath string) (any, bool) {
	return get(c.snap(), keypath)
}

func get(s *snapshot, keypath string) (any, bool) {
	var v any = s.options
	for _, key := range strings.Split(keypath, ".") {
		if key == "" {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[key]; !ok {
			return nil, false
		}
	}
	return v, true
}

// Bool returns the option with PHP truthiness, like the h5fs checks.
func (c *Config) Bool(keypath string, def bool) bool {
	v, ok := c.Get(keypath)
	if !ok {
		return def
	}
	return truthy(v)
}

// IsTrue reports whether the option is exactly the boolean true.
func (c *Config) IsTrue(keypath string) bool {
	v, _ := c.Get(keypath)
	b, ok := v.(bool)
	return ok && b
}

// Int returns an integer option (a JSON integer or a digit string), or def.
func (c *Config) Int(keypath string, def int) int {
	v, ok := c.Get(keypath)
	if !ok {
		return def
	}
	switch n := v.(type) {
	case json.Number:
		if i, err := strconv.Atoi(n.String()); err == nil {
			return i
		}
	case string:
		if n != "" && strings.Trim(n, "0123456789") == "" {
			if i, err := strconv.Atoi(n); err == nil {
				return i
			}
		}
	}
	return def
}

// PositiveInt returns an option that is a JSON integer > 0.
func (c *Config) PositiveInt(keypath string) (int, bool) {
	v, _ := c.Get(keypath)
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(n.String())
	if err != nil || i <= 0 {
		return 0, false
	}
	return i, true
}

// String returns a string option, or def.
func (c *Config) String(keypath, def string) string {
	if s, ok := c.getString(keypath); ok {
		return s
	}
	return def
}

func (c *Config) getString(keypath string) (string, bool) {
	v, _ := c.Get(keypath)
	s, ok := v.(string)
	return s, ok
}

// Strings returns the string elements of an array option.
func (c *Config) Strings(keypath string) []string {
	return stringList(c.snap(), keypath)
}

func stringList(s *snapshot, keypath string) []string {
	v, _ := get(s, keypath)
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// StringsOr is like Strings, but returns def if the option is no array.
func (c *Config) StringsOr(keypath string, def []string) []string {
	if v, _ := c.Get(keypath); v != nil {
		if _, ok := v.([]any); ok {
			return c.Strings(keypath)
		}
	}
	return def
}

// IsHidden applies the "view.hidden" patterns to a name or href.
func (c *Config) IsHidden(name string) bool {
	if name == "." || name == ".." {
		return true
	}
	for _, re := range c.snap().hidden {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// FileType matches a file name against the types.json glob patterns
// (case insensitive, the last matching type wins), like the client.
func (c *Config) FileType(name string) string {
	result := "file"
	for _, t := range c.snap().typeREs {
		if t.re.MatchString(name) {
			result = t.name
		}
	}
	return result
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case json.Number:
		f, err := t.Float64()
		return err == nil && f != 0
	case string:
		return t != "" && t != "0"
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}

func decode(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(jsonc.Strip(raw)))
	dec.UseNumber()
	return dec.Decode(v)
}

// loadTypes keeps types.json in its original key order: both the client
// and FileType let the last matching type win.
func loadTypes(src layered) (json.RawMessage, []typeRE, error) {
	raw, err := src.read("types.json")
	if err != nil {
		return nil, nil, err
	}
	stripped := jsonc.Strip(raw)
	dec := json.NewDecoder(bytes.NewReader(stripped))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, errors.New("types.json: object expected")
	}
	var res []typeRE
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("types.json: %w", err)
		}
		name, _ := tok.(string)
		var globs []any
		if err := dec.Decode(&globs); err != nil {
			continue
		}
		var parts []string
		for _, g := range globs {
			if s, ok := g.(string); ok {
				parts = append(parts, "("+strings.ReplaceAll(regexp.QuoteMeta(s), `\*`, ".*")+")")
			}
		}
		if name == "" || len(parts) == 0 {
			continue
		}
		res = append(res, typeRE{name, regexp.MustCompile(`(?is)^(` + strings.Join(parts, "|") + `)$`)})
	}
	var check any
	if err := json.Unmarshal(stripped, &check); err != nil {
		return nil, nil, fmt.Errorf("types.json: %w", err)
	}
	return json.RawMessage(stripped), res, nil
}

func loadL10n(src layered) (map[string]map[string]any, map[string]string, error) {
	names, err := src.list("l10n")
	if err != nil {
		return nil, nil, err
	}
	all := map[string]map[string]any{}
	langs := map[string]string{}
	for _, name := range names {
		code, ok := strings.CutSuffix(name, ".json")
		if !ok || !isoCodeRe.MatchString(code) {
			continue
		}
		raw, err := src.read("l10n/" + name)
		if err != nil {
			return nil, nil, err
		}
		var t map[string]any
		if err := decode(raw, &t); err != nil {
			return nil, nil, fmt.Errorf("l10n/%s: %w", name, err)
		}
		t["isoCode"] = code
		all[code] = t
		lang, _ := t["lang"].(string)
		langs[code] = lang
	}
	return all, langs, nil
}

// layered reads a file from the config directory if present, otherwise
// from the embedded defaults.
type layered struct {
	dir      string
	defaults fs.FS
}

func (l layered) read(name string) ([]byte, error) {
	if l.dir != "" {
		b, err := os.ReadFile(filepath.Join(l.dir, filepath.FromSlash(name)))
		if err == nil {
			return b, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return fs.ReadFile(l.defaults, name)
}

// readOverride reads a file of the config directory only.
func (l layered) readOverride(name string) ([]byte, bool, error) {
	if l.dir == "" {
		return nil, false, nil
	}
	b, err := os.ReadFile(filepath.Join(l.dir, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// merge deep merges src into dst: objects are merged key by key, any
// other value (arrays included) replaces the default.
func merge(dst, src map[string]any) {
	for k, v := range src {
		sm, srcIsMap := v.(map[string]any)
		dm, dstIsMap := dst[k].(map[string]any)
		if srcIsMap && dstIsMap {
			merge(dm, sm)
			continue
		}
		dst[k] = v
	}
}

func (l layered) list(dir string) ([]string, error) {
	seen := map[string]bool{}
	entries, err := fs.ReadDir(l.defaults, dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		seen[e.Name()] = true
	}
	if l.dir != "" {
		if entries, err := os.ReadDir(filepath.Join(l.dir, dir)); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					seen[e.Name()] = true
				}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}
