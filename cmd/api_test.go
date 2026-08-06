package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thaodangspace/bitbucket-cli/auth"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

// runStdin is run() but with an explicit stdin stream.
func runStdin(t *testing.T, transport roundTripFunc, stdin string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BITBUCKET_EMAIL", "dev@example.com")
	t.Setenv("BITBUCKET_API_TOKEN", "token")
	t.Setenv("BITBUCKET_DEFAULT_WORKSPACE", "team")
	t.Setenv("BITBUCKET_DEFAULT_REPO", "repo")
	t.Setenv("BITBUCKET_CONFIG", t.TempDir()+"/bitbucket-cli.yaml")
	auth.SetGlobalStore(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetGlobalStore(nil) })

	flagWorkspace, flagRepo, flagPretty = "", "", false
	flagLoginEmail, flagLoginWithToken, flagLoginTokenType = "", false, string(auth.TokenAPI)
	flagStatusJSON, flagLogoutYes = false, false
	resetFlags(rootCmd)
	attachFiles, attachMessage = nil, ""
	apiMethod = ""
	apiHeaders, apiRawField, apiField = nil, nil, nil
	apiInput, apiOutput, apiJQ, apiTemplate, apiCache = "", "", "", "", ""
	apiPaginate, apiSlurp, apiInclude, apiSilent = false, false, false, false
	apiCacheStore.m = map[string]apiCacheEntry{}
	testTransport = transport
	t.Cleanup(func() { testTransport = nil })

	origOut, origIn := os.Stdout, os.Stdin
	rOut, wOut, _ := os.Pipe()
	rIn, wIn, _ := os.Pipe()
	os.Stdout = wOut
	os.Stdin = rIn
	_, _ = wIn.WriteString(stdin)
	wIn.Close()

	rootCmd.SetArgs(args)
	err := rootCmd.Execute()

	wOut.Close()
	os.Stdout = origOut
	os.Stdin = origIn
	out, _ := io.ReadAll(rOut)
	return string(out), err
}

func TestAPIGetJSON(t *testing.T) {
	var gotURL string
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{"id":1,"display_name":"dev"}`), nil
	}, "api", "/user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != "https://api.bitbucket.org/2.0/user" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if m["id"] != float64(1) {
		t.Fatalf("unexpected body: %v", m)
	}
}

func TestAPIGetFieldsGoToQuery(t *testing.T) {
	var gotURL, gotMethod string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL, gotMethod = r.URL.String(), r.Method
		return jsonResp(200, `{}`), nil
	}, "api", "/user", "-f", "limit=5", "-F", "admin=true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", gotMethod)
	}
	if !strings.Contains(gotURL, "limit=5") || !strings.Contains(gotURL, "admin=true") {
		t.Fatalf("query missing fields: %s", gotURL)
	}
}

func TestAPIPostNestedFieldBody(t *testing.T) {
	var gotMethod string
	var gotBody []byte
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("expected JSON content type, got %q", ct)
		}
		return jsonResp(201, `{}`), nil
	}, "api", "/repositories/team/repo/pipelines",
		"-X", "POST",
		"-F", "target[ref_type]=branch",
		"-F", "target[ref_name]=main",
		"-F", "variables[]=a",
		"-F", "variables[]=b",
		"-F", "enabled=true",
		"-F", "count=3",
		"-F", "empty=null",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("body not json: %v\n%s", err, gotBody)
	}
	target := sent["target"].(map[string]any)
	if target["ref_type"] != "branch" || target["ref_name"] != "main" {
		t.Fatalf("unexpected nested body: %s", gotBody)
	}
	vars := sent["variables"].([]any)
	if len(vars) != 2 || vars[0] != "a" || vars[1] != "b" {
		t.Fatalf("unexpected array body: %s", gotBody)
	}
	if sent["enabled"] != true || sent["count"] != float64(3) {
		t.Fatalf("typed values wrong: %s", gotBody)
	}
	if sent["empty"] != nil {
		t.Fatalf("null value wrong: %s", gotBody)
	}
}

func TestAPIRawFieldAlwaysString(t *testing.T) {
	var gotBody []byte
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotBody, _ = io.ReadAll(r.Body)
		return jsonResp(201, `{}`), nil
	}, "api", "/x", "-X", "POST", "-f", "enabled=true", "-f", "count=3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(gotBody, &sent)
	if sent["enabled"] != "true" || sent["count"] != "3" {
		t.Fatalf("-f must send strings, got: %s", gotBody)
	}
}

func TestAPIInputFileRawBody(t *testing.T) {
	dir := t.TempDir()
	inputPath := dir + "/body.json"
	if err := os.WriteFile(inputPath, []byte(`{"title":"raw"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotURL string
	var gotBody []byte
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotBody, _ = io.ReadAll(r.Body)
		return jsonResp(201, `{}`), nil
	}, "api", "/repositories/team/repo/pullrequests", "-X", "POST",
		"--input", inputPath, "-f", "extra=1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(gotBody) != `{"title":"raw"}` {
		t.Fatalf("unexpected raw body: %s", gotBody)
	}
	if !strings.Contains(gotURL, "extra=1") {
		t.Fatalf("--input must move fields to query: %s", gotURL)
	}
}

