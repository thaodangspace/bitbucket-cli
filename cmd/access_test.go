package cmd

import (
	"encoding/base64"
	"encoding/binary"
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

func TestExpiryValueUsesUTC(t *testing.T) {
	got, err := expiryValue("2025-01-02")
	if err != nil || got != "2025-01-02T00:00:00Z" {
		t.Fatalf("unexpected expiry: %q, %v", got, err)
	}
	if _, err := expiryValue("not-a-date"); err == nil {
		t.Fatal("expected invalid expiry error")
	}
}
