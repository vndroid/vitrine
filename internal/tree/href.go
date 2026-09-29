package tree

import (
	"errors"
	"net/url"
	"strings"
)

// ErrBadHref is returned for hrefs outside the root or with "." and ".."
// segments, NUL bytes or invalid escapes.
var ErrBadHref = errors.New("bad href")

// RawURLEncode encodes a path segment like PHP's rawurlencode (RFC 3986:
// everything but unreserved characters), which is what the client expects.
func RawURLEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// splitHref decodes the segments of an absolute href. Empty segments
// ("//") are dropped; ".", "..", NUL and "/" inside a segment are rejected.
func splitHref(href string) ([]string, error) {
	if !strings.HasPrefix(href, "/") {
		return nil, ErrBadHref
	}
	var segs []string
	for _, raw := range strings.Split(href, "/") {
		if raw == "" {
			continue
		}
		seg, err := url.PathUnescape(raw)
		if err != nil || seg == "." || seg == ".." || strings.ContainsAny(seg, "/\\\x00") {
			return nil, ErrBadHref
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

func joinHref(segs []string, trailingSlash bool) string {
	var b strings.Builder
	b.WriteByte('/')
	for i, s := range segs {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(RawURLEncode(s))
	}
	if trailingSlash && len(segs) > 0 {
		b.WriteByte('/')
	}
	return b.String()
}

// NormalizeBase turns a base path ("files/", "/my files") into the href
// prefix the tree is served below ("/files", "/my%20files"); "" and "/"
// mean the site root.
func NormalizeBase(base string) (string, error) {
	var segs []string
	for _, seg := range strings.Split(base, "/") {
		switch {
		case seg == "":
		case seg == "." || seg == ".." || strings.ContainsAny(seg, "\\\x00?#"):
			return "", ErrBadHref
		default:
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 {
		return "", nil
	}
	return joinHref(segs, false), nil
}
