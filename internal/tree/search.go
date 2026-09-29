package tree

import (
	"os"
	"path/filepath"
	"time"

	"github.com/vndroid/vitrine/internal/pattern"
)

const (
	maxPatternLength  = 256
	maxSearchVisited  = 10000
	maxSearchDepth    = 32
	maxSearchDuration = 2 * time.Second
)

// Search returns the entries below the folder at href whose name matches
// the pattern. The search is bounded in visited entries, depth and time;
// RE2 matches in linear time, so patterns can't backtrack catastrophically.
func (t *Tree) Search(href, expr string, ignoreCase bool) []*Item {
	if expr == "" || len(expr) > maxPatternLength {
		return []*Item{}
	}
	path, err := t.ToPath(href)
	if err != nil {
		return []*Item{}
	}
	if !t.IsManagedPath(path) {
		return []*Item{}
	}
	re, err := pattern.Compile(expr, ignoreCase)
	if err != nil {
		return []*Item{}
	}

	s := &searchState{
		visited:  0,
		seen:     map[string]bool{},
		deadline: time.Now().Add(maxSearchDuration),
	}
	var paths []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if s.aborted || depth > maxSearchDepth || s.visited >= maxSearchVisited || time.Now().After(s.deadline) {
			s.aborted = true
			return
		}
		// walk the paths below the root (links stay links), but visit every
		// real folder once, so link cycles end
		real, ok := t.ResolveManagedPath(dir)
		if !ok || s.seen[real] {
			return
		}
		s.seen[real] = true
		for _, name := range t.ReadDir(dir) {
			s.visited++
			if s.visited > maxSearchVisited || time.Now().After(s.deadline) {
				s.aborted = true
				return
			}
			p := filepath.Join(dir, name)
			if re.MatchString(name) {
				paths = append(paths, p)
			}
			if fi, err := os.Stat(p); err == nil && fi.IsDir() && !t.isAliasLink(p) {
				walk(p, depth+1)
			}
			if s.aborted {
				return
			}
		}
	}
	walk(path, 0)
	return t.ItemsForPaths(paths)
}

type searchState struct {
	visited  int
	seen     map[string]bool
	deadline time.Time
	aborted  bool
}