func TestAPIInputStdinRawBody(t *testing.T) {
	var gotBody []byte
	_, err := runStdin(t, func(r *http.Request) (*http.Response, error) {
		gotBody, _ = io.ReadAll(r.Body)
		return jsonResp(201, `{}`), nil
	}, `{"title":"stdin"}`, "api", "/repositories/team/repo/pullrequests", "-X", "POST", "--input", "-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(gotBody) != `{"title":"stdin"}` {
		t.Fatalf("unexpected stdin body: %s", gotBody)
	}
}

func TestAPIFieldValueFromFile(t *testing.T) {
	dir := t.TempDir()
	descPath := dir + "/desc.md"
	if err := os.WriteFile(descPath, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotBody []byte
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotBody, _ = io.ReadAll(r.Body)
		return jsonResp(201, `{}`), nil
	}, "api", "/repositories/team/repo/pullrequests", "-X", "POST", "-f", "description=@"+descPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(gotBody, &sent)
	if sent["description"] != "hello world" {
		t.Fatalf("unexpected @file value: %s", gotBody)
	}
}

func TestAPIHeaderFlag(t *testing.T) {
	var gotAccept string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotAccept = r.Header.Get("Accept")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("diff")), Header: http.Header{"Content-Type": {"text/plain"}}}, nil
	}, "api", "/repositories/team/repo/pullrequests/42/diff", "-H", "Accept: text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccept != "text/plain" {
		t.Fatalf("expected custom Accept, got %q", gotAccept)
	}
}

func TestAPITextStreaming(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("line1\nline2\n")), Header: http.Header{"Content-Type": {"text/plain"}}}, nil
	}, "api", "/repositories/team/repo/pullrequests/42/diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "line1\nline2\n" {
		t.Fatalf("expected raw stream, got %q", out)
	}
}

func TestAPIInclude(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"ok":true}`), nil
	}, "api", "/user", "-i")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "HTTP/1.1 200 OK\n") {
		t.Fatalf("missing status line: %q", out)
	}
	if !strings.Contains(out, "Content-Type: application/json\n\n") {
		t.Fatalf("missing header block: %q", out)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Fatalf("missing body: %q", out)
	}
}

func TestAPISilent(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"ok":true}`), nil
	}, "api", "/user", "--silent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Fatalf("expected no output, got %q", out)
	}
}

func TestAPIOutputFileBinary(t *testing.T) {
	dir := t.TempDir()
	outPath := dir + "/out.diff"
	blob := "\x00\x01binary\xffdata"
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(blob)), Header: http.Header{"Content-Type": {"application/octet-stream"}}}, nil
	}, "api", "/repositories/team/repo/pullrequests/42/diff", "--output", outPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != blob {
		t.Fatalf("binary output corrupted: %q", data)
	}
}

