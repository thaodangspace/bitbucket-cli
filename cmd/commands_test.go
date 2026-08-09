package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/thaodangspace/bitbucket-cli/auth"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// resetFlags clears flag values and their `Changed` markers across the command
// tree. cobra reuses the rootCmd singleton between Execute() calls, so flag
// state otherwise leaks from one test into the next.
func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	cmd.SetContext(nil)
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// run executes the root command with args, capturing stdout, sandboxed to a
// fresh temp config path. The stub transport (may be nil) handles HTTP and can
// record the last request.
func run(t *testing.T, transport roundTripFunc, args ...string) (string, error) {
	t.Helper()
	return runAt(t, transport, t.TempDir()+"/bitbucket-cli.yaml", args...)
}

// runAt is run() but with an explicit BITBUCKET_CONFIG path, so tests can
// inspect or pre-populate a specific config file.
func runAt(t *testing.T, transport roundTripFunc, cfgPath string, args ...string) (string, error) {
	t.Helper()
	return runAtStore(t, auth.NewMemoryStore(), transport, cfgPath, args...)
}

func runAtStore(t *testing.T, store auth.SecretStore, transport roundTripFunc, cfgPath string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BITBUCKET_EMAIL", "dev@example.com")
	t.Setenv("BITBUCKET_API_TOKEN", "token")
	t.Setenv("BITBUCKET_DEFAULT_WORKSPACE", "team")
	t.Setenv("BITBUCKET_DEFAULT_REPO", "repo")
	// Never read the developer's real config or keychain in tests.
	t.Setenv("BITBUCKET_CONFIG", cfgPath)
	t.Setenv("BITBUCKET_CACHE_DIR", t.TempDir())
	auth.SetGlobalStore(store)
	t.Cleanup(func() { auth.SetGlobalStore(nil) })

	// Reset global flag state between runs.
	flagWorkspace, flagRepo, flagPretty = "", "", false
	flagLoginEmail, flagLoginWithToken, flagLoginTokenType = "", false, string(auth.TokenAPI)
	flagStatusJSON, flagLogoutYes = false, false
	resetFlags(rootCmd)
	attachFiles, attachMessage = nil, ""
	commitListIncludes, commitListExcludes = nil, nil
	apiMethod = ""
	apiHeaders, apiRawField, apiField = nil, nil, nil
	apiInput, apiOutput, apiJQ, apiTemplate, apiCache = "", "", "", "", ""
	apiPaginate, apiSlurp, apiInclude, apiSilent = false, false, false, false
	apiMaxPages = bitbucket.DefaultMaxPages
	apiCacheStore.m = map[string]apiCacheEntry{}
	testTransport = transport
	t.Cleanup(func() { testTransport = nil })

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs(args)
	err := rootCmd.Execute()

	w.Close()
	os.Stdout = origStdout
	out, _ := io.ReadAll(r)
	return string(out), err
}

func TestStatusJSON(t *testing.T) {
	out, err := run(t, nil, "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if m["configured"] != true || m["defaultRepo"] != "team/repo" {
		t.Fatalf("unexpected status: %v", m)
	}
}

func TestStatusMissingCredsFails(t *testing.T) {
	t.Setenv("BITBUCKET_CONFIG", t.TempDir()+"/missing.yaml")
	t.Setenv("BITBUCKET_EMAIL", "")
	t.Setenv("BITBUCKET_API_TOKEN", "token")
	t.Setenv("BITBUCKET_DEFAULT_WORKSPACE", "")
	t.Setenv("BITBUCKET_DEFAULT_REPO", "")

	flagWorkspace, flagRepo, flagPretty = "", "", false
	resetFlags(rootCmd)
	rootCmd.SetArgs([]string{"status"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error when credentials missing")
	}
}

func TestPRListPathAndQuery(t *testing.T) {
	var gotURL string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"values":[{"id":1,"title":"A","state":"OPEN"},{"id":2,"title":"B","state":"OPEN"}]}`), nil
	}, "pr", "list", "--state", "OPEN", "--limit", "5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotURL, "/repositories/team/repo/pullrequests") {
		t.Fatalf("unexpected path: %s", gotURL)
	}
	if !strings.Contains(gotURL, "state=OPEN") || !strings.Contains(gotURL, "pagelen=50") {
		t.Fatalf("query missing params: %s", gotURL)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("not json array: %v\n%s", err, out)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(arr))
	}
}

func TestPRListAuthorFilter(t *testing.T) {
	var gotURL string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"values":[{"id":1,"title":"A","state":"OPEN"}]}`), nil
	}, "pr", "list", "--author", "6352db651cc605b1fd15525a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotURL, `q=author.account_id%3D%226352db651cc605b1fd15525a%22`) {
		t.Fatalf("query missing author filter: %s", gotURL)
	}
}

