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
