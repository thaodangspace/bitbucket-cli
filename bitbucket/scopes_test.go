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
	}
	for _, tt := range tests {
		if got := RequiredScopesFor(tt.method, tt.path); got != tt.want {
			t.Errorf("RequiredScopesFor(%s, %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}
