package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/auth"
)

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bitbucket-cli.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func TestLoadConfigFromFile(t *testing.T) {
	path := writeConfigFile(t, `email: file@example.com
api_token: file-token
default_workspace: file-workspace
default_repo: file-repo
`)
	cfg, err := LoadConfig(map[string]string{}, t.TempDir(), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email != "file@example.com" || cfg.APIToken != "file-token" ||
		cfg.DefaultWorkspace != "file-workspace" || cfg.DefaultRepo != "file-repo" {
		t.Fatalf("got %+v", cfg)
	}
	if cfg.CredentialSource != SourceFile || cfg.TokenType != "api" {
		t.Fatalf("expected file source + api token type, got %+v", cfg)
	}
	if cfg.Auth == nil {
		t.Fatal("expected an auth provider to be built")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	path := writeConfigFile(t, `email: file@example.com
api_token: file-token
default_workspace: file-workspace
default_repo: file-repo
`)
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_EMAIL":             "env@example.com",
		"BITBUCKET_DEFAULT_WORKSPACE": "env-workspace",
	}, t.TempDir(), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email != "env@example.com" || cfg.DefaultWorkspace != "env-workspace" {
		t.Fatalf("env should override file: %+v", cfg)
	}
	if cfg.APIToken != "file-token" || cfg.DefaultRepo != "file-repo" {
		t.Fatalf("file should fill gaps: %+v", cfg)
	}
}

func TestLoadConfigMissingFileIsNotAnError(t *testing.T) {
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_EMAIL":     "dev@example.com",
		"BITBUCKET_API_TOKEN": "token",
	}, t.TempDir(), filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("missing config file should be ignored, got: %v", err)
	}
	if cfg.Email != "dev@example.com" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestLoadConfigMalformedFileErrors(t *testing.T) {
	path := writeConfigFile(t, "email: [unterminated\n")
	_, err := LoadConfig(map[string]string{}, t.TempDir(), path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "parse config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetFileValueRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "bitbucket-cli.yaml")
	if err := SetFileValue(path, "email", "  me@example.com  "); err != nil {
		t.Fatalf("set email: %v", err)
	}
	if err := SetFileValue(path, "api_token", "tok"); err != nil {
		t.Fatalf("set api_token: %v", err)
	}
	fc, err := LoadFileConfig(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fc.Email != "me@example.com" {
		t.Fatalf("email not trimmed/stored: %q", fc.Email)
	}
	if fc.APIToken != "tok" {
		t.Fatalf("setting api_token clobbered email or failed: %+v", fc)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config file perms = %o, want 600", perm)
	}
}

func TestSetFileValueRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bitbucket-cli.yaml")
	err := SetFileValue(path, "bogus", "x")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("file should not be created when the key is invalid")
	}
}

func TestFileConfigGetUnknownKey(t *testing.T) {
	var fc FileConfig
	if _, err := fc.Get("bogus"); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	if got := DefaultConfigPath(map[string]string{"BITBUCKET_CONFIG": "/custom/path.yaml"}); got != "/custom/path.yaml" {
		t.Fatalf("BITBUCKET_CONFIG not honored: %q", got)
	}
	if got := DefaultConfigPath(map[string]string{"XDG_CONFIG_HOME": "/xdg"}); got != "/xdg/bitbucket-cli.yaml" {
		t.Fatalf("XDG_CONFIG_HOME not honored: %q", got)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_EMAIL":             "dev@example.com",
		"BITBUCKET_API_TOKEN":         "token",
		"BITBUCKET_DEFAULT_WORKSPACE": "team",
		"BITBUCKET_DEFAULT_REPO":      "repo",
	}, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email != "dev@example.com" || cfg.APIToken != "token" ||
		cfg.DefaultWorkspace != "team" || cfg.DefaultRepo != "repo" {
		t.Fatalf("got %+v", cfg)
	}
	if cfg.CredentialSource != SourceEnv || cfg.TokenType != "api" {
		t.Fatalf("expected env source + api token type, got %+v", cfg)
	}
}

