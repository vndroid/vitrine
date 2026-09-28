package tree

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	folderSizeTTL     = time.Minute
	folderSizeTimeout = 10 * time.Second
	maxSizeCache      = 10000
)

// Item is a file or folder as sent to the client.
type Item struct {
	Path     string
	Href     string
	IsFolder bool
	ModTime  time.Time
	Size     *int64
	Fetched  bool
	Managed  bool
}

// MarshalJSON writes the h5fs item format.
func (it *Item) MarshalJSON() ([]byte, error) {
	obj := struct {
		Href    string `json:"href"`
		Time    int64  `json:"time"`
		Size    *int64 `json:"size"`
		Managed *bool  `json:"managed,omitempty"`
		Fetched *bool  `json:"fetched,omitempty"`
	}{Href: it.Href, Size: it.Size}
	if !it.ModTime.IsZero() {
		obj.Time = it.ModTime.Unix() * 1000
	}
	if it.IsFolder {
		obj.Managed = &it.Managed
		obj.Fetched = &it.Fetched
	}
	return json.Marshal(obj)
}

// Name returns the base name of the item.
func (it *Item) Name() string { return filepath.Base(it.Path) }

// itemCache collects the items of one request by path.
type itemCache map[string]*Item

func (t *Tree) item(path string, cache itemCache) *Item {
	if !within(path, t.root) {
		return nil
	}
	if it, ok := cache[path]; ok {
		return it
	}
	it := &Item{Path: path}
	if fi, err := os.Stat(path); err == nil {
		it.IsFolder = fi.IsDir()
		it.ModTime = fi.ModTime()
		if !it.IsFolder {
			size := fi.Size()
			it.Size = &size
		}
	}
	it.Href, _ = t.ToHref(path, it.IsFolder)
	if it.IsFolder {
		it.Managed = t.IsManagedPath(path)
		it.Size = t.folderSize(path)
	}
	cache[path] = it
	return it
}

func (t *Tree) parent(it *Item, cache itemCache) *Item {
	p := filepath.Dir(it.Path)
	if p == it.Path || !within(p, t.root) {
		return nil
	}
	return t.item(p, cache)
}

func (t *Tree) content(it *Item, cache itemCache) []*Item {
	if !it.IsFolder || !it.Managed {
		return nil
	}
	names := t.ReadDir(it.Path)
	items := make([]*Item, 0, len(names))
	for _, name := range names {
		if child := t.item(filepath.Join(it.Path, name), cache); child != nil {
			items = append(items, child)
		}
	}
	it.Fetched = true
	return items
}

// Items returns the items of a folder as requested by the client: with
// what >= 1 the content of the folder and of all its parents, with
// what >= 2 additionally the content of its subfolders.
func (t *Tree) Items(href string, what int) []*Item {
	path, err := t.ToPath(href)
	if err != nil || !t.IsManagedPath(path) {
		return []*Item{}
	}
	cache := itemCache{}
	folder := t.item(path, cache)

	if what >= 2 && folder != nil {
		for _, it := range t.content(folder, cache) {
			t.content(it, cache)
		}
		folder = t.parent(folder, cache)
	}
	for what >= 1 && folder != nil {
		t.content(folder, cache)
		folder = t.parent(folder, cache)
	}
	return sortedItems(cache)
}

// Listing returns the sorted visible content of a folder (fallback mode).
func (t *Tree) Listing(path string) (folder *Item, items []*Item, hasParent bool) {
	cache := itemCache{}
	folder = t.item(path, cache)
	if folder == nil {
		return nil, nil, false
	}
	items = t.content(folder, cache)
	sortItems(items)
	return folder, items, t.parent(folder, cache) != nil
}

// ItemsForPaths returns the items of the given paths, in order.
func (t *Tree) ItemsForPaths(paths []string) []*Item {
	cache := itemCache{}
	items := make([]*Item, 0, len(paths))
	for _, p := range paths {
		if it := t.item(p, cache); it != nil {
			items = append(items, it)
		}
	}
	return items
}

func sortedItems(cache itemCache) []*Item {
	items := make([]*Item, 0, len(cache))
	for _, it := range cache {
		items = append(items, it)
	}
	sortItems(items)
	return items
}

// sortItems orders folders first, then by path ignoring ASCII case, like
// the h5fs Item::cmp.
func sortItems(items []*Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.IsFolder != b.IsFolder {
			return a.IsFolder
		}
		return compareFold(a.Path, b.Path) < 0
	})
}

// compareFold compares like PHP's strcasecmp: bytewise, ASCII letters
// lowercased.
func compareFold(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := lower(a[i]), lower(b[i])
		if ca != cb {
			return int(ca) - int(cb)
		}
	}
	return len(a) - len(b)
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// folderSize returns the size of a folder if "foldersize" is enabled,
// cached for a short time since it walks the whole subtree.
func (t *Tree) folderSize(path string) *int64 {
	if !t.cfg.Bool("foldersize.enabled", false) {
		return nil
	}
	now := time.Now()
	t.sizeMu.Lock()
	if e, ok := t.sizeCache[path]; ok && now.Before(e.expires) {
		t.sizeMu.Unlock()
		return e.size
	}
	t.sizeMu.Unlock()

	var size *int64
	if t.cfg.String("foldersize.type", "") == "shell-du" {
		size = duSize(path)
	} else {
		size = walkSize(path)
	}

	t.sizeMu.Lock()
	if len(t.sizeCache) >= maxSizeCache {
		for k, e := range t.sizeCache {
			if now.After(e.expires) {
				delete(t.sizeCache, k)
			}
		}
		if len(t.sizeCache) >= maxSizeCache {
			t.sizeCache = map[string]sizeEntry{}
		}
	}
	t.sizeCache[path] = sizeEntry{size, now.Add(folderSizeTTL)}
	t.sizeMu.Unlock()
	return size
}

// walkSize sums up the sizes of all regular files below path. Symbolic
// links are not followed, so link cycles can't loop.
func walkSize(path string) *int64 {
	var total int64
	deadline := time.Now().Add(folderSizeTimeout)
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return &total
}

// duSize runs "du -sbL" (GNU coreutils) like h5fs.
func duSize(path string) *int64 {
	ctx, cancel := context.WithTimeout(context.Background(), folderSizeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "du", "-sbL", "--", path).Output()
	if err != nil {
		return nil
	}
	field, _, _ := bytes.Cut(out, []byte("\t"))
	n, err := strconv.ParseInt(strings.TrimSpace(string(field)), 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
