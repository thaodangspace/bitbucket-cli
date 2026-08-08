package cmd

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func testPublicKey(algorithm string) string {
	blob := make([]byte, 4+len(algorithm)+4)
	binary.BigEndian.PutUint32(blob, uint32(len(algorithm)))
	copy(blob[4:], algorithm)
	binary.BigEndian.PutUint32(blob[4+len(algorithm):], 1)
	return algorithm + " " + base64.StdEncoding.EncodeToString(blob) + " test-key"
}

func TestParsePublicKeyFingerprintAndPrivateKeyRejection(t *testing.T) {
	key, err := parsePublicKey([]byte(testPublicKey("ssh-ed25519")))
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	if key.Algorithm != "ssh-ed25519" || !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Fatalf("unexpected parsed key: %+v", key)
	}
	if _, err := parsePublicKey([]byte("-----BEGIN OPENSSH PRIVATE KEY-----")); err == nil {
		t.Fatal("expected private key rejection")
	}
}

func TestWebhookURLValidation(t *testing.T) {
	for _, value := range []string{"https://user:password@example.com/hook", "http://example.com/hook", "https://127.0.0.1/hook"} {
		if err := validateWebhookURL(value, false, false); err == nil {
			t.Fatalf("expected URL validation error for %q", value)
		}
	}
	if err := validateWebhookURL("http://localhost/hook", true, true); err != nil {
		t.Fatalf("localhost exception rejected: %v", err)
	}
}

func TestWebhookEventsDeduplicateInOrder(t *testing.T) {
	got := uniqueStrings([]string{"repo:push", "", "repo:push", "pullrequest:created"})
	want := []string{"repo:push", "pullrequest:created"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWebhookApplyComparesQueryParameters(t *testing.T) {
	spec := webhookSpec{Description: "CI", URL: "https://ci.example.test/hook?token=new", Events: []string{"repo:push"}}
	existing := []json.RawMessage{json.RawMessage(`{"uuid":"hook-1","description":"CI","url":"https://ci.example.test/hook?token=old","events":["repo:push"]}`)}
	operations := planWebhookApply([]webhookSpec{spec}, existing, false)
	if len(operations) != 1 || operations[0].Action == "no-op" {
		t.Fatalf("query parameter change must not be treated as no-op: %+v", operations)
	}
}

func TestSSHKeyAddUsesResolvedUserUUIDAndExpiryQuery(t *testing.T) {
	keyFile := t.TempDir() + "/id.pub"
	if err := os.WriteFile(keyFile, []byte(testPublicKey("ssh-ed25519")), 0600); err != nil {
		t.Fatal(err)
	}
	var postBody map[string]any
	var postPath, expiry string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			return jsonResp(200, `{"uuid":"{account-1}"}`), nil
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/ssh-keys"):
			if !strings.HasSuffix(r.URL.Path, "/users/{account-1}/ssh-keys") {
				t.Fatalf("unexpected SSH key list path: %s", r.URL.Path)
			}
			return jsonResp(200, `{"values":[]}`), nil
		case r.Method == http.MethodPost:
			postPath = r.URL.Path
			expiry = r.URL.Query().Get("expires_on")
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &postBody); err != nil {
				t.Fatalf("decode POST body: %v", err)
			}
			return jsonResp(201, `{"uuid":"{key-1}","key":"`+testPublicKey("ssh-ed25519")+`","expires_on":"2025-01-02T00:00:00Z"}`), nil
		default:
			return jsonResp(404, `{}`), nil
		}
	}, "ssh-key", "add", "--file", keyFile, "--expires", "2025-01-02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(postPath, "/users/{account-1}/ssh-keys") || expiry != "2025-01-02T00:00:00Z" {
		t.Fatalf("unexpected SSH key request path/query: %s?expires_on=%s", postPath, expiry)
	}
	if _, ok := postBody["expires_on"]; ok {
		t.Fatalf("expiry must be sent as query parameter, body=%v", postBody)
	}
	if postBody["key"] == nil {
		t.Fatalf("missing public key in POST body: %v", postBody)
	}
}

func TestSSHKeyViewAcceptsUUID(t *testing.T) {
	var gotPath string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			return jsonResp(200, `{"uuid":"{account-1}"}`), nil
		}
		gotPath = r.URL.Path
		return jsonResp(200, `{"uuid":"{key-1}","label":"CI"}`), nil
	}, "ssh-key", "view", "{key-1}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/users/{account-1}/ssh-keys/{key-1}") {
		t.Fatalf("unexpected SSH key UUID path: %s", gotPath)
	}
}

func TestExpiryValueUsesUTC(t *testing.T) {
	got, err := expiryValue("2025-01-02")
	if err != nil || got != "2025-01-02T00:00:00Z" {
		t.Fatalf("unexpected expiry: %q, %v", got, err)
	}
	if _, err := expiryValue("not-a-date"); err == nil {
		t.Fatal("expected invalid expiry error")
	}
}
