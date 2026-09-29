package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(url string) *Client {
	c := New(url, "gzr_sa_0123456789abcdef_secret", "ws_default", "test")
	c.RetryBase = time.Millisecond
	c.RetryMax = 5 * time.Millisecond
	c.PollInterval = time.Millisecond
	c.PollTimeout = 2 * time.Second
	return c
}

func TestHeadersAndWorkspaceOverride(t *testing.T) {
	var gotAuth, gotWS, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get(WorkspaceHeader)
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := testClient(srv.URL)

	var out map[string]any
	if err := c.Get(context.Background(), "/api/x", "", &out); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer gzr_sa_0123456789abcdef_secret" || gotWS != "ws_default" {
		t.Fatalf("headers: %q %q", gotAuth, gotWS)
	}
	if err := c.Post(context.Background(), "/api/x", "analytics", map[string]any{"a": 1}, nil); err != nil {
		t.Fatal(err)
	}
	if gotWS != "analytics" || gotCT != "application/json" {
		t.Fatalf("override: %q %q", gotWS, gotCT)
	}
}

func TestRetriesRateLimitForWrites(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := testClient(srv.URL).Post(context.Background(), "/api/x", "", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestNoRetryOn500ForWrites(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := testClient(srv.URL)
	if err := c.Post(context.Background(), "/api/x", "", map[string]any{}, nil); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("POST should not retry a 500, got %d calls", calls)
	}
	atomic.StoreInt32(&calls, 0)
	_ = c.Get(context.Background(), "/api/x", "", nil)
	if calls != int32(c.MaxRetries+1) {
		t.Fatalf("GET should retry a 500, got %d calls", calls)
	}
}

func TestErrorDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"role not found"}`))
		case "/validation":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"detail":[{"loc":["body","name"],"msg":"field required"}]}`))
		}
	}))
	defer srv.Close()
	c := testClient(srv.URL)
	err := c.Get(context.Background(), "/text", "", nil)
	if !IsNotFound(err) || !strings.Contains(err.Error(), "role not found") {
		t.Fatalf("got %v", err)
	}
	err = c.Post(context.Background(), "/validation", "", map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "name: field required") {
		t.Fatalf("got %v", err)
	}
}

func TestRunCommandPollsUntilReady(t *testing.T) {
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"request_id":"9b2f","status":"pending"}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/9b2f") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if atomic.AddInt32(&polls, 1) < 3 {
			_, _ = w.Write([]byte(`{"request_id":"9b2f","status":"pending"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "9b2f", "status": "ready", "result": map[string]any{"exists": true}})
	}))
	defer srv.Close()
	res, err := testClient(srv.URL).RunCommand(context.Background(), http.MethodPost, "/api/c/start", "", map[string]any{},
		func(id string) string { return "/api/c/poll/" + id })
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Result) != `{"exists":true}` || polls != 3 {
		t.Fatalf("got %s after %d polls", res.Result, polls)
	}
}

func TestWaitCommandErrors(t *testing.T) {
	status := "error"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "error": "topic already exists"})
	}))
	defer srv.Close()
	c := testClient(srv.URL)
	if _, err := c.WaitCommand(context.Background(), "/p", ""); err == nil || !strings.Contains(err.Error(), "topic already exists") {
		t.Fatalf("got %v", err)
	}
	status = "expired"
	if _, err := c.WaitCommand(context.Background(), "/p", ""); err == nil || !strings.Contains(err.Error(), "operator online") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitCommandExpiredThenReady(t *testing.T) {
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&polls, 1) == 1 {
			_, _ = w.Write([]byte(`{"status":"expired","error":"schema registry action expired or unknown"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready","result":{"ok":true}}`))
	}))
	defer srv.Close()
	res, err := testClient(srv.URL).WaitCommand(context.Background(), "/p", "")
	if err != nil || string(res.Result) != `{"ok":true}` || polls != 2 {
		t.Fatalf("got %v after %d polls", err, polls)
	}
}

func TestWaitCommandTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer srv.Close()
	c := testClient(srv.URL)
	c.PollTimeout = 30 * time.Millisecond
	if _, err := c.WaitCommand(context.Background(), "/p", ""); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("got %v", err)
	}
}
