package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thaodangspace/bitbucket-cli/auth"
	"github.com/thaodangspace/bitbucket-cli/config"
)

// TestLiveAccessTokenValidation is opt-in so the normal suite never requires
// network access or a real credential. Run it with a disposable token and a
// repository the token can read:
//
// BITBUCKET_CLI_LIVE_ACCESS_TOKEN=... \
// BITBUCKET_CLI_LIVE_REPOSITORY=workspace/repo go test ./cmd -run TestLiveAccessTokenValidation
func TestLiveAccessTokenValidation(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("BITBUCKET_CLI_LIVE_ACCESS_TOKEN"))
	repository := strings.TrimSpace(os.Getenv("BITBUCKET_CLI_LIVE_REPOSITORY"))
	if token == "" || repository == "" {
		t.Skip("set BITBUCKET_CLI_LIVE_ACCESS_TOKEN and BITBUCKET_CLI_LIVE_REPOSITORY to run")
	}
	ref, err := parseRepositorySelector(repository)
	if err != nil {
		t.Fatalf("invalid live repository: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := validateAccessCredentials(ctx, auth.NewBearerAuth(auth.TokenAccess, token), ref); err != nil {
		t.Fatalf("live access-token validation failed: %v", err)
	}

	// Exercise the same profile-key contract used by auth login without ever
	// writing the live secret to disk.
	if key, err := config.CredentialStoreKey(auth.TokenAccess, ""); err != nil || key != "access-token" {
		t.Fatalf("unexpected access credential key %q: %v", key, err)
	}
}
