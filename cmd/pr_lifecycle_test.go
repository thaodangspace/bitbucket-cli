package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPRViewAndGetAlias(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/2.0/repositories/team/repo/pullrequests/7" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return jsonResp(200, `{"id":7,"title":"Review me","state":"OPEN"}`), nil
	}, "pr", "view", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if value["id"] != float64(7) {
		t.Fatalf("unexpected response: %s", out)
	}
}

func TestPRGetAliasSupportsViewFlags(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			return jsonResp(200, `{"values":[{"id":9}]}`), nil
		}
		return jsonResp(200, `{"id":7,"title":"Alias","state":"OPEN"}`), nil
	}, "pr", "get", "7", "--comments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Alias") || !strings.Contains(out, "comments") {
		t.Fatalf("unexpected alias output: %s", out)
	}
}

func TestPRGetAliasSupportsBranchSelector(t *testing.T) {
	var paths []string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/pullrequests") {
			return jsonResp(200, `{"values":[{"id":8}]}`), nil
		}
		return jsonResp(200, `{"id":8,"title":"Branch alias","state":"OPEN"}`), nil
	}, "pr", "get", "feature/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/pullrequests") || !strings.HasSuffix(paths[1], "/pullrequests/8") {
		t.Fatalf("unexpected alias paths: %v", paths)
	}
}

func TestPRDiffPatchPreservesRawBytes(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/2.0/repositories/team/repo/pullrequests/7/patch" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("diff --git a/a b/a\n")), Header: http.Header{}}, nil
	}, "pr", "diff", "7", "--patch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "diff --git a/a b/a\n" {
		t.Fatalf("raw diff changed: %q", out)
	}
}

func TestPRMergeReadsStateBeforePosting(t *testing.T) {
	var methods []string
	var body []byte
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			return jsonResp(200, `{"id":7,"state":"OPEN","title":"Merge me"}`), nil
		}
		body, _ = io.ReadAll(r.Body)
		return jsonResp(200, `{"id":7,"state":"MERGED"}`), nil
	}, "pr", "merge", "7", "--strategy", "squash", "--message", "ship it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != "GET /2.0/repositories/team/repo/pullrequests/7" || methods[1] != "POST /2.0/repositories/team/repo/pullrequests/7/merge" {
		t.Fatalf("unexpected calls: %v", methods)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["merge_strategy"] != "squash" || payload["message"] != "ship it" {
		t.Fatalf("unexpected merge payload: %s", body)
	}
	if !strings.Contains(out, `"MERGED"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPRMergeAsyncUsesLocationAndTaskStatus(t *testing.T) {
	calls := 0
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		calls++
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pullrequests/7"):
			return jsonResp(200, `{"id":7,"state":"OPEN"}`), nil
		case r.Method == http.MethodPost:
			resp := jsonResp(http.StatusAccepted, "")
			resp.Header.Set("Location", "https://api.bitbucket.org/2.0/repositories/team/repo/pullrequests/7/merge/task-status/task-1")
			return resp, nil
		case strings.HasSuffix(r.URL.Path, "/merge/task-status/task-1") && calls == 3:
			return jsonResp(200, `{"task_status":"PENDING"}`), nil
		default:
			return jsonResp(200, `{"task_status":"SUCCESS"}`), nil
		}
	}, "pr", "merge", "7", "--interval", "1ms")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 4 || !strings.Contains(out, `"SUCCESS"`) {
		t.Fatalf("unexpected async result: calls=%d output=%s", calls, out)
	}
}

func TestPRMergeRejectsClosedState(t *testing.T) {
	posted := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posted = true
		}
		return jsonResp(200, `{"id":7,"state":"DECLINED"}`), nil
	}, "pr", "merge", "7")
	if err == nil || posted {
		t.Fatalf("expected closed-state rejection, err=%v posted=%v", err, posted)
	}
}

func TestPRDeclineRequiresConfirmation(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) { called = true; return jsonResp(200, `{}`), nil }, "pr", "decline", "7")
	if err == nil || called {
		t.Fatalf("expected non-TTY confirmation error, err=%v called=%v", err, called)
	}
}

func TestPRStatusMineUsesWorkspaceUserEndpoint(t *testing.T) {
	var path string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		if strings.HasSuffix(path, "/user") {
			return jsonResp(200, `{"account_id":"me-123"}`), nil
		}
		return jsonResp(200, `{"values":[{"id":1,"title":"Mine","state":"OPEN"}]}`), nil
	}, "pr", "status", "--mine")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/2.0/workspaces/team/pullrequests/me-123" {
		t.Fatalf("unexpected mine path: %s", path)
	}
	if !strings.Contains(out, "Mine") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPRStatusReviewRequestedUsesRepositoryFilter(t *testing.T) {
	var requestURL string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		requestURL = r.URL.String()
		if strings.HasSuffix(r.URL.Path, "/user") {
			return jsonResp(200, `{"account_id":"me-123"}`), nil
		}
		return jsonResp(200, `{"values":[{"id":2,"title":"Review","state":"OPEN"}]}`), nil
	}, "pr", "status", "--review-requested")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(requestURL, "/repositories/team/repo/pullrequests") || !strings.Contains(requestURL, "reviewers.account_id") {
		t.Fatalf("unexpected review URL: %s", requestURL)
	}
	if !strings.Contains(out, "Review") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPRChecksFailed(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"values":[{"name":"unit","state":"FAILED"}]}`), nil
	}, "pr", "checks", "7")
	if err == nil {
		t.Fatal("expected failed checks error")
	}
	if !strings.Contains(out, "FAILED") {
		t.Fatalf("expected check output: %s", out)
	}
}
