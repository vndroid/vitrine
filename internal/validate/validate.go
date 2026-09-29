// Package validate checks a vitrine config folder: syntax, unknown
// options, value types and ranges, and references between the files
// (themes, languages, file types, patterns, the password hash).
package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vndroid/vitrine/internal/auth"
	"github.com/vndroid/vitrine/internal/jsonc"
	"github.com/vndroid/vitrine/internal/pattern"
)

// Level is the severity of an issue.
type Level int

const (
	// Warning: vitrine works, but probably not as intended.
	Warning Level = iota
	// Error: the setting is invalid and ignored, or vitrine can't start.
	Error
)

func (l Level) String() string {
	if l == Error {
		return "error"
	}
	return "warning"
}

// Issue is a problem found in a config file.
type Issue struct {
	File  string
	Line  int // 0 if unknown
	Col   int
	Level Level
	Path  string // option key path, may be empty
	Msg   string
}

func (i Issue) String() string {
	loc := i.File
	if i.Line > 0 {
		loc += fmt.Sprintf(":%d:%d", i.Line, i.Col)
	}
	s := loc + ": " + i.Level.String() + ": "
	if i.Path != "" {
		s += i.Path + ": "
	}
	return s + i.Msg
}

// Options of a validation.
type Options struct {
	Dir      string   // config folder, empty for the built-in defaults only
	Defaults fs.FS    // built-in config (options.json, types.json, l10n/)
	Themes   []string // names of the available icon themes
}

// Errors counts the issues of level Error.
func Errors(issues []Issue) int {
	n := 0
	for _, i := range issues {
		if i.Level == Error {
			n++
		}
	}
	return n
}

// Config validates a config folder.
func Config(o Options) []Issue {
	v := &validator{opts: o, typeNames: map[string]bool{}, langs: map[string]bool{}}
	if o.Dir == "" {
		return nil
	}
	// types and languages first: options.json refers to them
	v.types()
	v.l10n()
	v.options()
	sort.SliceStable(v.issues, func(a, b int) bool {
		x, y := v.issues[a], v.issues[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		return x.Col < y.Col
	})
	return v.issues
}

type validator struct {
	opts   Options
	issues []Issue

	file string // current file
	src  []byte // its content

	typeNames map[string]bool // file types of types.json
	langs     map[string]bool // available translations
}

func (v *validator) add(n *node, level Level, path, format string, args ...any) {
	v.addAt(offsetOf(n), level, path, format, args...)
}

func (v *validator) addAt(offset int64, level Level, path, format string, args ...any) {
	iss := Issue{File: v.file, Level: level, Path: path, Msg: fmt.Sprintf(format, args...)}
	if offset >= 0 && v.src != nil {
		iss.Line, iss.Col = jsonc.Position(v.src, offset)
	}
	v.issues = append(v.issues, iss)
}

func offsetOf(n *node) int64 {
	if n == nil {
		return -1
	}
	return n.offset
}

// load reads and parses a file of the config folder; ok is false if it is
// missing or could not be parsed (the error is recorded).
func (v *validator) load(name string) (*node, bool) {
	path := filepath.Join(v.opts.Dir, filepath.FromSlash(name))
	v.file, v.src = path, nil
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		v.addAt(-1, Error, "", "%v", err)
		return nil, false
	}
	v.src = src
	n, err := parse(jsonc.Strip(src))
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		v.addAt(int64(len(src)), Error, "", "invalid JSON: unexpected end of file")
		return nil, false
	}
	if err != nil {
		v.addAt(errorOffset(err, src), Error, "", "invalid JSON: %v", err)
		return nil, false
	}
	return n, true
}

// errorOffset returns the offset of a JSON error, pointing at the
// offending character.
func errorOffset(err error, src []byte) int64 {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	var off int64 = -1
	switch {
	case errors.As(err, &syntax):
		off = syntax.Offset
	case errors.As(err, &typ):
		off = typ.Offset
	}
	// the streaming decoder points at the offending character
	if off > int64(len(src)) {
		off = int64(len(src))
	}
	return off
}

// options checks options.json against the defaults and the rules.
func (v *validator) options() {
	n, ok := v.load("options.json")
	if !ok {
		return
	}
	if n.kind != kObject {
		v.add(n, Error, "", "options.json must contain an object")
		return
	}
	raw, err := fs.ReadFile(v.opts.Defaults, "options.json")
	if err != nil {
		return
	}
	defaults, err := parse(jsonc.Strip(raw))
	if err != nil {
		return
	}
	v.object(n, defaults, "")
	for path, rule := range rules {
		if child := n.get(path); child != nil {
			rule(v, child, path)
		}
	}
}

// freeForm options have arbitrary keys.
var freeForm = map[string]bool{"preview-txt.styles": true}

