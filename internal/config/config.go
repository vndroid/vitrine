// Package config loads the h5fs-compatible configuration: options.json,
// types.json and the l10n translations. Files in the config directory
// override the embedded defaults file by file.
package config

import (
	"bytes"
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

	"github.com/vndroid/vitrine/internal/jsonc"
	"github.com/vndroid/vitrine/internal/pattern"
)

var isoCodeRe = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2})?$`)

// Config is the loaded configuration. It is read-only after Load.
type Config struct {
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
	src := layered{dir: dir, defaults: defaults}
	c := &Config{}

	raw, err := src.read("options.json")
	if err != nil {
		return nil, err
	}
	if err := decode(raw, &c.options); err != nil {
		return nil, fmt.Errorf("options.json: %w", err)
	}
	if c.options == nil {
		c.options = map[string]any{}
	}
	if s, ok := c.options["passhash"].(string); ok {
		c.passhash = strings.TrimSpace(s)
	}
	delete(c.options, "passhash")

	if c.types, c.typeREs, err = loadTypes(src); err != nil {
		return nil, err
	}

	for _, p := range c.Strings("view.hidden") {
		re, err := pattern.Compile(p, false)
		if err != nil {
			slog.Warn("ignoring unsupported view.hidden pattern", "pattern", p, "err", err)
			continue
		}
		c.hidden = append(c.hidden, re)
	}

	if c.l10n, c.langs, err = loadL10n(src); err != nil {
		return nil, err
	}
	return c, nil
}

// SetLoginEnabled records whether the admin login is available; the
// client reads it as "hasCustomPasshash" (name kept for compatibility).
func (c *Config) SetLoginEnabled(enabled bool) {
	c.options["hasCustomPasshash"] = enabled
}

// Passhash returns the configured admin password hash (may be empty).
func (c *Config) Passhash() string { return c.passhash }

// Options returns the options as sent to the client (without passhash).
func (c *Config) Options() map[string]any { return c.options }

// Types returns types.json as sent to the client, in its original order.
func (c *Config) Types() json.RawMessage { return c.types }

// Langs maps the available iso codes to the language names.
func (c *Config) Langs() map[string]string { return c.langs }

// L10n returns the translations of an iso code.
func (c *Config) L10n(isoCode string) (map[string]any, bool) {
	t, ok := c.l10n[isoCode]
	return t, ok
}

// Get returns the option at a dot separated key path.
func (c *Config) Get(keypath string) (any, bool) {
	var v any = c.options
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
	v, _ := c.Get(keypath)
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
	for _, re := range c.hidden {
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
	for _, t := range c.typeREs {
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
