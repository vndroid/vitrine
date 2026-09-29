package server

import (
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const homepage = "https://github.com/vndroid/vitrine"

// The markup matches the h5fs page template: the bundled frontend relies on
// its ids and classes.
var pageTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html class="no-js" lang="en"><head><meta charset="utf-8"><meta http-equiv="x-ua-compatible" content="ie=edge"><title>{{.Title}}</title><meta name="description" content="{{.Title}}"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="shortcut icon" href="{{.PublicHref}}images/favicon/favicon-16-32.ico"><link rel="apple-touch-icon-precomposed" type="image/png" href="{{.PublicHref}}images/favicon/favicon-152.png"><link rel="stylesheet" href="{{.PublicHref}}css/styles.css">{{if not .Fallback}}<script src="{{.PublicHref}}js/scripts.js" data-module="{{.Module}}"></script>{{end}}
{{.HeadTags}}</head><body class="{{.Module}}" id="root"><div id="fallback-hints">{{if not .Fallback}}<span class="noJsMsg">Works best with JavaScript enabled!</span><span class="noBrowserMsg">Works best in <a href="http://browsehappy.com">modern browsers</a>!</span>{{end}}<span class="backlink"><a href="{{.Homepage}}" title="vitrine {{.Version}}">powered by vitrine</a></span></div>
{{- if eq .Module "info"}}<div id="content"><h1 id="header"><a href="{{.Homepage}}">vitrine</a></h1></div>
{{- else}}<div id="fallback">{{.FallbackHTML}}</div>{{end}}</body></html>
`))

var fallbackTmpl = template.Must(template.New("fallback").Parse(
	`<table><tr><th class="fb-i"></th><th class="fb-n"><span>Name</span></th><th class="fb-d"><span>Last modified</span></th><th class="fb-s"><span>Size</span></th></tr>
{{- if .HasParent}}<tr><td class="fb-i"><img src="{{.Images}}folder-parent.png" alt="folder-parent"/></td><td class="fb-n"><a href="..">Parent Directory</a></td><td class="fb-d"></td><td class="fb-s"></td></tr>{{end}}
{{- range .Rows}}<tr><td class="fb-i"><img src="{{$.Images}}{{.Type}}.png" alt="{{.Type}}"/></td><td class="fb-n"><a href="{{.Href}}">{{.Name}}</a></td><td class="fb-d">{{.Date}}</td><td class="fb-s">{{.Size}}</td></tr>{{end}}</table>`))

type pageData struct {
	Title        string
	Module       string
	PublicHref   string
	Version      string
	Homepage     string
	Fallback     bool
	HeadTags     template.HTML
	FallbackHTML template.HTML
}

type fallbackRow struct {
	Type, Href, Name, Date, Size string
}

var textBrowserRe = regexp.MustCompile(`(?i)curl|links|lynx|w3m`)

func (s *Server) isFallbackMode(r *http.Request) bool {
	return s.cfg.Bool("view.fallbackMode", false) || textBrowserRe.MatchString(r.UserAgent())
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, module, dir string) {
	data := pageData{
		Module:     module,
		PublicHref: s.publicHref(),
		Version:    s.version,
		Homepage:   homepage,
		HeadTags:   s.headTags(),
	}
	if module == "info" {
		data.Title = "vitrine info page - " + s.version
	} else {
		data.Title = "index - powered by vitrine " + s.version
		data.Fallback = s.isFallbackMode(r)
		data.FallbackHTML = s.fallbackHTML(dir)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	if err := pageTmpl.Execute(w, data); err != nil {
		s.log.Error("render page", "err", err)
	}
}

// fallbackHTML renders the plain listing shown without JavaScript and to
// text browsers.
func (s *Server) fallbackHTML(dir string) template.HTML {
	_, items, hasParent := s.tree.Listing(dir)
	rows := make([]fallbackRow, 0, len(items))
	for _, it := range items {
		row := fallbackRow{Type: "file", Href: it.Href, Name: it.Name()}
		if it.IsFolder {
			row.Type = "folder"
		}
		if !it.ModTime.IsZero() {
			row.Date = it.ModTime.Format("2006-01-02 15:04")
		}
		if it.Size != nil {
			row.Size = strconv.FormatInt(*it.Size/1000, 10) + " KB"
		}
		rows = append(rows, row)
	}
	var b strings.Builder
	err := fallbackTmpl.Execute(&b, struct {
		Images    string
		HasParent bool
		Rows      []fallbackRow
	}{s.publicHref() + "images/fallback/", hasParent, rows})
	if err != nil {
		s.log.Error("render fallback", "err", err)
	}
	return template.HTML(b.String())
}

// headTags renders "resources.styles", "resources.scripts" and the font
// options. Relative resources point into the "ext" folder.
func (s *Server) headTags() template.HTML {
	var b strings.Builder
	for _, href := range s.cfg.Strings("resources.styles") {
		b.WriteString(`<link rel="stylesheet" href="` + html.EscapeString(s.extHref(href)) + `" class="x-head">`)
	}
	for _, href := range s.cfg.Strings("resources.scripts") {
		b.WriteString(`<script src="` + html.EscapeString(s.extHref(href)) + `" class="x-head"></script>`)
	}
	b.WriteString(`<style class="x-head">`)
	if fonts := s.cfg.Strings("view.fonts"); len(fonts) > 0 {
		b.WriteString(`#root,input,select{font-family:` + fontList(fonts) + `!important}`)
	}
	if fonts := s.cfg.Strings("view.fontsMono"); len(fonts) > 0 {
		b.WriteString(`pre,code{font-family:` + fontList(fonts) + `!important}`)
	}
	b.WriteString(`</style>`)
	return template.HTML(b.String())
}

var absHrefRe = regexp.MustCompile(`(?i)^(https?://|/)`)

func (s *Server) extHref(href string) string {
	if absHrefRe.MatchString(href) {
		return href
	}
	return s.publicHref() + "ext/" + href
}

// fontList quotes font names for CSS, dropping characters that could end
// the string or the style element.
func fontList(fonts []string) string {
	quoted := make([]string, 0, len(fonts))
	for _, f := range fonts {
		f = strings.Map(func(r rune) rune {
			if r == '"' || r == '\\' || r == '<' || r == '>' || r < ' ' {
				return -1
			}
			return r
		}, f)
		quoted = append(quoted, `"`+f+`"`)
	}
	return strings.Join(quoted, ",")
}
