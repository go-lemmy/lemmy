package lemmy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewDefaults(t *testing.T) {
	c := New("https://lemmy.world/")
	if c.Instance != "https://lemmy.world" {
		t.Fatalf("trailing slash not trimmed: %q", c.Instance)
	}
	if c.HTTPClient != http.DefaultClient || c.UserAgent != "go-lemmy/lemmy" {
		t.Fatalf("defaults wrong: %+v", c)
	}
	c2 := New("x", WithHTTPClient(&http.Client{Timeout: time.Second}), WithUserAgent("ua"))
	if c2.UserAgent != "ua" || c2.HTTPClient.Timeout != time.Second {
		t.Fatalf("options not applied: %+v", c2)
	}
}

func TestHTTPClientFallback(t *testing.T) {
	if (&Client{}).httpClient() != http.DefaultClient {
		t.Fatal("nil HTTPClient should fall back to http.DefaultClient")
	}
}

func TestPostsDefaultsAndMapping(t *testing.T) {
	var gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"posts":[
			{"post":{"id":1,"name":"T","url":"https://x","body":"b","ap_id":"https://lemmy.world/post/1","thumbnail_url":"https://t","published":"2026-07-10T12:00:00Z","nsfw":true},
			 "creator":{"name":"alice"},"community":{"name":"tech"},"counts":{"score":9,"comments":4}}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.jwt = "tok"
	res, err := c.Posts(context.Background(), PostsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "sort=Hot") || !strings.Contains(gotQuery, "type_=All") ||
		!strings.Contains(gotQuery, "limit=20") || !strings.Contains(gotQuery, "page=1") {
		t.Fatalf("defaults not applied: %s", gotQuery)
	}
	if strings.Contains(gotQuery, "community_name") {
		t.Fatalf("community should be omitted: %s", gotQuery)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	p := res.Posts[0]
	if p.ID != 1 || p.Title != "T" || p.URL != "https://x" || p.Permalink != "https://lemmy.world/post/1" ||
		p.Creator != "alice" || p.Community != "tech" || p.Score != 9 || p.Comments != 4 || !p.NSFW ||
		p.Published.Unix() != time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("mapping wrong: %+v", p)
	}
}

func TestPostsExplicitOptions(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		io.WriteString(w, `{"posts":[]}`)
	}))
	defer srv.Close()
	_, err := New(srv.URL).Posts(context.Background(), PostsOptions{Community: "tech@lemmy.world", Sort: "New", Type: "Local", Limit: 5, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sort=New", "type_=Local", "limit=5", "page=2", "community_name=tech"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("missing %q in %s", want, gotQuery)
		}
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/user/login" || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(400)
			return
		}
		io.WriteString(w, `{"jwt":"secret"}`)
	}))
	defer srv.Close()
	c := New(srv.URL)
	if err := c.Login(context.Background(), "u", "p"); err != nil {
		t.Fatal(err)
	}
	if c.jwt != "secret" {
		t.Fatalf("jwt = %q", c.jwt)
	}
}

func TestLoginError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "nope")
	}))
	defer srv.Close()
	if err := New(srv.URL).Login(context.Background(), "u", "p"); err == nil {
		t.Fatal("want login error")
	}
}

func TestDoErrors(t *testing.T) {
	// Non-2xx with a long body (exercises the 512 truncation).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, strings.Repeat("x", 600))
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Posts(context.Background(), PostsOptions{}); err == nil {
		t.Fatal("want non-2xx error")
	}

	// Decode error: 200 with invalid JSON.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "{not json")
	}))
	defer bad.Close()
	if _, err := New(bad.URL).Posts(context.Background(), PostsOptions{}); err == nil {
		t.Fatal("want decode error")
	}

	// Request-build error: an invalid method token.
	if err := (&Client{Instance: "http://x", HTTPClient: http.DefaultClient}).do(context.Background(), "BAD METHOD", "/p", nil, nil, nil); err == nil {
		t.Fatal("want request build error")
	}

	// Transport error.
	rt := &Client{Instance: "http://x", HTTPClient: &http.Client{Transport: errRoundTripper{}}, UserAgent: "u"}
	if err := rt.do(context.Background(), http.MethodGet, "/p", nil, nil, nil); err == nil {
		t.Fatal("want transport error")
	}

	// Body-read error.
	br := &Client{Instance: "http://x", HTTPClient: &http.Client{Transport: badBodyRoundTripper{}}, UserAgent: "u"}
	if err := br.do(context.Background(), http.MethodGet, "/p", nil, nil, nil); err == nil {
		t.Fatal("want body read error")
	}
}

func TestParseTime(t *testing.T) {
	if !parseTime("").IsZero() {
		t.Fatal("empty should be zero")
	}
	if !parseTime("garbage").IsZero() {
		t.Fatal("garbage should be zero")
	}
	if parseTime("2026-07-10T12:00:00Z").IsZero() {
		t.Fatal("RFC3339 should parse")
	}
	if parseTime("2026-07-10T12:00:00.500Z").IsZero() {
		t.Fatal("RFC3339Nano should parse")
	}
	if parseTime("2026-07-10T12:00:00").IsZero() {
		t.Fatal("no-zone layout should parse")
	}
}

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

type badBodyRoundTripper struct{}

func (badBodyRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(errReader{}), Header: make(http.Header)}, nil
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read boom") }