// object compares an option object with the default one: unknown keys and
// value types.
func (v *validator) object(n, def *node, path string) {
	for _, key := range n.keys {
		child := n.fields[key]
		p := join(path, key)
		switch {
		case path == "" && key == "passhash":
			v.passhash(child)
			continue
		case path == "" && key == "hasCustomPasshash":
			v.addAt(n.keyOff[key], Warning, p, "set by vitrine, the value is ignored")
			continue
		}
		d := def.fields[key]
		if d == nil {
			msg := fmt.Sprintf("unknown option %q", key)
			if s := suggest(key, def.keys); s != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", s)
			}
			v.addAt(n.keyOff[key], Warning, path, "%s, it is ignored", msg)
			continue
		}
		if !v.compatible(child, d, p) {
			continue
		}
		if child.kind == kObject && d.kind == kObject && !freeForm[p] {
			v.object(child, d, p)
		}
		if child.kind == kArray && d.kind == kArray && len(d.items) > 0 {
			for i, item := range child.items {
				if item.kind != d.items[0].kind {
					v.add(item, Error, fmt.Sprintf("%s[%d]", p, i), "expected %s, got %s", d.items[0].kind, item.kind)
				}
			}
		}
	}
}

// compatible checks the type of an option against the default value.
func (v *validator) compatible(n, def *node, path string) bool {
	if n.kind == def.kind || def.kind == kNull {
		return true
	}
	switch {
	case path == "preview-img.size" && (n.kind == kNumber || n.kind == kBool):
		return true // false or a size
	case def.kind == kBool && (n.kind == kNumber || n.kind == kString):
		v.add(n, Warning, path, "expected boolean, got %s (treated as %v)", n.kind, truthy(n))
		return false
	case def.kind == kNumber && n.kind == kString && isDigits(n.value.(string)):
		v.add(n, Warning, path, "expected number, got string %q", n.value)
		return false
	}
	v.add(n, Error, path, "expected %s, got %s", def.kind, n.kind)
	return false
}

func truthy(n *node) bool {
	switch t := n.value.(type) {
	case json.Number:
		f, _ := t.Float64()
		return f != 0
	case string:
		return t != "" && t != "0"
	}
	return false
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func (v *validator) passhash(n *node) {
	if n.kind != kString {
		v.add(n, Error, "passhash", "expected string, got %s: the login is disabled", n.kind)
		return
	}
	h := strings.TrimSpace(n.value.(string))
	switch {
	case h == "":
	case auth.LoginEnabled(h):
	case auth.IsEmptyPasswordHash(h):
		v.add(n, Warning, "passhash", "hash of the empty password: the login is disabled")
	default:
		v.add(n, Error, "passhash", "not a bcrypt, argon2 or SHA512 hash (create one with \"vitrine passwd\"): the login is disabled")
	}
}

// types checks types.json of the config folder.
func (v *validator) types() {
	n, ok := v.load("types.json")
	if !ok {
		// missing or broken: vitrine uses (or keeps using) the defaults
		if raw, err := fs.ReadFile(v.opts.Defaults, "types.json"); err == nil {
			if d, err := parse(jsonc.Strip(raw)); err == nil {
				for _, key := range d.keys {
					v.typeNames[key] = true
				}
			}
		}
		return
	}
	for _, key := range n.keys {
		v.typeNames[key] = true
	}
	if n.kind != kObject {
		v.add(n, Error, "", "types.json must contain an object")
		return
	}
	for _, key := range n.keys {
		globs := n.fields[key]
		if globs.kind != kArray {
			v.add(globs, Error, key, "expected an array of patterns, got %s", globs.kind)
			continue
		}
		for i, g := range globs.items {
			if g.kind != kString || g.value.(string) == "" {
				v.add(g, Error, fmt.Sprintf("%s[%d]", key, i), "expected a non-empty string pattern")
			}
		}
	}
}

var isoCodeRe = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2})?$`)

// l10n checks the translations of the config folder.
func (v *validator) l10n() {
	if defaults, err := fs.ReadDir(v.opts.Defaults, "l10n"); err == nil {
		for _, e := range defaults {
			if code, ok := strings.CutSuffix(e.Name(), ".json"); ok {
				v.langs[code] = true
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(v.opts.Dir, "l10n"))
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		code, isJSON := strings.CutSuffix(name, ".json")
		if e.IsDir() || !isJSON {
			continue
		}
		if !isoCodeRe.MatchString(code) {
			v.file, v.src = filepath.Join(v.opts.Dir, "l10n", name), nil
			v.addAt(-1, Warning, "", "file name is no language code like \"de\" or \"zh-cn\", the file is ignored")
			continue
		}
		n, ok := v.load("l10n/" + name)
		if !ok {
			continue
		}
		if n.kind != kObject {
			v.add(n, Error, "", "translations must be an object")
			continue
		}
		if lang := n.fields["lang"]; lang == nil || lang.kind != kString {
			v.add(n, Warning, "lang", "missing the language name (\"lang\")")
		}
		v.langs[code] = true
	}
}

// helpers for the rules

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// intValue returns the integer of a number node.
func intValue(n *node) (int64, bool) {
	num, ok := n.value.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := num.Int64()
	return i, err == nil
}

// suggest returns the most similar key, if similar enough.
func suggest(key string, keys []string) string {
	best, bestDist := "", 3
	for _, k := range keys {
		if d := distance(strings.ToLower(key), strings.ToLower(k)); d < bestDist {
			best, bestDist = k, d
		}
	}
	return best
}

// distance is the Levenshtein distance.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// compilePattern reports whether a hidden pattern works in vitrine.
func compilePattern(p string) error {
	_, err := pattern.Compile(p, false)
	return err
}
