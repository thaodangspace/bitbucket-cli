package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestWorkspaceListRedactsEmailAndBuildsQuery(t *testing.T) {
	var gotURL string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"values":[{"name":"Team","slug":"team","email":"private@example.com"}]}`), nil
	}, "workspace", "list", "--role", "OWNER", "--query", `name="Team"`, "--limit", "3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotURL, "/workspaces") || !strings.Contains(gotURL, "role=owner") || !strings.Contains(gotURL, "pagelen=50") {
		t.Fatalf("unexpected URL: %s", gotURL)
	}
	if strings.Contains(out, "private@example.com") {
		t.Fatalf("private email leaked: %s", out)
	}
}

func TestProjectEditPreservesUnchangedFields(t *testing.T) {
	var method string
	var body map[string]any
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		method = r.Method
		if r.Method == http.MethodPut {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &body)
			return jsonResp(200, `{"key":"WEB","name":"New name","description":"old","is_private":true}`), nil
		}
		return jsonResp(200, `{"key":"WEB","name":"Old name","description":"old","is_private":true}`), nil
	}, "project", "edit", "web", "--name", "New name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != http.MethodPut || body["name"] != "New name" || body["description"] != "old" || body["is_private"] != true {
		t.Fatalf("unexpected read-modify-write body: %#v", body)
	}
	if !strings.Contains(out, `"key": "WEB"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPermissionReposUsesValidBBQLQuotes(t *testing.T) {
	var gotURL string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"values":[]}`), nil
	}, "permission", "repos", "--workspace", "team", "--user", "{123e4567-e89b-12d3-a456-426614174000}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotURL, `q=user.uuid%3D%22%7B123e4567-e89b-12d3-a456-426614174000%7D%22`) {
		t.Fatalf("expected quoted user BBQL, got %s", gotURL)
	}
	if strings.Contains(gotURL, "%5C") {
		t.Fatalf("BBQL unexpectedly contains escaped backslash: %s", gotURL)
	}
}

func TestWorkspaceMemberViewCommandHierarchy(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/members/") {
			return jsonResp(200, `{"user":{"uuid":"{123e4567-e89b-12d3-a456-426614174000}","display_name":"Alice"},"role":"member"}`), nil
		}
		t.Fatalf("unexpected request path: %s", r.URL.Path)
		return nil, nil
	}, "workspace", "member", "view", "{123e4567-e89b-12d3-a456-426614174000}", "--workspace", "team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Alice") {
		t.Fatalf("unexpected member output: %s", out)
	}
}

func TestPermissionGrantNoOpDoesNotWrite(t *testing.T) {
	calls := 0
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutation request: %s", r.Method)
		}
		return jsonResp(200, `{"permission":"read","user":{"uuid":"{user}"}}`), nil
	}, "permission", "grant", "--repository", "team/repo", "--user", "{123e4567-e89b-12d3-a456-426614174000}", "--permission", "read")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 || strings.Contains(out, `"changed": true`) {
		t.Fatalf("expected one read and unchanged result: calls=%d output=%s", calls, out)
	}
}

func TestWorkspaceInviteReturnsCapabilityError(t *testing.T) {
	_, err := run(t, nil, "workspace", "invite", "person@example.com")
	if err == nil || !strings.Contains(err.Error(), "workspace.invite") {
		t.Fatalf("expected targeted capability error, got %v", err)
	}
}