func TestPRListMineResolvesUser(t *testing.T) {
	var paths []string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.String())
		if strings.HasSuffix(r.URL.Path, "/user") {
			return jsonResp(200, `{"account_id":"me-123"}`), nil
		}
		return jsonResp(200, `{"values":[{"id":1,"title":"A","state":"OPEN"}]}`), nil
	}, "pr", "list", "--mine")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sawUser, sawFilter bool
	for _, p := range paths {
		if strings.HasSuffix(p, "/user") {
			sawUser = true
		}
		if strings.Contains(p, `q=author.account_id%3D%22me-123%22`) {
			sawFilter = true
		}
	}
	if !sawUser {
		t.Fatalf("expected a /user call, got: %v", paths)
	}
	if !sawFilter {
		t.Fatalf("expected author filter with resolved account id, got: %v", paths)
	}
}

func TestPRListAuthorAndMineConflict(t *testing.T) {
	_, err := run(t, nil, "pr", "list", "--author", "x", "--mine")
	if err == nil {
		t.Fatal("expected error when both --author and --mine are set")
	}
}

func TestPRListPretty(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"values":[{"id":12,"title":"Fix","state":"OPEN"}]}`), nil
	}, "pr", "list", "--pretty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "#12 Fix [OPEN]" {
		t.Fatalf("unexpected pretty output: %q", out)
	}
}

func TestPRGetInvalidID(t *testing.T) {
	_, err := run(t, nil, "pr", "get", "abc")
	if err == nil {
		t.Fatal("expected error for non-numeric id")
	}
}

func TestRepoGet(t *testing.T) {
	var gotURL string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"full_name":"team/repo","name":"repo"}`), nil
	}, "repo", "get", "--pretty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != "https://api.bitbucket.org/2.0/repositories/team/repo" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	if strings.TrimSpace(out) != "Repository: team/repo" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestBranchListQuery(t *testing.T) {
	var gotURL string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"values":[{"name":"main"}]}`), nil
	}, "branch", "list", "--query", `name ~ "feature/"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotURL, "/refs/branches") || !strings.Contains(gotURL, "q=") {
		t.Fatalf("unexpected url: %s", gotURL)
	}
}

func TestPRCommentPostsBody(t *testing.T) {
	var gotURL, gotMethod string
	var gotBody []byte
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		return jsonResp(201, `{"id":99}`), nil
	}, "pr", "comment", "123", "--body", "Looks good", "--pretty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if !strings.Contains(gotURL, "/pullrequests/123/comments") {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	content := sent["content"].(map[string]any)
	if content["raw"] != "Looks good" {
		t.Fatalf("unexpected body: %s", gotBody)
	}
	if _, has := content["markup"]; has {
		t.Fatalf("comment body must not send content.markup: %s", gotBody)
	}
	if _, has := sent["parent"]; has {
		t.Fatalf("top-level comment must not send parent: %s", gotBody)
	}
	if strings.TrimSpace(out) != "Posted comment #99 on pull request #123." {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPRCommentReplyPostsParent(t *testing.T) {
	var gotURL, gotMethod string
	var gotBody []byte
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		return jsonResp(201, `{"id":100}`), nil
	}, "pr", "comment", "123", "--body", "Fixed now", "--reply-to", "456", "--pretty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if !strings.Contains(gotURL, "/pullrequests/123/comments") {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	content := sent["content"].(map[string]any)
	if content["raw"] != "Fixed now" {
		t.Fatalf("unexpected body: %s", gotBody)
	}
	parent := sent["parent"].(map[string]any)
	if parent["id"] != float64(456) {
		t.Fatalf("unexpected parent: %s", gotBody)
	}
	if strings.TrimSpace(out) != "Posted comment #100 on pull request #123." {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPRCommentInvalidReplyToFails(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(201, `{}`), nil
	}, "pr", "comment", "123", "--body", "Fixed now", "--reply-to", "0")
	if err == nil {
		t.Fatal("expected error for invalid reply-to")
	}
	if called {
		t.Fatal("should not call API for invalid reply-to")
	}
}

func TestPRCommentEmptyBodyFails(t *testing.T) {
	called := false
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(201, `{}`), nil
	}, "pr", "comment", "123", "--body", "   ")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if called {
		t.Fatal("should not call API for empty body")
	}
}
