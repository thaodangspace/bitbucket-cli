package bitbucket

import (
	"net/http"
	"testing"
)

func TestRequiredScopesForRepositoryWorkflows(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{http.MethodPost, "/repositories/team/new-repo", "admin:repository:bitbucket"},
		{http.MethodPut, "/repositories/team/repo", "admin:repository:bitbucket"},
		{http.MethodDelete, "/repositories/team/repo", "delete:repository:bitbucket"},
		{http.MethodPost, "/repositories/team/repo/forks", "read:repository:bitbucket and write:repository:bitbucket"},
		{http.MethodGet, "/repositories/team/repo/branch-restrictions", "admin:repository:bitbucket"},
		{http.MethodGet, "/repositories/team/repo/branching-model/settings", "admin:repository:bitbucket"},
		{http.MethodGet, "/repositories/team/repo/commit/abc/statuses", "read:repository:bitbucket"},
		{http.MethodPost, "/repositories/team/repo/commit/abc/statuses/build", "read:repository:bitbucket and write:repository:bitbucket"},
		{http.MethodGet, "/repositories/team/repo/commit/abc/reports", "read:repository:bitbucket"},
		{http.MethodPut, "/repositories/team/repo/commit/abc/reports/scan", "read:repository:bitbucket and write:repository:bitbucket"},
		{http.MethodPost, "/repositories/team/repo/commit/abc/approve", "read:repository:bitbucket and write:repository:bitbucket"},
		{http.MethodDelete, "/repositories/team/repo/commit/abc/approve", "read:repository:bitbucket and write:repository:bitbucket"},
		{http.MethodPost, "/repositories/team/repo/commit/abc/comments", "read:repository:bitbucket and write:repository:bitbucket"},
		{http.MethodGet, "/repositories/team/repo/pipelines-config/variables", "read:pipeline:bitbucket"},
		{http.MethodPost, "/repositories/team/repo/pipelines-config/variables", "admin:pipeline:bitbucket"},
		{http.MethodPut, "/workspaces/team/pipelines-config/runners/runner-1", "read:runner:bitbucket and write:runner:bitbucket"},
		{http.MethodDelete, "/repositories/team/repo/pipelines-config/runners/runner-1", "write:runner:bitbucket"},
		{http.MethodGet, "/hook_events/repository", ""},
		{http.MethodGet, "/repositories/team/repo/hooks", "read:webhook:bitbucket"},
		{http.MethodPost, "/repositories/team/repo/hooks", "read:webhook:bitbucket and write:webhook:bitbucket"},
		{http.MethodDelete, "/repositories/team/repo/hooks/hook-1", "delete:webhook:bitbucket"},
		{http.MethodGet, "/repositories/team/repo/deploy-keys", "admin:repository:bitbucket"},
		{http.MethodPost, "/users/{account}/ssh-keys", "read:ssh-key:bitbucket and write:ssh-key:bitbucket"},
		{http.MethodGet, "/user", "read:user:bitbucket"},
	}
	for _, tt := range tests {
		if got := RequiredScopesFor(tt.method, tt.path); got != tt.want {
			t.Errorf("RequiredScopesFor(%s, %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}
