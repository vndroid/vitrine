package server

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHealthy(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	for _, method := range []string{"GET", "HEAD"} {
		rec := do(s, method, "/-/healthy", "", nil)
		if rec.Code != 200 {
			t.Errorf("%s /-/healthy = %d", method, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("Content-Type = %q", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q", cc)
		}
	}
	if body := do(s, "GET", "/-/healthy", "", nil).Body.String(); body != "vitrine is healthy.\n" {
		t.Errorf("body = %q", body)
	}
	if rec := do(s, "HEAD", "/-/healthy", "", nil); rec.Body.Len() != 0 {
		t.Errorf("HEAD sent a body of %d bytes", rec.Body.Len())
	}
	// no details: no version, path or error
	for _, target := range []string{"/-/healthy", "/-/ready"} {
		body := do(s, "GET", target, "", nil).Body.String()
		if strings.ContainsAny(body, "/0123456789") {
			t.Errorf("%s leaks details: %q", target, body)
		}
	}
	for _, target := range []string{"/-/healthy/", "/-/health", "/-/ready/x", "/-/Healthy"} {
		if rec := do(s, "GET", target, "", nil); rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

func TestReadyFollowsTheSharedFolder(t *testing.T) {
	s, root := newTestServer(t, fixtureOpts{})
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 200 || rec.Body.String() != "vitrine is ready.\n" {
		t.Fatalf("ready = %d %q", rec.Code, rec.Body.String())
	}
	if err := os.Rename(root, root+".gone"); err != nil {
		t.Fatal(err)
	}
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 503 || rec.Body.String() != "vitrine is not ready.\n" {
		t.Errorf("without the folder: %d %q", rec.Code, rec.Body.String())
	}
	// healthy does not depend on the folder: no restart because of storage
	if rec := do(s, "GET", "/-/healthy", "", nil); rec.Code != 200 {
		t.Errorf("healthy without the folder = %d", rec.Code)
	}
	if err := os.Rename(root+".gone", root); err != nil {
		t.Fatal(err)
	}
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 200 {
		t.Errorf("ready after the folder is back = %d", rec.Code)
	}
}

// A check that blocks (a lost network mount) is answered with 503 after the
// timeout, and no second check starts while it runs.
func TestReadyWithBlockedFolder(t *testing.T) {
	old := readyTimeout
	readyTimeout = 50 * time.Millisecond
	defer func() { readyTimeout = old }()

	s, _ := newTestServer(t, fixtureOpts{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	s.rootCheck = func() error {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release
		return nil
	}
	start := time.Now()
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 503 {
		t.Errorf("blocked check = %d", rec.Code)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the probe was not bounded by the timeout")
	}
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 503 {
		t.Errorf("second probe = %d", rec.Code)
	}
	mu.Lock()
	if calls != 1 {
		t.Errorf("%d checks started, want 1", calls)
	}
	mu.Unlock()

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && do(s, "GET", "/-/ready", "", nil).Code != 200 {
		time.Sleep(10 * time.Millisecond)
	}
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 200 {
		t.Errorf("after the folder answers again: %d", rec.Code)
	}

	s.rootCheck = func() error { return errors.New("input/output error") }
	if rec := do(s, "GET", "/-/ready", "", nil); rec.Code != 503 {
		t.Errorf("failing check = %d", rec.Code)
	}
}

func TestHealthBelowBasePath(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{base: "/my files/"})
	const base = "/my%20files"
	for target, code := range map[string]int{
		base + "/-/healthy": 200,
		base + "/-/ready":   200,
		"/-/healthy":        404,
		"/-/ready":          404,
	} {
		if rec := do(s, "GET", target, "", nil); rec.Code != code {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, code)
		}
	}
}

func TestProbesAreNotLogged(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	logs := &logBuffer{}
	ts := httptest.NewServer(s.AccessLog(s, slog.New(slog.NewJSONHandler(logs, nil))))
	defer ts.Close()
	for _, path := range []string{"/-/healthy", "/-/ready", "/a.txt", "/-/nope"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	var paths []string
	for _, e := range logs.entries(t) {
		paths = append(paths, e["path"].(string))
	}
	if got := strings.Join(paths, " "); got != "/a.txt /-/nope" {
		t.Errorf("logged %q, want only /a.txt and /-/nope", got)
	}
}
