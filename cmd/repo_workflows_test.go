package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRepoListFilters(t *testing.T) {
	var got *http.Request
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return jsonResp(200, `{"values":[{"full_name":"team/one","name":"one"}]}`), nil
	}, "repo", "list", "team", "--private", "--project", "OPS", "--limit", "3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.URL.Path != "/2.0/repositories/team" {
		t.Fatalf("unexpected request: %v", got)
	}
	if got.URL.Query().Get("pagelen") != "50" || got.URL.Query().Get("q") == "" {
		t.Fatalf("missing list query: %s", got.URL.RawQuery)
	}
	if !strings.Contains(got.URL.Query().Get("q"), "is_private=true") || !strings.Contains(got.URL.Query().Get("q"), `project.key="OPS"`) {
		t.Fatalf("unexpected q: %s", got.URL.Query().Get("q"))
	}
}

func TestRepoCreatePayload(t *testing.T) {
	var method, path string
	var body map[string]any
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		method, path = r.Method, r.URL.Path
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		return jsonResp(201, `{"full_name":"team/new-repo","name":"new-repo"}`), nil
	}, "repo", "create", "new-repo", "--workspace", "team", "--private", "--description", "hello", "--project", "OPS", "--main-branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != http.MethodPost || path != "/2.0/repositories/team" {
		t.Fatalf("unexpected request %s %s", method, path)
	}
	if body["name"] != "new-repo" || body["is_private"] != true || body["description"] != "hello" {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if _, ok := body["project"].(map[string]any); !ok {
		t.Fatalf("project missing: %#v", body)
	}
	if !strings.Contains(out, "team/new-repo") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRepoEditOnlySendsChangedFields(t *testing.T) {
	var body map[string]any
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return jsonResp(200, `{"full_name":"team/repo","name":"repo","is_private":true}`), nil
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		return jsonResp(200, `{"full_name":"team/repo","name":"repo"}`), nil
	}, "repo", "edit", "team/repo", "--description", "updated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["name"] != "repo" || body["is_private"] != true || body["description"] != "updated" {
		t.Fatalf("unexpected read-modify-write payload: %#v", body)
	}
}

func TestRepoDeleteRequiresConfirmation(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) { called = true; return jsonResp(204, ""), nil }, "repo", "delete", "team/repo")
	if err == nil || called {
		t.Fatalf("delete should require --yes, err=%v called=%v", err, called)
	}
}

func TestRepoViewReadmeExplicitBranch(t *testing.T) {
	var paths []string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("# README\n")), Header: http.Header{}}, nil
	}, "repo", "view", "team/repo", "--readme", "--branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/src/main/README") {
		t.Fatalf("unexpected README request: %v", paths)
	}
	if out != "# README\n" {
		t.Fatalf("unexpected README: %q", out)
	}
}

func TestRepoClonePassesGitFlagsAsArguments(t *testing.T) {
	var got []string
	old := gitClone
	gitClone = func(_ context.Context, args ...string) error { got = append([]string(nil), args...); return nil }
	t.Cleanup(func() { gitClone = old })
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"links":{"clone":[{"name":"https","href":"https://bitbucket.org/team/repo.git"}]}}`), nil
	}, "repo", "clone", "team/repo", "dest", "--", "--depth", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://bitbucket.org/team/repo.git", "dest", "--depth", "1"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("git args = %#v, want %#v", got, want)
	}
}
