package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type checkoutGitFake struct {
	branch   string
	dirty    []byte
	remotes  []Remote
	fetches  []string
	checks   []string
	added    []string
	upstream []string
}

func (g *checkoutGitFake) Root(context.Context) (string, error)            { return "/tmp/repo", nil }
func (g *checkoutGitFake) CurrentBranch(context.Context) (string, error)   { return g.branch, nil }
func (g *checkoutGitFake) StatusPorcelain(context.Context) ([]byte, error) { return g.dirty, nil }
func (g *checkoutGitFake) Remotes(context.Context) ([]Remote, error)       { return g.remotes, nil }
func (g *checkoutGitFake) Fetch(_ context.Context, remote, refspec string, args ...string) error {
	g.fetches = append(g.fetches, strings.Join(append(args, remote, refspec), " "))
	return nil
}
func (g *checkoutGitFake) Checkout(_ context.Context, args ...string) error {
	g.checks = append(g.checks, strings.Join(args, " "))
	return nil
}
func (g *checkoutGitFake) AddRemote(_ context.Context, name, remoteURL string) error {
	g.added = append(g.added, name+"="+remoteURL)
	return nil
}
func (g *checkoutGitFake) BranchStatus(context.Context, string) (bool, string, error) {
	return false, "", nil
}
func (g *checkoutGitFake) IsAncestor(context.Context, string, string) (bool, error) {
	return false, nil
}
func (g *checkoutGitFake) SetUpstream(_ context.Context, branch, upstream string) error {
	g.upstream = append(g.upstream, branch+"="+upstream)
	return nil
}
func (g *checkoutGitFake) UpdateSubmodules(context.Context) error                 { return nil }
func (g *checkoutGitFake) TrackingRemote(context.Context) (string, string, error) { return "", "", nil }

func withCheckoutGit(t *testing.T, git Git) {
	t.Helper()
	old := gitRunner
	gitRunner = git
	t.Cleanup(func() { gitRunner = old })
}

const checkoutPRJSON = `{"id":9,"title":"Checkout me","state":"OPEN","source":{"branch":{"name":"feature/login"},"commit":{"hash":"abcdef1234567"},"repository":{"full_name":"team/repo","links":{"clone":[{"name":"https","href":"https://bitbucket.org/team/repo.git"}]}}},"destination":{"branch":{"name":"main"}}}`

func TestPRCurrentResolvesTrackedBranch(t *testing.T) {
	withCheckoutGit(t, &checkoutGitFake{branch: "feature/login", remotes: []Remote{{Name: "origin", FetchURL: "https://bitbucket.org/team/repo.git"}}})
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/pullrequests") {
			return jsonResp(200, `{"values":[{"id":9,"title":"Checkout me","state":"OPEN"}]}`), nil
		}
		return jsonResp(200, checkoutPRJSON), nil
	}, "pr", "current")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(out), &value); err != nil || value["id"] != float64(9) {
		t.Fatalf("unexpected output: %s (%v)", out, err)
	}
}

func TestPRCheckoutFetchesBranchAndTracksIt(t *testing.T) {
	git := &checkoutGitFake{remotes: []Remote{{Name: "origin", FetchURL: "https://bitbucket.org/team/repo.git"}}}
	withCheckoutGit(t, git)
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, checkoutPRJSON), nil
	}, "pr", "checkout", "9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(git.fetches) != 1 || !strings.Contains(git.fetches[0], "refs/heads/feature/login:refs/remotes/origin/feature/login") {
		t.Fatalf("unexpected fetches: %v", git.fetches)
	}
	if len(git.checks) != 1 || git.checks[0] != "-b feature/login abcdef1234567" {
		t.Fatalf("unexpected checkout: %v", git.checks)
	}
	if len(git.upstream) != 1 {
		t.Fatalf("expected upstream configuration: %v", git.upstream)
	}
	if !strings.Contains(out, `"pull_request_id": 9`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPRCheckoutRefusesDirtyWorktree(t *testing.T) {
	git := &checkoutGitFake{dirty: []byte(" M file.go"), remotes: []Remote{{Name: "origin", FetchURL: "https://bitbucket.org/team/repo.git"}}}
	withCheckoutGit(t, git)
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, checkoutPRJSON), nil
	}, "pr", "checkout", "9")
	if err == nil || !strings.Contains(err.Error(), "dirty working tree") {
		t.Fatalf("expected dirty-tree refusal, got %v", err)
	}
	if len(git.fetches) != 0 {
		t.Fatalf("should not fetch before dirty check: %v", git.fetches)
	}
}
