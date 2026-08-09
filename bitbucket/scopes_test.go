package bitbucket

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/auth"
)

func TestRequiredScopesForCredentialFamilies(t *testing.T) {
	tests := []struct {
		name   string
		kind   auth.TokenType
		method string
		path   string
		want   []string
	}{
		{"api repository read", auth.TokenAPI, http.MethodGet, "/repositories/team/repo", []string{"read:repository:bitbucket"}},
		{"access repository read", auth.TokenAccess, http.MethodGet, "/repositories/team/repo", []string{"repository:read"}},
		{"oauth repository read", auth.TokenOAuth, http.MethodGet, "/repositories/team/repo", []string{"repository"}},
		{"api repository write", auth.TokenAPI, http.MethodPut, "/repositories/team/repo", []string{"admin:repository:bitbucket"}},
		{"access repository write", auth.TokenAccess, http.MethodPut, "/repositories/team/repo", []string{"repository:admin"}},
		{"oauth repository write", auth.TokenOAuth, http.MethodPut, "/repositories/team/repo", []string{"repository:admin"}},
		{"api repository delete", auth.TokenAPI, http.MethodDelete, "/repositories/team/repo", []string{"delete:repository:bitbucket"}},
		{"access repository delete", auth.TokenAccess, http.MethodDelete, "/repositories/team/repo", []string{"repository:delete"}},
		{"oauth repository delete", auth.TokenOAuth, http.MethodDelete, "/repositories/team/repo", []string{"repository:delete"}},
		{"api pull request write", auth.TokenAPI, http.MethodPut, "/repositories/team/repo/pullrequests/1", []string{"read:pullrequest:bitbucket", "write:pullrequest:bitbucket"}},
		{"access pull request write", auth.TokenAccess, http.MethodPut, "/repositories/team/repo/pullrequests/1", []string{"pullrequest:read", "pullrequest:write"}},
		{"oauth pull request write", auth.TokenOAuth, http.MethodPut, "/repositories/team/repo/pullrequests/1", []string{"pullrequest", "pullrequest:write"}},
		{"api pipeline read", auth.TokenAPI, http.MethodGet, "/repositories/team/repo/pipelines", []string{"read:pipeline:bitbucket"}},
		{"access pipeline read", auth.TokenAccess, http.MethodGet, "/repositories/team/repo/pipelines", []string{"pipeline:read"}},
		{"oauth pipeline read", auth.TokenOAuth, http.MethodGet, "/repositories/team/repo/pipelines", []string{"pipeline:read"}},
		{"api pipeline write", auth.TokenAPI, http.MethodPost, "/repositories/team/repo/pipelines", []string{"write:pipeline:bitbucket"}},
		{"access pipeline write", auth.TokenAccess, http.MethodPost, "/repositories/team/repo/pipelines", []string{"pipeline:write"}},
		{"api webhook delete", auth.TokenAPI, http.MethodDelete, "/repositories/team/repo/hooks/1", []string{"delete:webhook:bitbucket"}},
		{"access webhook delete", auth.TokenAccess, http.MethodDelete, "/repositories/team/repo/hooks/1", []string{"webhook:delete"}},
		{"oauth webhook delete", auth.TokenOAuth, http.MethodDelete, "/repositories/team/repo/hooks/1", []string{"webhook:write"}},
		{"api ssh key read", auth.TokenAPI, http.MethodGet, "/users/u/ssh-keys", []string{"read:ssh-key:bitbucket"}},
		{"access ssh key read", auth.TokenAccess, http.MethodGet, "/users/u/ssh-keys", []string{"account:read"}},
		{"api deploy key read", auth.TokenAPI, http.MethodGet, "/repositories/team/repo/deploy-keys", []string{"admin:repository:bitbucket"}},
		{"access deploy key read", auth.TokenAccess, http.MethodGet, "/repositories/team/repo/deploy-keys", []string{"repository:admin"}},
		{"api project read", auth.TokenAPI, http.MethodGet, "/workspaces/team/projects/p1", []string{"read:project:bitbucket"}},
		{"access project read", auth.TokenAccess, http.MethodGet, "/workspaces/team/projects/p1", []string{"project:read"}},
		{"api workspace read", auth.TokenAPI, http.MethodGet, "/workspaces/team/members", []string{"read:workspace:bitbucket"}},
		{"access workspace read", auth.TokenAccess, http.MethodGet, "/workspaces/team/members", []string{"workspace:read"}},
		{"api permission write", auth.TokenAPI, http.MethodPut, "/repositories/team/repo/permissions-config/u", []string{"admin:repository:bitbucket", "write:permission:bitbucket"}},
		{"access permission write", auth.TokenAccess, http.MethodPut, "/repositories/team/repo/permissions-config/u", []string{"repository:admin"}},
		{"unknown kind", auth.TokenType("custom"), http.MethodGet, "/repositories/team/repo", nil},
		{"public endpoint", auth.TokenAPI, http.MethodGet, "/hook_events/repository", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequiredScopesFor(tt.kind, tt.method, tt.path)
			if !reflect.DeepEqual(got.Scopes, tt.want) {
				t.Fatalf("scopes = %#v, want %#v", got.Scopes, tt.want)
			}
			if got.CredentialKind != tt.kind {
				t.Fatalf("credential kind = %q, want %q", got.CredentialKind, tt.kind)
			}
		})
	}
}
