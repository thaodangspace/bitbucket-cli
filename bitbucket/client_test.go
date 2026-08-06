package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/auth"
)

// roundTripFunc lets a test stand in for an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func testClient(rt roundTripFunc) *Client {
	auth := auth.NewBasicAuth(auth.TokenAPI, "dev@example.com", "token")
	return NewClient(auth, WithHTTPClient(&http.Client{Transport: rt}))
}

func TestEncodePathSegment(t *testing.T) {
	if got := EncodePathSegment("team/repo name"); got != "team%2Frepo%20name" {
		t.Fatalf("got %q", got)
	}
}

func TestRequestSendsBasicAuthAndAccept(t *testing.T) {
	var captured *http.Request
	c := testClient(func(r *http.Request) (*http.Response, error) {
		captured = r
		return jsonResponse(200, `{"type":"repository","name":"repo"}`), nil
	})

	var out map[string]any
	if err := c.Request(context.Background(), "/repositories/team/repo", RequestOptions{}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.URL.String() != "https://api.bitbucket.org/2.0/repositories/team/repo" {
		t.Fatalf("unexpected url: %s", captured.URL)
	}
	if captured.Method != http.MethodGet {
		t.Fatalf("unexpected method: %s", captured.Method)
	}
	if got := captured.Header.Get("Authorization"); got != "Basic ZGV2QGV4YW1wbGUuY29tOnRva2Vu" {
		t.Fatalf("unexpected auth header: %s", got)
	}
	if got := captured.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected accept header: %s", got)
	}
}

