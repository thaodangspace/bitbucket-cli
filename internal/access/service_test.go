package access

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

type requestCall struct {
	method string
	path   string
	body   map[string]any
}

type fakeClient struct {
	calls []requestCall
}

func (f *fakeClient) Request(_ context.Context, path string, opts bitbucket.RequestOptions, out any) error {
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}
	body, _ := opts.Body.(map[string]any)
	f.calls = append(f.calls, requestCall{method: method, path: path, body: body})
	var value any = map[string]any{"description": "old", "url": "https://old.example/hook", "events": []any{"repo:push"}, "active": true}
	if method != http.MethodGet {
		value = map[string]any{"uuid": "{hook-1}", "ok": true}
	}
	if out != nil {
		data, _ := json.Marshal(value)
		return json.Unmarshal(data, out)
	}
	return nil
}

func (f *fakeClient) Paginate(context.Context, string, int, int) ([]json.RawMessage, error) {
	return nil, nil
}
func (f *fakeClient) PaginateAll(context.Context, string, int) ([]json.RawMessage, error) {
	return nil, nil
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
