package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) entries(t *testing.T) []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

func TestAccessLog(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{trusted: "127.0.0.1"})
	logs := &logBuffer{}
	ts := httptest.NewServer(s.AccessLog(s, slog.New(slog.NewJSONHandler(logs, nil))))
	defer ts.Close()

	get := func(path string, header map[string]string) {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	get("/a.txt", map[string]string{"X-Real-IP": "203.0.113.7", "User-Agent": "test-agent"})
	get("/missing", nil)
	get("/a.txt", map[string]string{"Range": "bytes=0-4"})

	e := logs.entries(t)
	if len(e) != 3 {
		t.Fatalf("entries = %d", len(e))
	}
	want := []struct {
		client string
		status float64
		bytes  float64
	}{
		{"203.0.113.7", 200, 11},
		{"127.0.0.1", 404, -1},
		{"127.0.0.1", 206, 5},
	}
	for i, w := range want {
		if e[i]["msg"] != "request" || e[i]["client"] != w.client || e[i]["status"] != w.status {
			t.Errorf("entry %d = %v", i, e[i])
		}
		if w.bytes >= 0 && e[i]["bytes"] != w.bytes {
			t.Errorf("entry %d bytes = %v, want %v", i, e[i]["bytes"], w.bytes)
		}
	}
	if e[0]["ua"] != "test-agent" || e[0]["path"] != "/a.txt" || e[0]["method"] != "GET" {
		t.Errorf("entry 0 = %v", e[0])
	}
}

func TestAccessLogAbortedRequest(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	logs := &logBuffer{}
	aborting := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("partial"))
		panic(http.ErrAbortHandler)
	})
	ts := httptest.NewServer(s.AccessLog(aborting, slog.New(slog.NewJSONHandler(logs, nil))))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/x")
	if err == nil {
		_, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if err == nil {
		t.Error("the client must see the aborted response")
	}
	e := logs.entries(t)
	if len(e) != 1 || e[0]["aborted"] != true || e[0]["bytes"] != float64(7) {
		t.Errorf("entries = %v", e)
	}
}
