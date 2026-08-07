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

func TestBranchDeleteProtectsConfiguredMainBranch(t *testing.T) {
	deleted := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete {
			deleted = true
		}
		return jsonResp(http.StatusOK, `{"mainbranch":{"name":"main"}}`), nil
	}, "branch", "delete", "main", "--yes")
	if err == nil || deleted || !strings.Contains(err.Error(), "main branch") {
		t.Fatalf("expected main branch protection, err=%v deleted=%v", err, deleted)
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

func TestRestrictionCreatesBothMatchModes(t *testing.T) {
	for _, test := range []struct {
		name  string
		args  []string
		check func(*testing.T, map[string]any)
	}{
		{name: "glob", args: []string{"--kind", "push", "--pattern", "main"}, check: func(t *testing.T, body map[string]any) {
			if body["branch_match_kind"] != "glob" || body["pattern"] != "main" {
				t.Fatalf("unexpected glob body: %#v", body)
			}
			if _, ok := body["users"].([]any); !ok {
				t.Fatalf("push users must be explicit: %#v", body)
			}
			if _, ok := body["groups"].([]any); !ok {
				t.Fatalf("push groups must be explicit: %#v", body)
			}
		}},
		{name: "branching-model", args: []string{"--kind", "require_approvals_to_merge", "--branch-type", "feature", "--value", "2"}, check: func(t *testing.T, body map[string]any) {
			if body["branch_match_kind"] != "branching_model" || body["branch_type"] != "feature" || body["value"] != float64(2) {
				t.Fatalf("unexpected branch-type body: %#v", body)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			args := append([]string{"branch-restriction", "create"}, test.args...)
			_, err := run(t, func(r *http.Request) (*http.Response, error) {
				data, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(data, &body)
				return jsonResp(http.StatusCreated, `{"id":1,"kind":"push","pattern":"main","branch_match_kind":"glob"}`), nil
			}, args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			test.check(t, body)
		})
	}
}

func TestRestrictionValidationRequiresOneMatchMode(t *testing.T) {
	_, err := run(t, nil, "branch-restriction", "create", "--kind", "push")
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected match-mode validation error: %v", err)
	}
}

func TestDefaultReviewerDisplayNameUnwrapsMembership(t *testing.T) {
	var path, memberQuery string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		path = r.URL.EscapedPath()
		if strings.HasSuffix(r.URL.EscapedPath(), "/users/Jane%20Doe") {
			return jsonResp(http.StatusNotFound, `{}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/members") {
			memberQuery = r.URL.RawQuery
			return jsonResp(http.StatusOK, `{"values":[{"user":{"uuid":"{u}","display_name":"Jane Doe"}}]}`), nil
		}
		return jsonResp(http.StatusOK, `{"uuid":"{u}","display_name":"Jane Doe"}`), nil
	}, "default-reviewer", "add", "Jane Doe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, "/default-reviewers/%7Bu%7D") || !strings.Contains(memberQuery, "user.display_name") {
		t.Fatalf("unexpected member lookup: path=%s query=%s", path, memberQuery)
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
