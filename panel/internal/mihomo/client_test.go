package mihomo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func clientFor(t *testing.T, srv *httptest.Server, secret string) *Client {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	return NewClient(port, secret)
}

func TestClientJSONAndAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/version" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"version":"v1.2.3"}`))
	}))
	defer srv.Close()
	res, err := clientFor(t, srv, "s3cret").Request("GET", "/version", nil, 5000)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["version"] != "v1.2.3" {
		t.Fatalf("res = %#v", res)
	}
}

func TestClientBodySent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if m["mode"] != "global" {
			t.Errorf("body = %s", b)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	res, err := clientFor(t, srv, "x").Request("PATCH", "/configs", map[string]any{"mode": "global"}, 5000)
	if err != nil || res != nil {
		t.Fatalf("204 should yield nil,nil; got %#v %v", res, err)
	}
}

func TestClientErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantStatus int
	}{
		{"json message", 400, `{"message":"bad request"}`, "bad request", 400},
		{"plain text", 500, "oops", "oops", 500},
		{"empty body", 404, "", "内核接口返回 HTTP 404", 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, err := clientFor(t, srv, "x").Request("GET", "/", nil, 5000)
			ce, ok := err.(*ClashError)
			if !ok {
				t.Fatalf("want ClashError, got %v", err)
			}
			if ce.Message != c.wantMsg || ce.StatusCode != c.wantStatus {
				t.Fatalf("got %q/%d want %q/%d", ce.Message, ce.StatusCode, c.wantMsg, c.wantStatus)
			}
		})
	}
}

func TestClientEmptyAndTextOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			return
		}
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	c := clientFor(t, srv, "x")
	if res, err := c.Request("GET", "/empty", nil, 5000); err != nil || res != nil {
		t.Fatalf("empty 200 => nil,nil; got %#v %v", res, err)
	}
	if res, err := c.Request("GET", "/text", nil, 5000); err != nil || res != "hello" {
		t.Fatalf("text 200 => string; got %#v %v", res, err)
	}
}

func TestRequestIntoDecodesTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"downloadTotal":10,"uploadTotal":20,"connections":[{"id":"a"}]}`))
	}))
	defer srv.Close()

	var payload struct {
		DownloadTotal int64 `json:"downloadTotal"`
		UploadTotal   int64 `json:"uploadTotal"`
		Connections   []struct {
			ID string `json:"id"`
		} `json:"connections"`
	}
	if err := clientFor(t, srv, "x").RequestInto("GET", "/connections", nil, 5000, &payload); err != nil {
		t.Fatalf("err: %v", err)
	}
	if payload.DownloadTotal != 10 || payload.UploadTotal != 20 || len(payload.Connections) != 1 || payload.Connections[0].ID != "a" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestRequestIntoErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	c := clientFor(t, srv, "x")

	var out map[string]any
	ce, ok := c.RequestInto("GET", "/bad", nil, 5000, &out).(*ClashError)
	if !ok || ce.Message != "boom" || ce.StatusCode != 502 {
		t.Fatalf("http error = %v", ce)
	}
	if err := c.RequestInto("GET", "/text", nil, 5000, &out); err == nil {
		t.Fatalf("non-JSON body should error")
	}
}

func TestClientConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	port := func() int { u, _ := url.Parse(srv.URL); p, _ := strconv.Atoi(u.Port()); return p }()
	srv.Close()
	_, err := NewClient(port, "x").Request("GET", "/version", nil, 1000)
	ce, ok := err.(*ClashError)
	if !ok || ce.StatusCode != 502 || !strings.HasPrefix(ce.Message, "无法连接 mihomo 内核：") {
		t.Fatalf("want 502 connect error, got %v", err)
	}
}

func TestWSURL(t *testing.T) {
	c := NewClient(9090, "sec ret")
	if got := c.WSURL("/traffic"); got != "ws://127.0.0.1:9090/traffic?token=sec%20ret" {
		t.Fatalf("WSURL no-query = %q", got)
	}
	if got := c.WSURL("/logs?level=info"); got != "ws://127.0.0.1:9090/logs?level=info&token=sec%20ret" {
		t.Fatalf("WSURL with-query = %q", got)
	}
	if c.WSBaseURL() != "ws://127.0.0.1:9090" {
		t.Fatalf("WSBaseURL = %q", c.WSBaseURL())
	}
}