func TestAPIEmpty204(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	}, "api", "/repositories/team/repo/pullrequests/1/approve", "-X", "POST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestAPIPagination(t *testing.T) {
	var requests int
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		requests++
		if strings.Contains(r.URL.String(), "page=2") {
			return jsonResp(200, `{"values":[{"id":2}],"next":""}`), nil
		}
		return jsonResp(200, `{"values":[{"id":1}],"next":"https://api.bitbucket.org/2.0/user?page=2"}`), nil
	}, "api", "/user", "--paginate", "--slurp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	var pages []map[string]any
	if err := json.Unmarshal([]byte(out), &pages); err != nil {
		t.Fatalf("not json array: %v\n%s", err, out)
	}
	if len(pages) != 2 || pages[0]["values"].([]any)[0].(map[string]any)["id"] != float64(1) || pages[1]["values"].([]any)[0].(map[string]any)["id"] != float64(2) {
		t.Fatalf("unexpected paginated result: %v", pages)
	}
}

func TestAPIPaginationSlurpJQ(t *testing.T) {
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.String(), "page=2") {
			return jsonResp(200, `{"values":[3],"next":""}`), nil
		}
		return jsonResp(200, `{"values":[1,2],"next":"https://api.bitbucket.org/2.0/user?page=2"}`), nil
	}, "api", "/user", "--paginate", "--slurp", "--jq", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIPaginationWithoutSlurpJQPerItem(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.String(), "page=2") {
			return jsonResp(200, `{"values":[3],"next":""}`), nil
		}
		return jsonResp(200, `{"values":[1,2],"next":"https://api.bitbucket.org/2.0/user?page=2"}`), nil
	}, "api", "/user", "--paginate", "--jq", ".values[]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var lines []int
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var n int
		if err := dec.Decode(&n); err != nil {
			break
		}
		lines = append(lines, n)
	}
	if len(lines) != 3 || lines[0] != 1 || lines[1] != 2 || lines[2] != 3 {
		t.Fatalf("expected per-item jq output, got %q", out)
	}
}

func TestAPIPaginationLoopDetected(t *testing.T) {
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"values":[],"next":"https://api.bitbucket.org/2.0/user?page=2"}`), nil
	}, "api", "/user", "--paginate")
	if err == nil {
		t.Fatal("expected pagination loop error")
	}
	if !strings.Contains(err.Error(), "pagination loop") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPINon2xxError(t *testing.T) {
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(404, `{"error":{"message":"nope"}}`), nil
	}, "api", "/repositories/team/repo/pullrequests/999")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *bitbucket.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if he.Status != 404 {
		t.Fatalf("expected 404, got %d", he.Status)
	}
}

func TestAPIRedactsTokenInErrorExcerpt(t *testing.T) {
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(403, `{"error":{"message":"token is invalid"}}`), nil
	}, "api", "/repositories/team/repo/pullrequests/1")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *bitbucket.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if strings.Contains(he.Excerpt, "token") {
		t.Fatalf("token leaked in excerpt: %q", he.Excerpt)
	}
	if !strings.Contains(he.Excerpt, "<redacted>") {
		t.Fatalf("expected redaction marker: %q", he.Excerpt)
	}
}

func TestAPICancellation(t *testing.T) {
	t.Setenv("BITBUCKET_EMAIL", "dev@example.com")
	t.Setenv("BITBUCKET_API_TOKEN", "token")
	t.Setenv("BITBUCKET_CONFIG", t.TempDir()+"/bitbucket-cli.yaml")
	auth.SetGlobalStore(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetGlobalStore(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1)
	testTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	t.Cleanup(func() { testTransport = nil })

	resetFlags(rootCmd)
	apiMethod = ""
	apiHeaders, apiRawField, apiField = nil, nil, nil
	apiInput, apiOutput, apiJQ, apiTemplate, apiCache = "", "", "", "", ""
	apiPaginate, apiSlurp, apiInclude, apiSilent = false, false, false, false

	done := make(chan error, 1)
	rootCmd.SetArgs([]string{"api", "/user"})
	go func() {
		done <- rootCmd.ExecuteContext(ctx)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never started")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
		if !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return after cancel")
	}
}

func TestAPICacheHitsSecondRequest(t *testing.T) {
	t.Setenv("BITBUCKET_EMAIL", "dev@example.com")
	t.Setenv("BITBUCKET_API_TOKEN", "token")
	t.Setenv("BITBUCKET_DEFAULT_WORKSPACE", "team")
	t.Setenv("BITBUCKET_DEFAULT_REPO", "repo")
	t.Setenv("BITBUCKET_CONFIG", t.TempDir()+"/bitbucket-cli.yaml")
	auth.SetGlobalStore(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetGlobalStore(nil) })

	requests := 0
	testTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return jsonResp(200, `{"id":1}`), nil
	})
	t.Cleanup(func() { testTransport = nil })

	for i := 0; i < 2; i++ {
		resetFlags(rootCmd)
		apiMethod = ""
		apiHeaders, apiRawField, apiField = nil, nil, nil
		apiInput, apiOutput, apiJQ, apiTemplate, apiCache = "", "", "", "", ""
		apiPaginate, apiSlurp, apiInclude, apiSilent = false, false, false, false
		rootCmd.SetArgs([]string{"api", "/user", "--cache", "5m"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if requests != 1 {
		t.Fatalf("expected 1 request with cache, got %d", requests)
	}
}

func TestAPIJQSubset(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"a":{"b":[1,2,3]},"name":"dev"}`), nil
	}, "api", "/user", "--jq", ".a.b[1]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "2" {
		t.Fatalf("unexpected jq result: %q", out)
	}
}

