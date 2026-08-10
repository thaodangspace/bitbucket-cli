package access

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

type requestCall struct {
	method string
	path   string
	body   map[string]any
	query  url.Values
}

type fakeClient struct {
	calls         []requestCall
	paginateCalls []requestCall
	paginateAll   []string
	pageValues    []json.RawMessage
	eventCatalog  any
	requestErrAt  int
	paginateErr   error
}

func (f *fakeClient) failRequest(callNumber int) error {
	if f.requestErrAt == callNumber {
		return errors.New("request failed")
	}
	return nil
}

func (f *fakeClient) Request(_ context.Context, path string, opts bitbucket.RequestOptions, out any) error {
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}
	body, _ := opts.Body.(map[string]any)
	f.calls = append(f.calls, requestCall{method: method, path: path, body: body, query: opts.Query})
	if err := f.failRequest(len(f.calls)); err != nil {
		return err
	}
	var value any = map[string]any{"description": "old", "url": "https://old.example/hook", "events": []any{"repo:push"}, "active": true}
	if strings.HasPrefix(path, "/hook_events/") && f.eventCatalog != nil {
		value = f.eventCatalog
	} else if method != http.MethodGet {
		value = map[string]any{"uuid": "{hook-1}", "ok": true}
	}
	if out != nil {
		data, _ := json.Marshal(value)
		return json.Unmarshal(data, out)
	}
	return nil
}

func (f *fakeClient) Paginate(_ context.Context, path string, limit, maxPages int) ([]json.RawMessage, error) {
	f.paginateCalls = append(f.paginateCalls, requestCall{path: path, query: url.Values{"limit": {fmt.Sprint(limit)}, "maxPages": {fmt.Sprint(maxPages)}}})
	return f.pageValues, f.paginateErr
}
func (f *fakeClient) PaginateAll(_ context.Context, path string, _ int) ([]json.RawMessage, error) {
	f.paginateAll = append(f.paginateAll, path)
	return f.pageValues, f.paginateErr
}

func TestResolveWebhookTarget(t *testing.T) {
	target, err := ResolveWebhookTarget("team/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Subject != "repository" || target.Path != "/repositories/team/repo/hooks" || target.Label != "team/repo" {
		t.Fatalf("unexpected target: %+v", target)
	}
	if _, err := ResolveWebhookTarget("team/repo", "team"); err == nil {
		t.Fatal("expected conflicting target selectors to fail")
	}
}

func TestEditWebhookReadModifyWrite(t *testing.T) {
	client := &fakeClient{}
	service := New(client)
	description := "new"
	_, err := service.EditWebhook(context.Background(), WebhookTarget{Path: "/repositories/team/repo/hooks"}, "{hook-1}", WebhookEdit{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 || client.calls[0].method != http.MethodGet || client.calls[1].method != http.MethodPut {
		t.Fatalf("expected read-modify-write, got %+v", client.calls)
	}
	body := client.calls[1].body
	if body["description"] != "new" || body["url"] != "https://old.example/hook" || body["active"] != true {
		t.Fatalf("edit did not preserve current fields: %#v", body)
	}
}

func TestValidateWebhookEventsUsesServiceCatalogAndCache(t *testing.T) {
	client := &fakeClient{eventCatalog: map[string]any{"values": []any{"repo:push", "pullrequest:created"}}}
	service := New(client)
	cache := &testEventCache{}
	got, err := ValidateWebhookEvents(context.Background(), service, "repository", []string{"repo:push", "repo:push"}, false, EventCatalogOptions{Cache: cache})
	if err != nil || strings.Join(got, ",") != "repo:push" {
		t.Fatalf("unexpected validation result: %v, %v", got, err)
	}
	if len(client.calls) != 1 || !cache.put {
		t.Fatalf("expected catalog request and cache write: calls=%+v cache=%+v", client.calls, cache)
	}
	if _, err := ValidateWebhookEvents(context.Background(), service, "repository", []string{"repo:puh"}, false, EventCatalogOptions{Cache: cache}); err == nil || !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("expected suggestion error, got %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatal("expected validation to read the cached catalog")
	}
}

type testEventCache struct {
	events []string
	put    bool
}

func (c *testEventCache) Get(string) ([]string, bool)   { return c.events, len(c.events) > 0 }
func (c *testEventCache) Put(_ string, events []string) { c.events, c.put = events, true }

func TestParsePublicKeyNormalizesMaterial(t *testing.T) {
	blob := make([]byte, 4+len("ssh-ed25519")+4)
	blob[3] = byte(len("ssh-ed25519"))
	copy(blob[4:], "ssh-ed25519")
	key := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " ci"
	parsed, err := ParsePublicKey([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Algorithm != "ssh-ed25519" || parsed.Fingerprint == "" || !strings.HasPrefix(parsed.Material, "ssh-ed25519 ") {
		t.Fatalf("unexpected parsed key: %+v", parsed)
	}
	if _, err := ParsePublicKey([]byte("-----BEGIN OPENSSH PRIVATE KEY-----")); err == nil {
		t.Fatal("expected private key rejection")
	}
}
