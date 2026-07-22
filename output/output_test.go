package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, []int{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("empty slice should render []: %q", buf.String())
	}
}

func TestRenderLinesEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderLines(&buf, nil, "No pull requests found."); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "No pull requests found." {
		t.Fatalf("got %q", buf.String())
	}
}

func TestPullRequestSummary(t *testing.T) {
	got := PullRequestSummary(decode(t, `{"id":12,"title":"Fix bug","state":"OPEN"}`))
	if got != "#12 Fix bug [OPEN]" {
		t.Fatalf("got %q", got)
	}
	got = PullRequestSummary(decode(t, `{"id":7}`))
	if got != "#7 (untitled) [unknown]" {
		t.Fatalf("got %q", got)
	}
}

func TestCommentSummary(t *testing.T) {
	got := CommentSummary(decode(t, `{"id":5,"content":{"raw":"hello"}}`))
	if got != "#5: hello" {
		t.Fatalf("got %q", got)
	}
}

func TestCommitSummary(t *testing.T) {
	got := CommitSummary(decode(t, `{"hash":"abcdef0123456789","message":"do thing"}`))
	if got != "abcdef012345 do thing" {
		t.Fatalf("got %q", got)
	}
	got = CommitSummary(decode(t, `{"hash":"abcdef0123456789"}`))
	if got != "abcdef012345" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchSummary(t *testing.T) {
	if got := BranchSummary(decode(t, `{"name":"main"}`)); got != "main" {
		t.Fatalf("got %q", got)
	}
	if got := BranchSummary(decode(t, `{"target":{"hash":"deadbeef"}}`)); got != "deadbeef" {
		t.Fatalf("got %q", got)
	}
}

func TestRepoSummary(t *testing.T) {
	if got := RepoSummary(decode(t, `{"full_name":"team/repo"}`)); got != "Repository: team/repo" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteErrorHTTP(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, &bitbucket.HTTPError{Method: "GET", URL: "https://x", Status: 403, StatusText: "Forbidden", Excerpt: "denied"})

	var env map[string]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("not json: %v\n%s", err, buf.String())
	}
	e := env["error"]
	if e["status"].(float64) != 403 {
		t.Fatalf("status missing: %v", e)
	}
	if e["method"] != "GET" || e["url"] != "https://x" || e["excerpt"] != "denied" {
		t.Fatalf("http fields missing: %v", e)
	}
	if !strings.Contains(e["message"].(string), "authorization failed") {
		t.Fatalf("message: %v", e["message"])
	}
}

func TestWriteErrorPlain(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, errors.New("Provide a non-empty body."))

	var env map[string]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("not json: %v", err)
	}
	e := env["error"]
	if e["message"] != "Provide a non-empty body." {
		t.Fatalf("message: %v", e["message"])
	}
	if _, ok := e["status"]; ok {
		t.Fatalf("plain error should omit status: %v", e)
	}
}
