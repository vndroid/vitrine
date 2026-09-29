package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const maxAPIBody = 1 << 20

// Error codes of the API.
const (
	errMissingParam = "ERR_MISSING_PARAM"
	errIllegalParam = "ERR_ILLEGAL_PARAM"
	errFailed       = "ERR_FAILED"
	errDisabled     = "ERR_DISABLED"
	errUnsupported  = "ERR_UNSUPPORTED"
	errLocked       = "ERR_LOCKED"
	errBusy         = "ERR_BUSY"
)

// apiError is sent as {"err": ..., "msg": ...} with status 200 like h5fs,
// or with Status if set.
type apiError struct {
	Err    string `json:"err"`
	Msg    string `json:"msg"`
	Status int    `json:"-"`
}

func (e *apiError) Error() string { return e.Err + ": " + e.Msg }

func fail(code, msg string) *apiError { return &apiError{Err: code, Msg: msg} }

// params are the request parameters: the JSON body if it is a JSON
// object, otherwise the form (for downloads), like h5fs.
type params map[string]any

func parseParams(r *http.Request) (params, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAPIBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxAPIBody {
		return nil, fail(errIllegalParam, "request too large")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err == nil && obj != nil {
		return obj, nil
	}

	values := url.Values{}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "application/x-www-form-urlencoded" {
		if values, err = url.ParseQuery(string(body)); err != nil {
			return nil, fail(errIllegalParam, "invalid form")
		}
	}
	for k, v := range r.URL.Query() {
		if _, ok := values[k]; !ok {
			values[k] = v
		}
	}
	return formParams(values), nil
}

// formParams turns form keys like "hrefs[0]" into nested values. The
// client sends "hrefs=" followed by "hrefs[0]=...", so like in PHP the
// bracketed keys win over a plain key of the same name.
func formParams(values url.Values) params {
	p := params{}
	for key, vals := range values {
		if len(vals) > 0 && !strings.Contains(key, "[") {
			p[key] = vals[len(vals)-1]
		}
	}
	for key, vals := range values {
		name, rest, nested := strings.Cut(key, "[")
		if len(vals) == 0 || !nested {
			continue
		}
		val := vals[len(vals)-1]
		sub, _, ok := strings.Cut(rest, "]")
		if !ok {
			p[key] = val
			continue
		}
		m, _ := p[name].(map[string]any)
		if m == nil {
			m = map[string]any{}
			p[name] = m
		}
		m[sub] = val
	}
	return p
}

func (p params) get(keypath string) (any, bool) {
	var v any = map[string]any(p)
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

func (p params) required(keypath string) (any, error) {
	v, ok := p.get(keypath)
	if !ok {
		return nil, fail(errMissingParam, fmt.Sprintf("parameter '%s' is missing", keypath))
	}
	return v, nil
}

// has reports whether a parameter is present and truthy (PHP semantics).
func (p params) has(keypath string) bool {
	v, ok := p.get(keypath)
	if !ok {
		return false
	}
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "0"
	case json.Number:
		f, err := t.Float64()
		return err == nil && f != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}

// boolean follows PHP's FILTER_VALIDATE_BOOLEAN, missing is false.
func (p params) boolean(keypath string) bool {
	v, ok := p.get(keypath)
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case json.Number:
		return t.String() == "1"
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "on", "yes":
			return true
		}
	}
	return false
}

func (p params) str(keypath string) (string, error) {
	v, err := p.required(keypath)
	if err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case json.Number:
		return t.String(), nil
	}
	return "", fail(errIllegalParam, fmt.Sprintf("parameter '%s' is no string", keypath))
}

func (p params) number(keypath string) (int, error) {
	v, err := p.required(keypath)
	if err != nil {
		return 0, err
	}
	s := ""
	switch t := v.(type) {
	case json.Number:
		s = t.String()
	case string:
		s = strings.TrimSpace(t)
	}
	f, perr := strconv.ParseFloat(s, 64)
	if s == "" || perr != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fail(errIllegalParam, fmt.Sprintf("parameter '%s' is not numeric", keypath))
	}
	return int(f), nil
}

func (p params) array(keypath string) ([]any, error) {
	v, err := p.required(keypath)
	if err != nil {
		return nil, err
	}
	return toArray(v, keypath)
}

