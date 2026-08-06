package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRReviewPostsBodyThenApproves(t *testing.T) {
	var methods []string
	var paths []string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments") {
			return jsonResp(http.StatusCreated, `{"id":41,"content":{"raw":"LGTM"}}`), nil
		}
		return jsonResp(http.StatusOK, `{"approved":true}`), nil
	}, "pr", "review", "12", "--approve", "--body", "LGTM")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodPost {
		t.Fatalf("unexpected calls: %v %v", methods, paths)
	}
	if !strings.HasSuffix(paths[1], "/pullrequests/12/approve") {
		t.Fatalf("unexpected approval path: %s", paths[1])
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatal(err)
	}
	if value["action"] != "approve" || value["comment"] == nil {
		t.Fatalf("unexpected result: %#v", value)
	}
}

func TestPRReviewReportsPartialSuccess(t *testing.T) {
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			return jsonResp(201, `{"id":9}`), nil
		}
		return jsonResp(403, `{"error":{"message":"forbidden"}}`), nil
	}, "pr", "review", "12", "--approve", "--body", "posted first")
	partial, ok := err.(*partialReviewError)
	if !ok {
		t.Fatalf("expected partial review error, got %T: %v", err, err)
	}
	if partial.commentID != int64(9) {
		t.Fatalf("unexpected comment id: %#v", partial.commentID)
	}
	if !partial.Details()["partial_success"].(bool) {
		t.Fatal("missing partial success detail")
	}
}

func TestInlineCommentValidation(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) { called = true; return jsonResp(201, `{}`), nil }, "pr", "comment", "12", "--body", "note", "--path", "main.go")
	if err == nil || called {
		t.Fatalf("expected local inline validation error, called=%v err=%v", called, err)
	}
}

func TestCommentDeleteRequiresConfirmationAndHandles204(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) { called = true; return jsonResp(204, ""), nil }, "pr", "comment", "delete", "12", "4")
	if err == nil || called {
		t.Fatalf("expected --yes error, called=%v err=%v", called, err)
	}

	calls := 0
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResp(200, `{"id":4,"user":{"display_name":"me"}}`), nil
		}
		return jsonResp(204, ""), nil
	}, "pr", "comment", "delete", "12", "4", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["deleted"] != true || result["id"] != float64(4) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestReviewBodyFileFromStdin(t *testing.T) {
	file := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(file, []byte("from file"), 0600); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &sent)
		}
		return jsonResp(201, `{"id":1}`), nil
	}, "pr", "review", "12", "--comment", "--body-file", file)
	if err != nil {
		t.Fatal(err)
	}
	content := sent["content"].(map[string]any)
	if content["raw"] != "from file" {
		t.Fatalf("unexpected body: %#v", sent)
	}
}