func TestRequestPostsJSONBody(t *testing.T) {
	var captured *http.Request
	var body []byte
	c := testClient(func(r *http.Request) (*http.Response, error) {
		captured = r
		body, _ = io.ReadAll(r.Body)
		return jsonResponse(201, `{"id":42}`), nil
	})

	payload := map[string]any{"content": map[string]any{"raw": "hi"}}
	var out map[string]any
	if err := c.Request(context.Background(), "/x", RequestOptions{Method: http.MethodPost, Body: payload}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Method != http.MethodPost {
		t.Fatalf("unexpected method: %s", captured.Method)
	}
	if got := captured.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content-type: %s", got)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if sent["content"].(map[string]any)["raw"] != "hi" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestUploadFilesSendsMultipartBody(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.log")
	if err := os.WriteFile(first, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	if err := os.WriteFile(second, []byte("world"), 0o600); err != nil {
		t.Fatalf("write second file: %v", err)
	}

	var captured *http.Request
	var body []byte
	c := testClient(func(r *http.Request) (*http.Response, error) {
		captured = r
		body, _ = io.ReadAll(r.Body)
		return jsonResponse(201, `{}`), nil
	})

	err := c.UploadFiles(context.Background(), "/repositories/team/repo/downloads", "files", []UploadFile{
		{Path: first},
		{Path: second, Name: "renamed.log"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Method != http.MethodPost {
		t.Fatalf("unexpected method: %s", captured.Method)
	}
	if captured.URL.String() != "https://api.bitbucket.org/2.0/repositories/team/repo/downloads" {
		t.Fatalf("unexpected url: %s", captured.URL)
	}
	if got := captured.Header.Get("Authorization"); got != "Basic ZGV2QGV4YW1wbGUuY29tOnRva2Vu" {
		t.Fatalf("unexpected auth header: %s", got)
	}
	if got := captured.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected accept header: %s", got)
	}
	contentType := captured.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Fatalf("unexpected content-type: %s", contentType)
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1024)
	if err != nil {
		t.Fatalf("parse multipart body: %v", err)
	}
	parts := form.File["files"]
	if len(parts) != 2 {
		t.Fatalf("expected 2 file parts, got %d", len(parts))
	}
	if parts[0].Filename != "first.txt" || parts[1].Filename != "renamed.log" {
		t.Fatalf("unexpected filenames: %q %q", parts[0].Filename, parts[1].Filename)
	}
	assertPartContent := func(i int, want string) {
		t.Helper()
		f, err := parts[i].Open()
		if err != nil {
			t.Fatalf("open multipart part: %v", err)
		}
		defer f.Close()
		got, _ := io.ReadAll(f)
		if string(got) != want {
			t.Fatalf("part %d content = %q, want %q", i, got, want)
		}
	}
	assertPartContent(0, "hello")
	assertPartContent(1, "world")
}

func TestUploadFilesMapsHTTPErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(file, []byte("artifact"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(406, `unsupported content type`), nil
	})

	err := c.UploadFiles(context.Background(), "/repositories/team/repo/downloads", "files", []UploadFile{{Path: file}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if he.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", he.Method)
	}
	if he.URL != "https://api.bitbucket.org/2.0/repositories/team/repo/downloads" {
		t.Fatalf("unexpected url: %s", he.URL)
	}
	if he.Status != 406 {
		t.Fatalf("status = %d, want 406", he.Status)
	}
	if he.Excerpt != "unsupported content type" {
		t.Fatalf("excerpt = %q", he.Excerpt)
	}
}

func TestPaginateFollowsNext(t *testing.T) {
	calls := 0
	c := testClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(200, `{"values":[{"id":1}],"next":"https://api.bitbucket.org/2.0/next-page"}`), nil
		}
		return jsonResponse(200, `{"values":[{"id":2}]}`), nil
	})

	values, err := c.Paginate(context.Background(), "/first", 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestPaginateRespectsLimit(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"values":[{"id":1},{"id":2},{"id":3}]}`), nil
	})
	values, err := c.Paginate(context.Background(), "/first", 2, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(values))
	}
}

func TestRequestMapsHTTPErrors(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{401, "authentication failed"},
		{403, "authorization failed"},
		{404, "resource not found"},
		{429, "rate limit reached"},
		{500, "request failed with 500"},
	}
	for _, tc := range cases {
		c := testClient(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(tc.status, `{"error":{"message":"denied"}}`), nil
		})
		err := c.Request(context.Background(), "/private", RequestOptions{}, nil)
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		var he *HTTPError
		if !errors.As(err, &he) {
			t.Fatalf("status %d: expected *HTTPError, got %T", tc.status, err)
		}
		if he.Status != tc.status {
			t.Fatalf("status mismatch: got %d want %d", he.Status, tc.status)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("status %d: message %q missing %q", tc.status, err.Error(), tc.want)
		}
	}
}

func TestExcerptTruncates(t *testing.T) {
	long := strings.Repeat("x", 600)
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(500, long), nil
	})
	err := c.Request(context.Background(), "/x", RequestOptions{}, nil)
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError")
	}
	if len(he.Excerpt) != excerptLimit+len("...") {
		t.Fatalf("excerpt not truncated: len=%d", len(he.Excerpt))
	}
}

func TestForbiddenIncludesRequiredScope(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(403, `{"error":{"message":"denied"}}`), nil
	})
	err := c.Request(context.Background(), "/repositories/team/repo/downloads", RequestOptions{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "write:repository:bitbucket") {
		t.Fatalf("403 message should name the required scope: %v", err)
	}
}

func TestDoStreamsBodyAndHeaders(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/plain"}, "X-Custom": {"v"}},
			Body:       io.NopCloser(strings.NewReader("streamed")),
		}, nil
	})
	resp, err := c.Do(context.Background(), "/x", RequestOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Custom") != "v" {
		t.Fatalf("missing header: %v", resp.Header)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "streamed" {
		t.Fatalf("body = %q", body)
	}
}

func TestDoRawBodyAndHeaders(t *testing.T) {
	var captured *http.Request
	c := testClient(func(r *http.Request) (*http.Response, error) {
		captured = r
		return jsonResponse(201, `{}`), nil
	})
	headers := http.Header{"Accept": {"text/plain"}, "X-Trace": {"1"}}
	raw := []byte(`{"title":"raw"}`)
	resp, err := c.Do(context.Background(), "/x", RequestOptions{Method: http.MethodPost, RawBody: raw, Headers: headers})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if captured.Header.Get("Accept") != "text/plain" {
		t.Fatalf("custom Accept not honored: %q", captured.Header.Get("Accept"))
	}
	if captured.Method != http.MethodPost {
		t.Fatalf("method = %s", captured.Method)
	}
	body, _ := io.ReadAll(captured.Body)
	if string(body) != `{"title":"raw"}` {
		t.Fatalf("raw body = %q", body)
	}
}

func TestDoBodyAndRawBodyConflict(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{}`), nil
	})
	_, err := c.Do(context.Background(), "/x", RequestOptions{Body: map[string]any{"a": 1}, RawBody: []byte("b")})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestDoAppendsQuery(t *testing.T) {
	var gotURL string
	c := testClient(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResponse(200, `{}`), nil
	})
	_, err := c.Do(context.Background(), "/x?existing=1", RequestOptions{Query: url.Values{"a": {"1", "2"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != "https://api.bitbucket.org/2.0/x?a=1&a=2&existing=1" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
}

func TestPaginateDetectsLoop(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"values":[{"id":1}],"next":"https://api.bitbucket.org/2.0/loop"}`), nil
	})
	_, err := c.Paginate(context.Background(), "/loop", 10, 5)
	if err == nil {
		t.Fatal("expected loop detection error")
	}
	if !strings.Contains(err.Error(), "pagination loop") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetriesOn429WithRetryAfter(t *testing.T) {
	calls := 0
	c := testClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			h := http.Header{}
			h.Set("Retry-After", "1")
			return &http.Response{StatusCode: 429, Header: h, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`))}, nil
		}
		return jsonResponse(200, `{"ok":true}`), nil
	})
	_, err := c.Do(context.Background(), "/x", RequestOptions{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected retry to succeed, calls=%d", calls)
	}
}

func TestNoRetryOnNonIdempotent(t *testing.T) {
	calls := 0
	c := testClient(func(r *http.Request) (*http.Response, error) {
		calls++
		h := http.Header{}
		h.Set("Retry-After", "1")
		return &http.Response{StatusCode: 429, Header: h, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	_, err := c.Do(context.Background(), "/x", RequestOptions{Method: http.MethodPost})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("POST must not be retried, calls=%d", calls)
	}
}
