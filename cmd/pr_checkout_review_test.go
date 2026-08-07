package cmd

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/selector"
)

func TestChooseRemoteUsesFetchURLOnly(t *testing.T) {
	git := &checkoutGitFake{}
	withCheckoutGit(t, git)
	remote, created, err := chooseRemote(context.Background(), prSource{}, selector.Repository{Workspace: "fork", Repo: "repo"}, selector.Repository{Workspace: "team", Repo: "repo"}, []Remote{{Name: "origin", FetchURL: "https://bitbucket.org/team/repo.git", PushURL: "https://bitbucket.org/fork/repo.git"}})
	if err != nil {
		t.Fatal(err)
	}
	if !created || remote.Name != "fork" || len(git.added) != 1 {
		t.Fatalf("expected a new fork remote, remote=%+v created=%v added=%v", remote, created, git.added)
	}
}

func TestChooseRemoteResolvesNameCollision(t *testing.T) {
	git := &checkoutGitFake{}
	withCheckoutGit(t, git)
	remote, created, err := chooseRemote(context.Background(), prSource{}, selector.Repository{Workspace: "contributor", Repo: "repo"}, selector.Repository{Workspace: "team", Repo: "repo"}, []Remote{
		{Name: "contributor", FetchURL: "https://bitbucket.org/team/repo.git"},
		{Name: "contributor-2", FetchURL: "https://bitbucket.org/other/repo.git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || remote.Name != "contributor-3" || !strings.HasPrefix(git.added[0], "contributor-3=") {
		t.Fatalf("unexpected collision resolution: remote=%+v added=%v", remote, git.added)
	}
}

func TestPRCheckoutDetachUpdatesSubmodules(t *testing.T) {
	git := &checkoutGitFake{remotes: []Remote{{Name: "origin", FetchURL: "https://bitbucket.org/team/repo.git"}}}
	withCheckoutGit(t, git)
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, checkoutPRJSON), nil
	}, "pr", "checkout", "9", "--detach", "--recurse-submodules")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(git.checks) != 1 || git.checks[0] != "--detach abcdef1234567" || git.submoduleRuns != 1 {
		t.Fatalf("unexpected detach/submodule operations: checks=%v submodules=%d", git.checks, git.submoduleRuns)
	}
}

func TestPRCheckoutUpdatesExistingBranchWithoutSourceCommit(t *testing.T) {
	git := &checkoutGitFake{
		exists: true, currentCommit: "1111111111111", targetCommit: "2222222222222",
		ancestor: true, remotes: []Remote{{Name: "origin", FetchURL: "https://bitbucket.org/team/repo.git"}},
	}
	withCheckoutGit(t, git)
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, strings.Replace(checkoutPRJSON, `,"commit":{"hash":"abcdef1234567"}`, "", 1)), nil
	}, "pr", "checkout", "9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(git.checks) != 1 || git.checks[0] != "-B feature/login 2222222222222" {
		t.Fatalf("existing branch was not updated to fetched commit: %v", git.checks)
	}
}

func TestPRViewExplicitBranchKeepsMainOpenPreference(t *testing.T) {
	withCheckoutGit(t, &checkoutGitFake{})
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/2.0/repositories/team/repo/pullrequests":
			return jsonResp(200, `{"values":[{"id":1,"title":"Historical","state":"DECLINED","destination":{"branch":{"name":"main"}}},{"id":2,"title":"Current","state":"OPEN","destination":{"branch":{"name":"main"}}}]}`), nil
		case "/2.0/repositories/team/repo":
			return jsonResp(200, `{"mainbranch":{"name":"main"}}`), nil
		default:
			return jsonResp(200, `{"id":2,"title":"Current","state":"OPEN"}`), nil
		}
	}, "pr", "view", "feature/login")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"id": 2`) {
		t.Fatalf("explicit branch selected wrong PR: %s", out)
	}
}
