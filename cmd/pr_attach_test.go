package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRAttachUploadsAndComments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test report.txt")
	if err := os.WriteFile(file, []byte("artifact body"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	type call struct {
		method      string
		url         string
		contentType string
		body        []byte
	}
	var calls []call
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, call{
			method:      r.Method,
			url:         r.URL.String(),
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		})
		if len(calls) == 1 {
			return jsonResp(201, `{}`), nil
		}
		return jsonResp(201, `{"id":99}`), nil
	}, "pr", "attach", "123", "--file", file, "--message", "See artifacts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 HTTP calls, got %d", len(calls))
	}
	if calls[0].method != http.MethodPost || !strings.HasSuffix(calls[0].url, "/repositories/team/repo/downloads") {
		t.Fatalf("unexpected upload call: %s %s", calls[0].method, calls[0].url)
	}
	if !strings.HasPrefix(calls[0].contentType, "multipart/form-data; boundary=") {
		t.Fatalf("unexpected upload content type: %s", calls[0].contentType)
	}
	_, params, err := mime.ParseMediaType(calls[0].contentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	form, err := multipart.NewReader(bytes.NewReader(calls[0].body), params["boundary"]).ReadForm(1024)
	if err != nil {
		t.Fatalf("parse multipart body: %v", err)
	}
	parts := form.File["files"]
	if len(parts) != 1 || parts[0].Filename != "test report.txt" {
		t.Fatalf("unexpected multipart files: %#v", parts)
	}
	part, err := parts[0].Open()
	if err != nil {
		t.Fatalf("open multipart part: %v", err)
	}
	partBody, _ := io.ReadAll(part)
	_ = part.Close()
	if string(partBody) != "artifact body" {
		t.Fatalf("unexpected uploaded content: %q", partBody)
	}

	if calls[1].method != http.MethodPost || !strings.HasSuffix(calls[1].url, "/repositories/team/repo/pullrequests/123/comments") {
		t.Fatalf("unexpected comment call: %s %s", calls[1].method, calls[1].url)
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[1].body, &sent); err != nil {
		t.Fatalf("comment body not json: %v", err)
	}
	raw := sent["content"].(map[string]any)["raw"].(string)
	wantURL := "https://bitbucket.org/team/repo/downloads/test%20report.txt"
	if !strings.Contains(raw, "See artifacts\n\nAttached files:") || !strings.Contains(raw, "- [test report.txt]("+wantURL+")") {
		t.Fatalf("unexpected comment markdown: %q", raw)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output not json: %v\n%s", err, out)
	}
	if result["pullrequest_id"] != float64(123) {
		t.Fatalf("unexpected pr id: %#v", result["pullrequest_id"])
	}
	files := result["files"].([]any)
	first := files[0].(map[string]any)
	if first["name"] != "test report.txt" || first["url"] != wantURL {
		t.Fatalf("unexpected files output: %#v", first)
	}
	comment := result["comment"].(map[string]any)
	if comment["id"] != float64(99) {
		t.Fatalf("unexpected comment output: %#v", comment)
	}
}

func TestPRAttachPretty(t *testing.T) {
	file := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(file, []byte("artifact"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	calls := 0
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResp(201, `{}`), nil
		}
		return jsonResp(201, `{"id":99}`), nil
	}, "pr", "attach", "123", "--file", file, "--pretty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "Attached 1 file to pull request #123." {
		t.Fatalf("unexpected pretty output: %q", out)
	}
}

func TestPRAttachRequiresFile(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(201, `{}`), nil
	}, "pr", "attach", "123")
	if err == nil {
		t.Fatal("expected missing file error")
	}
	if called {
		t.Fatal("should not call API when --file is missing")
	}
}

func TestPRAttachRejectsInvalidFilesBeforeHTTP(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		file string
	}{
		{name: "missing", file: filepath.Join(dir, "missing.txt")},
		{name: "directory", file: dir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			_, err := run(t, func(r *http.Request) (*http.Response, error) {
				called = true
				return jsonResp(201, `{}`), nil
			}, "pr", "attach", "123", "--file", tc.file)
			if err == nil {
				t.Fatal("expected invalid file error")
			}
			if called {
				t.Fatal("should not call API for invalid file")
			}
		})
	}
}

func TestPRAttachRejectsDuplicateBasenames(t *testing.T) {
	dir := t.TempDir()
	firstDir := filepath.Join(dir, "a")
	secondDir := filepath.Join(dir, "b")
	if err := os.Mkdir(firstDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(secondDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	first := filepath.Join(firstDir, "same.txt")
	second := filepath.Join(secondDir, "same.txt")
	if err := os.WriteFile(first, []byte("one"), 0o600); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("two"), 0o600); err != nil {
		t.Fatalf("write second: %v", err)
	}
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(201, `{}`), nil
	}, "pr", "attach", "123", "--file", first, "--file", second)
	if err == nil {
		t.Fatal("expected duplicate basename error")
	}
	if called {
		t.Fatal("should not call API for duplicate basenames")
	}
}