func TestLoadConfigMissingCredentials(t *testing.T) {
	_, err := LoadConfig(map[string]string{}, t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
	if !strings.Contains(err.Error(), "Set BITBUCKET_EMAIL and BITBUCKET_API_TOKEN") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestAccessTokenLoadsFromKeychainWithoutEmail(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.Set("access-token", "resource-secret"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_TOKEN_TYPE":        "access",
		"BITBUCKET_DEFAULT_WORKSPACE": "team",
		"BITBUCKET_DEFAULT_REPO":      "repo",
	}, t.TempDir(), "", WithSecretStore(store))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email != "" || cfg.APIToken != "resource-secret" || cfg.TokenType != "access" {
		t.Fatalf("unexpected access config: %+v", cfg)
	}
	if cfg.Auth.Kind() != "access" {
		t.Fatalf("auth kind = %q", cfg.Auth.Kind())
	}
}

func TestProfiledBearerCredentialsDoNotCollide(t *testing.T) {
	store := auth.NewMemoryStore()
	pathA := filepath.Join(t.TempDir(), "repo-a.yaml")
	pathB := filepath.Join(t.TempDir(), "repo-b.yaml")
	keyA, err := CredentialStoreKeyForProfile(auth.TokenAccess, "", ProfileKey(pathA))
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := CredentialStoreKeyForProfile(auth.TokenAccess, "", ProfileKey(pathB))
	if err != nil {
		t.Fatal(err)
	}
	if keyA == keyB {
		t.Fatal("profiled access-token keys collided")
	}
	if err := store.Set(keyA, "token-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(keyB, "token-b"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want string
	}{
		{pathA, "token-a"},
		{pathB, "token-b"},
	} {
		if err := SetFileValue(tc.path, "token_type", "access"); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(map[string]string{}, t.TempDir(), tc.path, WithSecretStore(store))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIToken != tc.want {
			t.Fatalf("%s resolved %q, want %q", tc.path, cfg.APIToken, tc.want)
		}
	}
}

func TestCredentialStoreKeyCompatibility(t *testing.T) {
	cases := []struct {
		tokenType auth.TokenType
		email     string
		want      string
	}{
		{auth.TokenAPI, "dev@example.com", "dev@example.com"},
		{auth.TokenAccess, "", "access-token"},
		{auth.TokenOAuth, "", "oauth-token"},
	}
	for _, tc := range cases {
		got, err := CredentialStoreKey(tc.tokenType, tc.email)
		if err != nil || got != tc.want {
			t.Fatalf("CredentialStoreKey(%q, %q) = %q, %v", tc.tokenType, tc.email, got, err)
		}
	}
	if _, err := CredentialStoreKey(auth.TokenAPI, ""); err == nil {
		t.Fatal("API token key should require email")
	}
	if _, err := CredentialStoreKey(auth.TokenType("unknown"), "x"); err == nil {
		t.Fatal("unknown token type should fail")
	}
}

func TestLoadConfigTrimsAndTreatsEmptyAsUnset(t *testing.T) {
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_EMAIL":             "  dev@example.com  ",
		"BITBUCKET_API_TOKEN":         "token",
		"BITBUCKET_DEFAULT_WORKSPACE": "   ",
		"BITBUCKET_DEFAULT_REPO":      "",
	}, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Email != "dev@example.com" {
		t.Fatalf("email not trimmed: %q", cfg.Email)
	}
	if cfg.DefaultWorkspace != "" || cfg.DefaultRepo != "" {
		t.Fatalf("expected empty defaults, got %q/%q", cfg.DefaultWorkspace, cfg.DefaultRepo)
	}
}

func gitInitWithRemote(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, out)
		}
	}
	return dir
}

func TestGitRepoRefFromSSH(t *testing.T) {
	dir := gitInitWithRemote(t, "git@bitbucket.org:my-workspace/my-repo-slug.git")
	ref, ok := GitRepoRefFrom(dir)
	if !ok {
		t.Fatal("expected detection")
	}
	if ref.Workspace != "my-workspace" || ref.RepoSlug != "my-repo-slug" {
		t.Fatalf("got %+v", ref)
	}
}

func TestGitRepoRefFromHTTPS(t *testing.T) {
	dir := gitInitWithRemote(t, "https://user@bitbucket.org/other-workspace/other-repo")
	ref, ok := GitRepoRefFrom(dir)
	if !ok {
		t.Fatal("expected detection")
	}
	if ref.Workspace != "other-workspace" || ref.RepoSlug != "other-repo" {
		t.Fatalf("got %+v", ref)
	}
}

func TestGitRepoRefIgnoresNonBitbucket(t *testing.T) {
	dir := gitInitWithRemote(t, "https://github.com/workspace/repo")
	if _, ok := GitRepoRefFrom(dir); ok {
		t.Fatal("expected non-bitbucket remote to be ignored")
	}
}

func TestLoadConfigFillsDefaultsFromGit(t *testing.T) {
	dir := gitInitWithRemote(t, "git@bitbucket.org:git-workspace/git-repo.git")
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_EMAIL":     "dev@example.com",
		"BITBUCKET_API_TOKEN": "token",
	}, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultWorkspace != "git-workspace" || cfg.DefaultRepo != "git-repo" {
		t.Fatalf("git defaults not applied: %+v", cfg)
	}
}

func TestEnvDefaultsOverrideGit(t *testing.T) {
	dir := gitInitWithRemote(t, "git@bitbucket.org:git-workspace/git-repo.git")
	cfg, err := LoadConfig(map[string]string{
		"BITBUCKET_EMAIL":             "dev@example.com",
		"BITBUCKET_API_TOKEN":         "token",
		"BITBUCKET_DEFAULT_WORKSPACE": "env-workspace",
		"BITBUCKET_DEFAULT_REPO":      "env-repo",
	}, dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultWorkspace != "env-workspace" || cfg.DefaultRepo != "env-repo" {
		t.Fatalf("env should override git: %+v", cfg)
	}
}

func TestResolveRepoRefPrefersExplicit(t *testing.T) {
	cfg := Config{DefaultWorkspace: "team", DefaultRepo: "repo"}
	ref, err := ResolveRepoRef(RepoRef{Workspace: "explicit-team", RepoSlug: "explicit-repo"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Workspace != "explicit-team" || ref.RepoSlug != "explicit-repo" {
		t.Fatalf("got %+v", ref)
	}
}

func TestResolveRepoRefFallsBackToDefaults(t *testing.T) {
	cfg := Config{DefaultWorkspace: "team", DefaultRepo: "repo"}
	ref, err := ResolveRepoRef(RepoRef{}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Workspace != "team" || ref.RepoSlug != "repo" {
		t.Fatalf("got %+v", ref)
	}
}

func TestResolveRepoRefErrorsWhenUnresolved(t *testing.T) {
	_, err := ResolveRepoRef(RepoRef{}, Config{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Provide workspace and repo") {
		t.Fatalf("unexpected error: %v", err)
	}
}
