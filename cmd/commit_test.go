package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCommitListPreservesServerFiltersAndSlashRef(t *testing.T) {
	var got *http.Request
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		got = r
		return jsonResp(200, `{"values":[{"hash":"abcdef0123456789","message":"fix"}]}`), nil
	}, "commit", "list", "feature/login", "--path", "cmd/main.go", "--include", "main", "--exclude", "old", "--query", `author.raw~"dev"`)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !strings.Contains(got.URL.EscapedPath(), "/commits/feature%2Flogin") {
		t.Fatalf("slash-containing ref was not one path segment: %v", got)
	}
	q := got.URL.Query()
	if q.Get("path") != "cmd/main.go" || q.Get("q") == "" || q.Get("include") != "main" || q.Get("exclude") != "old" {
		t.Fatalf("missing commit filters: %s", got.URL.RawQuery)
	}
	var values []map[string]any
	if err := json.Unmarshal([]byte(out), &values); err != nil {
		t.Fatal(err)
	}
	if values[0]["requested_selector"] != "feature/login" || values[0]["resolved_hash"] != "abcdef0123456789" {
		t.Fatalf("missing selector metadata: %v", values[0])
	}
}

func TestCommitViewURLResolvesRepository(t *testing.T) {
	var gotPath string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return jsonResp(200, `{"hash":"abcdef0123456789","message":"fix"}`), nil
	}, "commit", "view", "https://bitbucket.org/other/project/commits/abcdef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/repositories/other/project/commit/abcdef0123456789") {
		t.Fatalf("unexpected URL: %s", gotPath)
	}
	if !strings.Contains(out, "resolved_hash") {
		t.Fatalf("missing resolved hash: %s", out)
	}
}

func TestCommitStatusSetValidatesAndUpserts(t *testing.T) {
	var method string
	var body string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		method = r.Method
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		return jsonResp(200, `{"key":"ci","state":"SUCCESSFUL"}`), nil
	}, "commit", "status", "set", "abcdef0123456789abcdef0123456789abcdef01", "--key", "ci", "--state", "successful", "--url", "https://ci.example.test/build/1")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || !strings.Contains(body, `"key":"ci"`) || !strings.Contains(body, `"state":"SUCCESSFUL"`) {
		t.Fatalf("unexpected status request: %s %s", method, body)
	}
	_, err = run(t, nil, "commit", "status", "set", "abcdef0123456789abcdef0123456789abcdef01", "--key", "ci", "--state", "SUCCESSFUL", "--url", "https://user:secret@example.test")
	if err == nil {
		t.Fatal("expected credential-bearing URL validation error")
	}
}

func TestCommitCommentDeleteRequiresConfirmation(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) { called = true; return jsonResp(204, ""), nil }, "commit", "comment", "delete", "abcdef0123456789abcdef0123456789abcdef01", "7")
	if err == nil || called {
		t.Fatal("delete without --yes should fail before making a request")
	}
}
