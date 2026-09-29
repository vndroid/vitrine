package validate

import (
	"fmt"
	"slices"
	"strings"
)

// rule checks one option of options.json (present in the file).
type rule func(v *validator, n *node, path string)

var rules = map[string]rule{
	"download.type":                   oneOf("tar", "zip", "php-tar", "shell-tar", "shell-zip"),
	"download.maxConcurrent":          intRange(1, 64),
	"download.maxConcurrentPerClient": intRange(1, 64),
	"download.maxDuration":            intRange(1, -1),
	"download.minRate":                intRange(0, -1),
	"download.minRateGrace":           intRange(0, -1),
	"foldersize.type":                 oneOf("sum", "php", "shell-du"),
	"autorefresh.interval":            intRangeLevel(1000, -1, Warning),
	"filter.debounceTime":             intRange(0, -1),
	"search.debounceTime":             intRange(0, -1),
	"sort.column":                     intRange(0, 2),
	"sort.folders":                    intRange(0, 2),
	"tree.maxSubfolders":              intRange(0, -1),
	"view.maxIconSize":                intRange(1, -1),
	"view.modes":                      nonEmptySubset("details", "grid", "icons"),
	"view.sizes":                      eachInt(1, 1000),
	"view.theme":                      theme,
	"view.hidden":                     hiddenPatterns,
	"view.unmanaged":                  fileNames,
	"l10n.lang":                       language,
	"thumbnails.size":                 intRange(1, 4096),
	"thumbnails.maxCacheSize":         intRange(1, -1),
	"thumbnails.delay":                intRange(0, -1),
	// the server answers at most 40 thumbnails per request
	"thumbnails.chunksize": intRange(1, 40),
	"thumbnails.img":       fileTypes,
	"thumbnails.mov":       fileTypesIn("vid-avi", "vid-flv", "vid-mkv", "vid-mov", "vid-mp4", "vid-mpg", "vid-ts", "vid-vob", "vid-webm", "vid-wmv"),
	"thumbnails.doc":       fileTypesIn("x-pdf", "x-ps", "x-eps"),
	"preview-img.size":     previewSize,
	"preview-img.types":    fileTypes,
	"preview-aud.types":    fileTypes,
	"preview-vid.types":    fileTypes,
	"preview-txt.styles":   textStyles,
}

func oneOf(values ...string) rule {
	return func(v *validator, n *node, path string) {
		s, ok := n.value.(string)
		if ok && slices.Contains(values, s) {
			return
		}
		v.add(n, Error, path, "%s is not one of %s", show(n), strings.Join(values, ", "))
	}
}

func intRangeLevel(lo, hi int64, level Level) rule {
	return func(v *validator, n *node, path string) {
		checkInt(v, n, path, lo, hi, level)
	}
}

// intRange checks an integer lo..hi, hi < 0 for no upper bound.
func intRange(lo, hi int64) rule { return intRangeLevel(lo, hi, Error) }

func checkInt(v *validator, n *node, path string, lo, hi int64, level Level) bool {
	if n.kind != kNumber {
		return false // reported by the type check
	}
	i, ok := intValue(n)
	switch {
	case !ok:
		v.add(n, Error, path, "expected a whole number, got %s", show(n))
	case i < lo:
		v.add(n, level, path, "%d is less than %d", i, lo)
	case hi >= 0 && i > hi:
		v.add(n, level, path, "%d is more than %d", i, hi)
	default:
		return true
	}
	return false
}

func eachInt(lo, hi int64) rule {
	return func(v *validator, n *node, path string) {
		if n.kind != kArray {
			return
		}
		if len(n.items) == 0 {
			v.add(n, Error, path, "must not be empty")
		}
		for i, item := range n.items {
			checkInt(v, item, fmt.Sprintf("%s[%d]", path, i), lo, hi, Error)
		}
	}
}

func nonEmptySubset(values ...string) rule {
	return func(v *validator, n *node, path string) {
		if n.kind != kArray {
			return
		}
		if len(n.items) == 0 {
			v.add(n, Error, path, "must not be empty")
		}
		for i, item := range n.items {
			if s, ok := item.value.(string); !ok || !slices.Contains(values, s) {
				v.add(item, Error, fmt.Sprintf("%s[%d]", path, i), "%s is not one of %s", show(item), strings.Join(values, ", "))
			}
		}
	}
}

func theme(v *validator, n *node, path string) {
	if s, ok := n.value.(string); ok && !slices.Contains(v.opts.Themes, s) {
		v.add(n, Error, path, "unknown theme %q, available: %s", s, strings.Join(v.opts.Themes, ", "))
	}
}

func language(v *validator, n *node, path string) {
	s, ok := n.value.(string)
	if !ok {
		return
	}
	if !v.langs[s] {
		v.add(n, Error, path, "no translations for %q (l10n/%s.json)", s, s)
	}
}

func hiddenPatterns(v *validator, n *node, path string) {
	for i, item := range n.items {
		if s, ok := item.value.(string); ok {
			if err := compilePattern(s); err != nil {
				v.add(item, Error, fmt.Sprintf("%s[%d]", path, i),
					"pattern %q is not supported (RE2 syntax: no lookarounds or backreferences), it is ignored: %v", s, err)
			}
		}
	}
}

func fileNames(v *validator, n *node, path string) {
	for i, item := range n.items {
		if s, ok := item.value.(string); ok && (s == "" || strings.ContainsAny(s, "/\\")) {
			v.add(item, Warning, fmt.Sprintf("%s[%d]", path, i), "%q is no plain file name, it never matches", s)
		}
	}
}

// fileTypes checks that the listed types are defined in types.json.
func fileTypes(v *validator, n *node, path string) {
	checkTypes(v, n, path, nil)
}

// fileTypesIn also checks that vitrine supports the types for the option.
func fileTypesIn(supported ...string) rule {
	return func(v *validator, n *node, path string) {
		checkTypes(v, n, path, supported)
	}
}

func checkTypes(v *validator, n *node, path string, supported []string) {
	for i, item := range n.items {
		s, ok := item.value.(string)
		if !ok {
			continue
		}
		p := fmt.Sprintf("%s[%d]", path, i)
		switch {
		case !v.typeDefined(s):
			v.add(item, Warning, p, "file type %q is not defined in types.json", s)
		case supported != nil && !slices.Contains(supported, s):
			v.add(item, Warning, p, "no thumbnails for file type %q, supported: %s", s, strings.Join(supported, ", "))
		}
	}
}

// typeDefined reports whether a file type is in the types.json vitrine
// uses (the config folder's or the default).
func (v *validator) typeDefined(name string) bool { return v.typeNames[name] }

func previewSize(v *validator, n *node, path string) {
	switch n.kind {
	case kBool:
		if n.value.(bool) {
			v.add(n, Error, path, "expected false or a size in pixels, got true")
		}
	case kNumber:
		checkInt(v, n, path, 1, 4096, Error)
	}
}

func textStyles(v *validator, n *node, path string) {
	for _, key := range n.keys {
		child := n.fields[key]
		p := path + "." + key
		if !v.typeDefined(key) {
			v.addAt(n.keyOff[key], Warning, p, "file type %q is not defined in types.json", key)
		}
		if child.kind != kNumber {
			v.add(child, Error, p, "expected a style number 0-3, got %s", child.kind)
			continue
		}
		checkInt(v, child, p, 0, 3, Error)
	}
}

// show formats a value for messages.
func show(n *node) string {
	switch n.kind {
	case kString:
		return fmt.Sprintf("%q", n.value)
	case kNumber, kBool:
		return fmt.Sprint(n.value)
	}
	return n.kind.String()
}
