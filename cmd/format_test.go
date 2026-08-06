package cmd

import (
	"net/http"
	"strings"
	"testing"
)

func TestPRListJSONProjectionJQ(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"values":[{"id":12,"title":"Fix","state":"OPEN","description":"private"}]}`), nil
	}, "pr", "list", "--json", "id,title", "--jq", ".[] | .title")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != `"Fix"` {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPRGetTemplate(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"id":12,"title":"Fix"}`), nil
	}, "pr", "get", "12", "--template", "{{.title}}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "Fix" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPRURLSelectorUsesURLRepository(t *testing.T) {
	var gotPath string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return jsonResp(200, `{"id":42,"title":"Fix"}`), nil
	}, "pr", "get", "https://bitbucket.org/other/project/pull-requests/42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/2.0/repositories/other/project/pullrequests/42" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestBranchListYAML(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"values":[{"name":"main","target":{"hash":"abc"}}]}`), nil
	}, "branch", "list", "--format", "yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "name: main") {
		t.Fatalf("unexpected yaml output: %q", out)
	}
}

func TestRepositorySelector(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/2.0/repositories/acme/widgets" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return jsonResp(200, `{"full_name":"acme/widgets"}`), nil
	}, "--repository", "https://bitbucket.org/acme/widgets", "repo", "get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "acme/widgets") {
		t.Fatalf("unexpected output: %q", out)
	}
}
