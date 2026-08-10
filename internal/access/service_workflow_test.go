package access

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestResolveDeployTarget(t *testing.T) {
	path, err := ResolveDeployTarget("team/repo")
	if err != nil || path != "/repositories/team/repo/deploy-keys" {
		t.Fatalf("unexpected deploy target: %q, %v", path, err)
	}
	if _, err := ResolveDeployTarget("not-a-repository"); err == nil {
		t.Fatal("expected invalid deploy target to fail")
	}
}

func TestListServicesBuildPaginationPaths(t *testing.T) {
	client := &fakeClient{pageValues: []json.RawMessage{json.RawMessage(`{"uuid":"one"}`)}}
	service := New(client)
	ctx := context.Background()

	if _, err := service.ListWebhooks(ctx, WebhookTarget{Path: "/repositories/team/repo/hooks"}, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListSSHKeys(ctx, "{user}", 8); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListDeployKeys(ctx, "/repositories/team/repo/deploy-keys", 9); err != nil {
		t.Fatal(err)
	}
	if len(client.paginateCalls) != 3 {
		t.Fatalf("expected three pagination calls, got %+v", client.paginateCalls)
	}
	want := []string{
		"/repositories/team/repo/hooks?pagelen=50",
		"/users/%7Buser%7D/ssh-keys?pagelen=50",
		"/repositories/team/repo/deploy-keys?pagelen=50",
	}
	for i, path := range want {
		if client.paginateCalls[i].path != path {
			t.Errorf("pagination call %d path = %q, want %q", i, client.paginateCalls[i].path, path)
		}
	}
}

func TestWebhookCRUDUsesExpectedMethodsAndBodies(t *testing.T) {
	client := &fakeClient{}
	service := New(client)
	target := WebhookTarget{Path: "/workspaces/team/hooks"}
	ctx := context.Background()

	if _, err := service.CreateWebhook(ctx, target, WebhookCreate{Description: "CI", URL: "https://ci.example/hook", Events: []string{"repo:push"}, Active: true, Secret: "secret", SecretSet: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ViewWebhook(ctx, target, "{hook}"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteWebhook(ctx, target, "{hook}"); err != nil {
		t.Fatal(err)
	}
	if got := []string{client.calls[0].method, client.calls[1].method, client.calls[2].method}; strings.Join(got, ",") != "POST,GET,DELETE" {
		t.Fatalf("methods = %v", got)
	}
	if client.calls[0].path != target.Path || client.calls[1].path != target.Path+"/%7Bhook%7D" || client.calls[2].path != target.Path+"/%7Bhook%7D" {
		t.Fatalf("unexpected paths: %+v", client.calls)
	}
	if client.calls[0].body["secret"] != "secret" || client.calls[0].body["active"] != true {
		t.Fatalf("unexpected create body: %#v", client.calls[0].body)
	}
}

func TestApplyWebhooksPreservesUUIDLessCreateOrderAndBodies(t *testing.T) {
	client := &fakeClient{}
	service := New(client)
	specs := []WebhookSpec{
		{Description: "first", URL: "https://one.example/hook", Events: []string{"repo:push", "repo:push"}},
		{Description: "second", URL: "https://two.example/hook", Events: []string{"repo:commit"}},
		{UUID: "{existing}", Description: "updated", URL: "https://updated.example/hook", Events: []string{"repo:push"}},
	}
	operations := []WebhookOperation{
		{Action: "create", Description: "first", MatchURL: specs[0].URL},
		{Action: "create", Description: "second", MatchURL: specs[1].URL},
		{Action: "update", UUID: "{existing}", Description: "updated", MatchURL: specs[2].URL},
		{Action: "delete", UUID: "{removed}"},
	}
	result := service.ApplyWebhooks(context.Background(), WebhookTarget{Path: "/repositories/team/repo/hooks"}, specs, operations)
	if len(result["errors"].([]any)) != 0 || len(result["applied"].([]any)) != len(operations) {
		t.Fatalf("unexpected apply result: %#v", result)
	}
	if len(client.calls) != 4 {
		t.Fatalf("expected four calls, got %+v", client.calls)
	}
	for i := 0; i < 2; i++ {
		if client.calls[i].method != http.MethodPost || client.calls[i].path != "/repositories/team/repo/hooks" {
			t.Fatalf("create call %d = %+v", i, client.calls[i])
		}
	}
	if client.calls[0].body["description"] != "first" || client.calls[1].body["description"] != "second" {
		t.Fatalf("UUID-less creates selected the wrong specs: %+v", client.calls)
	}
	if got := client.calls[0].body["events"].([]string); len(got) != 1 || got[0] != "repo:push" {
		t.Fatalf("events were not normalized: %#v", got)
	}
	if client.calls[2].method != http.MethodPut || client.calls[2].path != "/repositories/team/repo/hooks/%7Bexisting%7D" {
		t.Fatalf("update call = %+v", client.calls[2])
	}
	if client.calls[3].method != http.MethodDelete || client.calls[3].path != "/repositories/team/repo/hooks/%7Bremoved%7D" {
		t.Fatalf("delete call = %+v", client.calls[3])
	}
}

func TestApplyWebhooksContinuesAfterRequestError(t *testing.T) {
	client := &fakeClient{requestErrAt: 1}
	service := New(client)
	operations := []WebhookOperation{
		{Action: "create", Description: "failed", MatchURL: "https://failed.example/hook"},
		{Action: "delete", UUID: "{removed}"},
	}
	result := service.ApplyWebhooks(context.Background(), WebhookTarget{Path: "/hooks"}, []WebhookSpec{{Description: "failed", URL: "https://failed.example/hook"}}, operations)
	if len(result["errors"].([]any)) != 1 || len(result["applied"].([]any)) != 1 {
		t.Fatalf("expected partial result, got %#v", result)
	}
	if len(client.calls) != 2 || client.calls[1].method != http.MethodDelete {
		t.Fatalf("expected apply to continue after error: %+v", client.calls)
	}
}

func TestSSHAndDeployWriteMethods(t *testing.T) {
	client := &fakeClient{pageValues: []json.RawMessage{json.RawMessage(`{"fingerprint":"other"}`)}}
	service := New(client)
	key := PublicKey{Material: "ssh-ed25519 AAAA", Fingerprint: "new"}
	ctx := context.Background()

	if _, err := service.AddSSHKey(ctx, "{user}", key, "laptop", "2025-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditSSHKey(ctx, "{user}", "{key}", "updated"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSSHKey(ctx, "{user}", "{key}"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddDeployKey(ctx, "/repositories/team/repo/deploy-keys", key, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteDeployKey(ctx, "/repositories/team/repo/deploy-keys", "12"); err != nil {
		t.Fatal(err)
	}
	methods := make([]string, len(client.calls))
	for i, call := range client.calls {
		methods[i] = call.method
	}
	if strings.Join(methods, ",") != "POST,PUT,DELETE,POST,DELETE" {
		t.Fatalf("methods = %v, calls = %+v", methods, client.calls)
	}
	if got := client.calls[0].query.Get("expires_on"); got != "2025-01-02T00:00:00Z" {
		t.Fatalf("SSH expiry query = %q", got)
	}
	if client.calls[4].path != "/repositories/team/repo/deploy-keys/12" {
		t.Fatalf("deploy delete path = %q", client.calls[4].path)
	}
	if err := service.DeleteDeployKey(ctx, "/hooks", "bad/id"); err == nil {
		t.Fatal("expected invalid deploy key id to be rejected")
	}
}
