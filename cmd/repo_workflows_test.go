package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/config"
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
	if method != http.MethodPost || path != "/2.0/repositories/team/new-repo" {
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

func TestRepoViewReadmeFindsMarkdown(t *testing.T) {
	var paths []string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if strings.TrimSuffix(r.URL.Path, "/") == "/2.0/repositories/team/repo/src/main" {
			return jsonResp(200, `{"values":[{"path":"README.md","type":"commit_file"},{"path":"README.rst","type":"commit_file"}]}`), nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("# README\n")), Header: http.Header{}}, nil
	}, "repo", "view", "team/repo", "--readme", "--branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 || strings.TrimSuffix(paths[0], "/") != "/2.0/repositories/team/repo/src/main" || !strings.HasSuffix(paths[1], "/src/main/README.md") {
		t.Fatalf("unexpected README requests: %v", paths)
	}
	if out != "# README\n" {
		t.Fatalf("unexpected README: %q", out)
	}
}

func TestRepoForkUsesCanonicalIdentity(t *testing.T) {
	var postPath string
	var gotClone string
	old := gitClone
	gitClone = func(_ context.Context, args ...string) error { gotClone = args[0]; return nil }
	t.Cleanup(func() { gitClone = old })
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			postPath = r.URL.Path
			return jsonResp(201, `{"slug":"fork-name","name":"Fork Name","full_name":"target/fork-name","workspace":{"slug":"target"},"links":{"clone":[{"name":"https","href":"https://bitbucket.org/target/fork-name.git"}]}}`), nil
		}
		return jsonResp(500, `{}`), nil
	}, "repo", "fork", "team/source", "--workspace", "target", "--name", "Fork Name", "--clone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if postPath != "/2.0/repositories/team/source/forks" {
		t.Fatalf("unexpected fork path: %s", postPath)
	}
	if gotClone != "https://bitbucket.org/target/fork-name.git" {
		t.Fatalf("clone used non-canonical URL: %s", gotClone)
	}
}

func TestRepoCloneUsesConfiguredProtocol(t *testing.T) {
	t.Setenv("BITBUCKET_CLONE_PROTOCOL", "ssh")
	var gotClone string
	old := gitClone
	gitClone = func(_ context.Context, args ...string) error { gotClone = args[0]; return nil }
	t.Cleanup(func() { gitClone = old })
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"links":{"clone":[{"name":"ssh","href":"git@bitbucket.org:team/repo.git"}]}}`), nil
	}, "repo", "clone", "team/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotClone != "git@bitbucket.org:team/repo.git" {
		t.Fatalf("unexpected configured clone URL: %s", gotClone)
	}
}

func TestRepoCreateSourceFailureReturnsPartialSuccess(t *testing.T) {
	called := 0
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		called++
		return jsonResp(201, `{"full_name":"team/new-repo","name":"new-repo"}`), nil
	}, "repo", "create", "new-repo", "--workspace", "team", "--source", "/does/not/exist")
	if err == nil || called != 1 {
		t.Fatalf("expected partial source failure, err=%v calls=%d", err, called)
	}
	if !strings.Contains(out, "team/new-repo") {
		t.Fatalf("partial success missing repository: %s", out)
	}
}

func TestRepoBrowseBuildsPathURL(t *testing.T) {
	var got string
	old := openBrowser
	openBrowser = func(target string) error { got = target; return nil }
	t.Cleanup(func() { openBrowser = old })
	_, err := run(t, nil, "repo", "browse", "team/repo", "src/main.go", "--branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://bitbucket.org/team/repo/src/main/src/main.go" {
		t.Fatalf("unexpected browse URL: %s", got)
	}
}

type repoWorkflowGit struct {
	remotes []Remote
	added   []string
	renamed []string
}

func (g *repoWorkflowGit) Root(context.Context) (string, error)                   { return "/tmp", nil }
func (g *repoWorkflowGit) CurrentBranch(context.Context) (string, error)          { return "main", nil }
func (g *repoWorkflowGit) StatusPorcelain(context.Context) ([]byte, error)        { return nil, nil }
func (g *repoWorkflowGit) Remotes(context.Context) ([]Remote, error)              { return g.remotes, nil }
func (g *repoWorkflowGit) Fetch(context.Context, string, string, ...string) error { return nil }
func (g *repoWorkflowGit) Checkout(context.Context, ...string) error              { return nil }
func (g *repoWorkflowGit) AddRemote(_ context.Context, name, remoteURL string) error {
	g.added = append(g.added, name+"="+remoteURL)
	return nil
}
func (g *repoWorkflowGit) RenameRemote(_ context.Context, oldName, newName string) error {
	g.renamed = append(g.renamed, oldName+"="+newName)
	return nil
}

func TestRepoForkRemoteRejectsUpstreamCollision(t *testing.T) {
	old := gitRunner
	fake := &repoWorkflowGit{remotes: []Remote{{Name: "origin"}, {Name: "upstream"}}}
	gitRunner = fake
	t.Cleanup(func() { gitRunner = old })
	if err := configureForkRemote(context.Background(), config.ResolvedRepoRef{Workspace: "target", RepoSlug: "fork"}, "origin", true); err == nil || !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("expected upstream collision, got %v", err)
	}
}

func TestRepoForkRemoteRenamesOriginAfterConfirmation(t *testing.T) {
	old := gitRunner
	fake := &repoWorkflowGit{remotes: []Remote{{Name: "origin"}}}
	gitRunner = fake
	t.Cleanup(func() { gitRunner = old })
	if err := configureForkRemote(context.Background(), config.ResolvedRepoRef{Workspace: "target", RepoSlug: "fork"}, "origin", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(fake.renamed, ",") != "origin=upstream" || len(fake.added) != 1 || !strings.Contains(fake.added[0], "target/fork.git") {
		t.Fatalf("unexpected remote changes: renamed=%v added=%v", fake.renamed, fake.added)
	}
}

func TestRepoLocalDefaultTakesPrecedence(t *testing.T) {
	oldReader := localDefaultReader
	oldWorkspace, oldRepo, oldRepository := flagWorkspace, flagRepo, flagRepository
	localDefaultReader = func() (string, bool) { return "local/repo", true }
	flagWorkspace, flagRepo, flagRepository = "", "", ""
	t.Cleanup(func() {
		localDefaultReader = oldReader
		flagWorkspace, flagRepo, flagRepository = oldWorkspace, oldRepo, oldRepository
	})
	ref, _, err := resolveRepo(config.Config{DefaultWorkspace: "global", DefaultRepo: "repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Workspace != "local" || ref.RepoSlug != "repo" {
		t.Fatalf("unexpected local default: %+v", ref)
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
