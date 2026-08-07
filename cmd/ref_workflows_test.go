package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBranchCreateResolvesSlashTargetAndReportsIt(t *testing.T) {
	var paths []string
	var body map[string]any
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.EscapedPath())
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.EscapedPath(), "/refs/branches/feature%2Fx"):
			return jsonResp(http.StatusNotFound, `{}`), nil
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.EscapedPath(), "/refs/branches/main"):
			return jsonResp(http.StatusOK, `{"target":{"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`), nil
		case r.Method == http.MethodPost:
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &body)
			return jsonResp(http.StatusCreated, `{"name":"feature/x"}`), nil
		default:
			return jsonResp(http.StatusNotFound, `{}`), nil
		}
	}, "branch", "create", "feature/x", "--target", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v (paths %v)", err, paths)
	}
	if body["name"] != "feature/x" || body["target"].(map[string]any)["hash"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected branch payload: %#v", body)
	}
	if !strings.Contains(out, `"requested_target": "main"`) || !strings.Contains(out, `"resolved_target": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) {
		t.Fatalf("target metadata missing: %s", out)
	}
}

func TestBranchCreateRejectsExistingRef(t *testing.T) {
	posted := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posted = true
		}
		return jsonResp(http.StatusOK, `{"name":"already-there"}`), nil
	}, "branch", "create", "already-there", "--target", "main")
	if err == nil || posted || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-ref rejection, err=%v posted=%v", err, posted)
	}
}

func TestBranchDeleteProtectsMainBeforeRequest(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(http.StatusOK, `{}`), nil
	}, "branch", "delete", "main", "--yes")
	if err == nil || called || !strings.Contains(err.Error(), "main branch") {
		t.Fatalf("expected main branch protection, err=%v called=%v", err, called)
	}
}

func TestBranchingModelEditSendsOnlyChangedSection(t *testing.T) {
	var put map[string]any
	calls := 0
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method == http.MethodPut {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &put)
			return jsonResp(http.StatusOK, `{"production":{"name":"release"}}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/branching-model/settings") {
			return jsonResp(http.StatusOK, `{"development":{"name":"develop","use_mainbranch":false},"production":{"name":"main","enabled":true},"branch_types":[{"kind":"feature","prefix":"feature/"}],"future_field":"keep"}`), nil
		}
		return jsonResp(http.StatusOK, `{"production":{"name":"release"},"branch_types":[]}`), nil
	}, "branching-model", "edit", "--production-branch", "release")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if put["production"].(map[string]any)["name"] != "release" || put["development"] != nil || put["future_field"] != nil {
		t.Fatalf("unexpected partial update: %#v", put)
	}
	if calls != 3 {
		t.Fatalf("expected settings GET, PUT, and effective GET; got %d calls", calls)
	}
}

func TestRestrictionValidationRequiresOneMatchMode(t *testing.T) {
	_, err := run(t, nil, "branch-restriction", "create", "--kind", "push")
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected match-mode validation error: %v", err)
	}
}

func TestDefaultReviewerAddResolvesUUIDWithoutLookup(t *testing.T) {
	var method, path string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		method, path = r.Method, r.URL.EscapedPath()
		return jsonResp(http.StatusOK, `{"uuid":"{12345678-1234-1234-1234-123456789abc}","display_name":"A Reviewer"}`), nil
	}, "default-reviewer", "add", "{12345678-1234-1234-1234-123456789abc}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != http.MethodPut || !strings.HasSuffix(path, "/default-reviewers/%7B12345678-1234-1234-1234-123456789abc%7D") {
		t.Fatalf("unexpected default reviewer request: %s %s", method, path)
	}
}