// toArray accepts JSON arrays and objects (PHP arrays), the latter in
// key order.
func toArray(v any, keypath string) ([]any, error) {
	switch t := v.(type) {
	case []any:
		return t, nil
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, errA := strconv.Atoi(keys[i])
			b, errB := strconv.Atoi(keys[j])
			if errA == nil && errB == nil {
				return a < b
			}
			return keys[i] < keys[j]
		})
		arr := make([]any, 0, len(keys))
		for _, k := range keys {
			arr = append(arr, t[k])
		}
		return arr, nil
	}
	return nil, fail(errIllegalParam, fmt.Sprintf("parameter '%s' is no array", keypath))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// handleAPI dispatches POST requests.
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	p, err := parseParams(r)
	if err != nil {
		s.apiFail(w, err)
		return
	}
	action, err := p.str("action")
	if err != nil {
		s.apiFail(w, err)
		return
	}
	switch action {
	case "get":
		res, err := s.onGet(r, p)
		if err != nil {
			s.apiFail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	case "login":
		s.onLogin(w, r, p)
	case "logout":
		s.onLogout(w, r)
	case "download":
		s.onDownload(w, r, p)
	default:
		s.apiFail(w, fail(errUnsupported, "unsupported action"))
	}
}

func (s *Server) apiFail(w http.ResponseWriter, err error) {
	if ae, ok := err.(*apiError); ok {
		status := http.StatusOK
		if ae.Status != 0 {
			status = ae.Status
		}
		writeJSON(w, status, ae)
		return
	}
	s.log.Error("api", "err", err)
	writeJSON(w, http.StatusInternalServerError, fail(errFailed, "internal error"))
}

func (s *Server) onGet(r *http.Request, p params) (map[string]any, error) {
	res := map[string]any{}
	admin := s.isAdmin(r)

	if p.boolean("langs") {
		res["langs"] = s.cfg.Langs()
	}
	if p.boolean("options") {
		res["options"] = s.cfg.Options()
	}
	if p.boolean("types") {
		res["types"] = s.cfg.Types()
	}
	if p.boolean("setup") {
		if admin && p.boolean("refresh") {
			s.detectCommands()
		}
		res["setup"] = s.setupInfo(admin)
	}
	if p.boolean("theme") {
		res["theme"] = s.themeIcons()
	}

	if p.has("items") {
		href, err := p.str("items.href")
		if err != nil {
			return nil, err
		}
		what, err := p.number("items.what")
		if err != nil {
			return nil, err
		}
		res["items"] = s.tree.Items(href, what)
	}

	if p.has("custom") {
		if !s.cfg.Bool("custom.enabled", false) {
			return nil, fail(errDisabled, "custom disabled")
		}
		href, err := p.str("custom")
		if err != nil {
			return nil, err
		}
		res["custom"] = s.tree.Custom(href)
	}

	if p.has("l10n") {
		if !s.cfg.Bool("l10n.enabled", false) {
			return nil, fail(errDisabled, "l10n disabled")
		}
		codes, err := p.array("l10n")
		if err != nil {
			return nil, err
		}
		l10n := map[string]any{}
		for _, c := range codes {
			if code, ok := c.(string); ok {
				if t, ok := s.cfg.L10n(code); ok {
					l10n[code] = t
				}
			}
		}
		res["l10n"] = l10n
	}

	if p.has("search") {
		if !s.cfg.Bool("search.enabled", false) {
			return nil, fail(errDisabled, "search disabled")
		}
		href, err := p.str("search.href")
		if err != nil {
			return nil, err
		}
		expr, err := p.str("search.pattern")
		if err != nil {
			return nil, err
		}
		res["search"] = s.tree.Search(href, expr, p.boolean("search.ignorecase"))
	}

	if p.has("thumbs") {
		if !s.cfg.Bool("thumbnails.enabled", false) {
			return nil, fail(errDisabled, "thumbnails disabled")
		}
		reqs, err := p.array("thumbs")
		if err != nil {
			return nil, err
		}
		thumbs, err := s.thumbs(r, reqs)
		if err != nil {
			return nil, err
		}
		res["thumbs"] = thumbs
	}
	return res, nil
}