func TestAPIJQIterate(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"a":{"b":[1,2,3]}}`), nil
	}, "api", "/user", "--jq", ".a.b[]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var vals []float64
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var n float64
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			t.Fatalf("bad line %q", line)
		}
		vals = append(vals, n)
	}
	if len(vals) != 3 || vals[0] != 1 || vals[2] != 3 {
		t.Fatalf("unexpected iterate result: %q", out)
	}
}

func TestAPIJQRequiresJSON(t *testing.T) {
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("text")), Header: http.Header{"Content-Type": {"text/plain"}}}, nil
	}, "api", "/x", "--jq", ".a")
	if err == nil {
		t.Fatal("expected error for jq on non-JSON response")
	}
}

func TestAPITemplate(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"name":"dev","tags":["a","b"]}`), nil
	}, "api", "/user", "--template", "{{.name}}:{{range .tags}}{{.}}{{end}}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "dev:ab" {
		t.Fatalf("unexpected template result: %q", out)
	}
}

func TestAPIJQAndTemplateConflict(t *testing.T) {
	_, err := run(t, nil, "api", "/user", "--jq", ".a", "--template", "{{.}}")
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestAPIEndpointPlaceholders(t *testing.T) {
	var gotURL string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{}`), nil
	}, "api", "/repositories/{workspace}/{repo}/pullrequests?state=OPEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != "https://api.bitbucket.org/2.0/repositories/team/repo/pullrequests?state=OPEN" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
}

func TestAPIExternalHostRejected(t *testing.T) {
	_, err := run(t, nil, "api", "https://example.com/evil")
	if err == nil {
		t.Fatal("expected host rejection error")
	}
	if !strings.Contains(err.Error(), "not the configured Bitbucket API host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIAbsoluteURLAllowed(t *testing.T) {
	var gotURL string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResp(200, `{}`), nil
	}, "api", "https://api.bitbucket.org/2.0/user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURL != "https://api.bitbucket.org/2.0/user" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
}

func TestAPIMethodPutAndDelete(t *testing.T) {
	var methods []string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		return jsonResp(200, `{}`), nil
	}, "api", "/repositories/team/repo/pullrequests/1", "-X", "PUT", "-f", "title=x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = run(t, func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		return jsonResp(200, `{}`), nil
	}, "api", "/repositories/team/repo/pullrequests/1/approve", "-X", "DELETE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodPut || methods[1] != http.MethodDelete {
		t.Fatalf("unexpected methods: %v", methods)
	}
}

func TestAPIInvalidField(t *testing.T) {
	_, err := run(t, nil, "api", "/user", "-f", "novalue")
	if err == nil {
		t.Fatal("expected error for malformed field")
	}
}

func TestAPIInvalidMethod(t *testing.T) {
	_, err := run(t, nil, "api", "/user", "-X", "TRACE")
	if err == nil {
		t.Fatal("expected error for unsupported method")
	}
}
