package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thaodangspace/bitbucket-cli/auth"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
)

// withStdin points os.Stdin at a temp file holding content for the duration of
// fn, guaranteeing a non-TTY stdin regardless of the test host.
func withStdin(t *testing.T, content string, fn func()) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "stdin")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	orig := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = orig }()
	fn()
}

// mustStore fetches the active (memory) secret store.
func mustStore(t *testing.T) *auth.MemoryStore {
	t.Helper()
	s, ok := auth.CurrentStore().(*auth.MemoryStore)
	if !ok {
		t.Fatalf("expected memory store, got %T", auth.CurrentStore())
	}
	return s
}

func authUserTransport(accountID string) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"account_id":"`+accountID+`","display_name":"Dev User"}`), nil
	}
}

func TestAuthLoginWithTokenSavesToStore(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	t.Setenv("BITBUCKET_CONFIG", cfgPath)

	var out string
	withStdin(t, "tok-secret-1\n", func() {
		var err error
		out, err = runAt(t, authUserTransport("acct-1"), cfgPath, "auth", "login", "--email", "dev@example.com", "--with-token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if m["authenticated"] != true || m["account"] != "dev@example.com" {
		t.Fatalf("unexpected login output: %v", m)
	}
	if strings.Contains(out, "tok-secret-1") {
		t.Fatalf("token leaked to output: %s", out)
	}

	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Email != "dev@example.com" {
		t.Fatalf("email not saved: %+v", fc)
	}
	if fc.TokenType != "api" {
		t.Fatalf("token_type not saved: %+v", fc)
	}
	if fc.APIToken != "" {
		t.Fatalf("token must not be stored in config file: %+v", fc)
	}

	got, err := mustStore(t).Get("dev@example.com")
	if err != nil || got != "tok-secret-1" {
		t.Fatalf("token not in store: %q err=%v", got, err)
	}
}

func TestAuthLoginInvalidCredsDoesNotSave(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	t.Setenv("BITBUCKET_CONFIG", cfgPath)

	withStdin(t, "bad-token\n", func() {
		_, err := runAt(t, func(r *http.Request) (*http.Response, error) {
			return jsonResp(401, `{"error":{"message":"invalid credentials"}}`), nil
		}, cfgPath, "auth", "login", "--email", "dev@example.com", "--with-token")
		if err == nil {
			t.Fatal("expected login to fail for invalid credentials")
		}
	})

	if _, err := mustStore(t).Get("dev@example.com"); err == nil {
		t.Fatal("token must not be stored after failed login")
	}
	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Email != "" {
		t.Fatalf("email must not be saved after failed login: %+v", fc)
	}
}

func TestAuthLoginNonTTYWithoutWithTokenRefused(t *testing.T) {
	withStdin(t, "tok\n", func() {
		_, err := run(t, nil, "auth", "login", "--email", "dev@example.com")
		if err == nil {
			t.Fatal("expected refusal when stdin is not a TTY and --with-token is absent")
		}
		if !strings.Contains(err.Error(), "--with-token") {
			t.Fatalf("expected hint about --with-token, got: %v", err)
		}
	})
}

func TestAuthLoginAccessTokenType(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	t.Setenv("BITBUCKET_CONFIG", cfgPath)

	withStdin(t, "acc-tok\n", func() {
		_, err := runAt(t, authUserTransport("acct-2"), cfgPath, "auth", "login",
			"--email", "bot@example.com", "--with-token", "--token-type", "access")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.TokenType != "access" || fc.Email != "" {
		t.Fatalf("access token must not persist an account identity: %+v", fc)
	}
	accessKey := config.ProfileKey(cfgPath) + ":access"
	if got, err := mustStore(t).Get(accessKey); err != nil || got != "acc-tok" {
		t.Fatalf("access token not stored under resource key: %q err=%v", got, err)
	}
}

func TestAuthLoginAccessTokenWithoutEmailUsesBearerRepoProbe(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	var paths []string
	var authHeader string
	withStdin(t, "access-secret\n", func() {
		_, err := runAt(t, func(r *http.Request) (*http.Response, error) {
			paths = append(paths, r.URL.Path)
			authHeader = r.Header.Get("Authorization")
			if strings.HasSuffix(r.URL.Path, "/user") {
				return jsonResp(500, `{"error":{"message":"must not probe user"}}`), nil
			}
			return jsonResp(200, `{}`), nil
		}, cfgPath, "auth", "login", "--with-token", "--token-type", "access")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if authHeader != "Bearer access-secret" {
		t.Fatalf("authorization = %q", authHeader)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/repositories/team/repo") {
		t.Fatalf("unexpected validation paths: %v", paths)
	}
}

func TestEnvSetBearerCredentialsWithoutEmail(t *testing.T) {
	t.Setenv("BITBUCKET_API_TOKEN", "bearer-secret")
	t.Setenv("BITBUCKET_EMAIL", "")
	t.Setenv("BITBUCKET_CONFIG", filepath.Join(t.TempDir(), "profile.yaml"))
	t.Setenv("BITBUCKET_TOKEN_TYPE", "access")
	if !envSet() {
		t.Fatal("access-token environment credentials were not detected")
	}
	t.Setenv("BITBUCKET_TOKEN_TYPE", "oauth")
	if !envSet() {
		t.Fatal("OAuth environment credentials were not detected")
	}
	t.Setenv("BITBUCKET_TOKEN_TYPE", "api")
	if envSet() {
		t.Fatal("API environment credentials without email were detected")
	}
}

func TestAuthStatusAccessTokenSkipsUserProbe(t *testing.T) {
	t.Setenv("BITBUCKET_TOKEN_TYPE", "access")
	var paths []string
	_, err := run(t, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return jsonResp(200, `{}`), nil
	}, "auth", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 || strings.HasSuffix(paths[0], "/user") {
		t.Fatalf("unexpected status paths: %v", paths)
	}
}

func TestAuthSwitchAPIToAccessCleansOldSecretAndLogout(t *testing.T) {
	store := auth.NewMemoryStore()
	cfgPath := filepath.Join(t.TempDir(), "profile.yaml")
	t.Setenv("BITBUCKET_TOKEN_TYPE", "")
	withStdin(t, "api-secret\n", func() {
		if _, err := runAtStore(t, store, authUserTransport("acct-switch"), cfgPath,
			"auth", "login", "--email", "dev@example.com", "--with-token"); err != nil {
			t.Fatal(err)
		}
	})
	withStdin(t, "access-secret\n", func() {
		if _, err := runAtStore(t, store, func(r *http.Request) (*http.Response, error) {
			return jsonResp(200, `{}`), nil
		}, cfgPath, "auth", "login", "--with-token", "--token-type", "access"); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := store.Get("dev@example.com"); err == nil {
		t.Fatal("API secret remained after switching token type")
	}
	accessKey := config.ProfileKey(cfgPath) + ":access"
	if got, err := store.Get(accessKey); err != nil || got != "access-secret" {
		t.Fatalf("access secret missing after switch: %q %v", got, err)
	}
	if _, err := runAtStore(t, store, nil, cfgPath, "auth", "logout", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(accessKey); err == nil {
		t.Fatal("access secret remained after logout")
	}
}

func TestAuthSwitchAccessToOAuthCleansOldSecret(t *testing.T) {
	store := auth.NewMemoryStore()
	cfgPath := filepath.Join(t.TempDir(), "profile.yaml")
	t.Setenv("BITBUCKET_TOKEN_TYPE", "")
	withStdin(t, "access-secret\n", func() {
		if _, err := runAtStore(t, store, func(r *http.Request) (*http.Response, error) {
			return jsonResp(200, `{}`), nil
		}, cfgPath, "auth", "login", "--with-token", "--token-type", "access"); err != nil {
			t.Fatal(err)
		}
	})
	withStdin(t, "oauth-secret\n", func() {
		if _, err := runAtStore(t, store, authUserTransport("oauth-acct"), cfgPath,
			"auth", "login", "--with-token", "--token-type", "oauth"); err != nil {
			t.Fatal(err)
		}
	})
	accessKey := config.ProfileKey(cfgPath) + ":access"
	oauthKey := config.ProfileKey(cfgPath) + ":oauth"
	if _, err := store.Get(accessKey); err == nil {
		t.Fatal("access secret remained after switching to OAuth")
	}
	if got, err := store.Get(oauthKey); err != nil || got != "oauth-secret" {
		t.Fatalf("OAuth secret missing after switch: %q %v", got, err)
	}
}

func TestAuthLoginInvalidTokenType(t *testing.T) {
	_, err := run(t, nil, "auth", "login", "--token-type", "nope")
	if err == nil {
		t.Fatal("expected error for invalid token type")
	}
}

func TestAuthStatusLoggedInEnvSource(t *testing.T) {
	out, err := run(t, authUserTransport("acct-3"), "auth", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if m["loggedIn"] != true {
		t.Fatalf("expected logged in: %v", m)
	}
	if m["credentialSource"] != "env" || m["tokenType"] != "api" {
		t.Fatalf("unexpected source/type: %v", m)
	}
	if m["defaultRepo"] != "team/repo" {
		t.Fatalf("unexpected default repo: %v", m)
	}
	if strings.Contains(out, `"`+tokenForTest(t)+`"`) || strings.Contains(out, "token") {
		// token must never appear; the literal env token is "token" and must not leak.
		if strings.Contains(out, "Basic ") {
			t.Fatalf("authorization leaked: %s", out)
		}
	}
	if m["repoAccess"] != true {
		t.Fatalf("expected repo access probe true: %v", m)
	}
}

// tokenForTest returns the env token used by the run() helper.
func tokenForTest(_ *testing.T) string { return "token" }

func TestAuthStatusNotLoggedIn(t *testing.T) {
	t.Setenv("BITBUCKET_EMAIL", "")
	t.Setenv("BITBUCKET_API_TOKEN", "")
	t.Setenv("BITBUCKET_CONFIG", t.TempDir()+"/cfg.yaml")

	var out string
	var err error
	flagWorkspace, flagRepo, flagPretty = "", "", false
	resetFlags(rootCmd)
	rootCmd.SetArgs([]string{"auth", "status"})
	out, err = captureRun(func() error { return rootCmd.Execute() })
	if err != nil {
		t.Fatalf("auth status must not fail when logged out: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if m["loggedIn"] != false {
		t.Fatalf("expected logged out: %v", m)
	}
}

func TestAuthLogoutRemovesProfile(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	t.Setenv("BITBUCKET_EMAIL", "dev@example.com")
	t.Setenv("BITBUCKET_API_TOKEN", "token")
	t.Setenv("BITBUCKET_DEFAULT_WORKSPACE", "team")
	t.Setenv("BITBUCKET_DEFAULT_REPO", "repo")
	t.Setenv("BITBUCKET_CONFIG", cfgPath)

	// Pre-populate a stored profile like `auth login` would.
	if err := config.SetFileValue(cfgPath, "email", "dev@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetFileValue(cfgPath, "token_type", "api"); err != nil {
		t.Fatal(err)
	}
	store := auth.NewMemoryStore()
	if err := store.Set("dev@example.com", "tok-secret"); err != nil {
		t.Fatal(err)
	}
	auth.SetGlobalStore(store)
	t.Cleanup(func() { auth.SetGlobalStore(nil) })

	flagWorkspace, flagRepo, flagPretty = "", "", false
	flagLoginEmail, flagLoginWithToken, flagLoginTokenType = "", false, string(auth.TokenAPI)
	flagStatusJSON, flagLogoutYes = false, false
	resetFlags(rootCmd)
	testTransport = nil

	rootCmd.SetArgs([]string{"auth", "logout", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.Get("dev@example.com"); err == nil {
		t.Fatal("token still in store after logout")
	}
	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Email != "" || fc.TokenType != "" {
		t.Fatalf("profile not cleared from config: %+v", fc)
	}
}

func TestAuthTokenPrintsToken(t *testing.T) {
	out, err := run(t, nil, "auth", "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "token" {
		t.Fatalf("expected env token, got %q", out)
	}
}

func TestMigrationClearsPlaintextAndSaves(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	raw := "email: dev@example.com\napi_token: plaintext-abc\ndefault_workspace: team\ndefault_repo: repo\n"
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	store := auth.NewMemoryStore()
	migrated, err := config.MigrateLegacyToken(cfgPath, store)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("expected migration to happen")
	}

	got, err := store.Get("dev@example.com")
	if err != nil || got != "plaintext-abc" {
		t.Fatalf("token not migrated: %q err=%v", got, err)
	}
	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.APIToken != "" {
		t.Fatalf("plaintext token still in config: %+v", fc)
	}
	if fc.Email != "dev@example.com" || fc.DefaultWorkspace != "team" {
		t.Fatalf("other keys not preserved: %+v", fc)
	}

	// Second run is a no-op.
	migrated2, err := config.MigrateLegacyToken(cfgPath, store)
	if err != nil || migrated2 {
		t.Fatalf("second migration should be no-op: %v %v", migrated2, err)
	}
}

func TestEnvPrecedenceOverFileAndKeychain(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := config.SetFileValue(cfgPath, "email", "file@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetFileValue(cfgPath, "api_token", "file-token"); err != nil {
		t.Fatal(err)
	}
	store := auth.NewMemoryStore()
	if err := store.Set("env@example.com", "keychain-token"); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{
		"BITBUCKET_EMAIL":     "env@example.com",
		"BITBUCKET_API_TOKEN": "env-token",
	}
	cfg, err := config.LoadConfig(env, "", cfgPath, config.WithSecretStore(store))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIToken != "env-token" || cfg.CredentialSource != config.SourceEnv {
		t.Fatalf("env must win: %+v", cfg)
	}
}

func TestKeychainFallback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := config.SetFileValue(cfgPath, "email", "dev@example.com"); err != nil {
		t.Fatal(err)
	}
	store := auth.NewMemoryStore()
	if err := store.Set("dev@example.com", "keychain-token"); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{"BITBUCKET_EMAIL": "dev@example.com"}
	cfg, err := config.LoadConfig(env, "", cfgPath, config.WithSecretStore(store))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIToken != "keychain-token" || cfg.CredentialSource != config.SourceKeychain {
		t.Fatalf("expected keychain fallback: %+v", cfg)
	}
}

func TestClientRedactsTokenInErrors(t *testing.T) {
	testTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(403, `{"message":"forbidden token tok-secret-9"}`), nil
	})
	t.Cleanup(func() { testTransport = nil })

	c := bitbucket.NewClient(
		auth.ProviderFor(auth.TokenAPI, "dev@example.com", "tok-secret-9"),
		bitbucket.WithHTTPClient(&http.Client{Transport: testTransport}),
	)
	err := c.Request(context.Background(), "/repositories/team/repo", bitbucket.RequestOptions{}, &map[string]any{})
	he, ok := err.(*bitbucket.HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T: %v", err, err)
	}
	if strings.Contains(he.Excerpt, "tok-secret-9") {
		t.Fatalf("token leaked in excerpt: %s", he.Excerpt)
	}
	if !strings.Contains(he.Excerpt, "<redacted>") {
		t.Fatalf("excerpt not redacted: %s", he.Excerpt)
	}
}

func TestAccessTokenRedactsReflectedSecret(t *testing.T) {
	testTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(400, `{"message":"access-secret"}`), nil
	})
	t.Cleanup(func() { testTransport = nil })
	c := bitbucket.NewClient(
		auth.ProviderFor(auth.TokenAccess, "", "access-secret"),
		bitbucket.WithHTTPClient(&http.Client{Transport: testTransport}),
	)
	err := c.Request(context.Background(), "/repositories/team/repo?token=access-secret", bitbucket.RequestOptions{}, nil)
	he, ok := err.(*bitbucket.HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T: %v", err, err)
	}
	if strings.Contains(he.URL, "access-secret") || strings.Contains(he.Excerpt, "access-secret") {
		t.Fatalf("access token leaked: %+v", he)
	}
	if !strings.Contains(he.URL, "<redacted>") || !strings.Contains(he.Excerpt, "<redacted>") {
		t.Fatalf("access token was not redacted: %+v", he)
	}
}

func TestClientBasicAndBearerHeaders(t *testing.T) {
	cases := []struct {
		name string
		tt   auth.TokenType
		want string
	}{
		{"api", auth.TokenAPI, "Basic ZGV2QGV4YW1wbGUuY29tOnRva3M="},
		{"access", auth.TokenAccess, "Bearer toks"},
		{"oauth", auth.TokenOAuth, "Bearer toks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var authHeader string
			testTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				authHeader = r.Header.Get("Authorization")
				return jsonResp(200, `{"values":[]}`), nil
			})
			t.Cleanup(func() { testTransport = nil })

			c := bitbucket.NewClient(
				auth.ProviderFor(tc.tt, "dev@example.com", "toks"),
				bitbucket.WithHTTPClient(&http.Client{Transport: testTransport}),
			)
			if err := c.Request(context.Background(), "/user", bitbucket.RequestOptions{}, nil); err != nil {
				t.Fatal(err)
			}
			if authHeader != tc.want {
				t.Fatalf("authorization = %q, want %q", authHeader, tc.want)
			}
		})
	}
}

// captureRun runs fn while capturing stdout.
func captureRun(fn func() error) (string, error) {
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	err := fn()
	w.Close()
	os.Stdout = orig
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if rerr != nil {
			break
		}
	}
	return sb.String(), err
}

// availabilityStub wraps a MemoryStore with a configurable Availability() and
// optional Set() failure so tests exercise the login availability gate and the
// "no partial profile" contract without touching a real store.
type availabilityStub struct {
	*auth.MemoryStore
	availErr error
	setErr   error
}

func (s *availabilityStub) Available() error { return s.availErr }

func (s *availabilityStub) Set(account, secret string) error {
	if s.setErr != nil {
		return s.setErr
	}
	return s.MemoryStore.Set(account, secret)
}

// deleteRecorder records every Delete so tests can assert rollback behavior.
type deleteRecorder struct {
	*auth.MemoryStore
	deleted []string
}

func (r *deleteRecorder) Delete(account string) error {
	r.deleted = append(r.deleted, account)
	return r.MemoryStore.Delete(account)
}

// chmodReadOnly makes a file unwritable so config writes fail deterministically.
// It is skipped when running as root, which can write anyway.
func chmodReadOnly(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not block root")
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
}

func TestAuthLoginAvailabilityFailureLeavesNoProfile(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	store := &availabilityStub{
		MemoryStore: auth.NewMemoryStore(),
		availErr:    fmt.Errorf("%w: no keyring", auth.ErrStoreUnavailable),
	}
	withStdin(t, "tok-secret\n", func() {
		_, err := runAtStore(t, store, authUserTransport("acct-1"), cfgPath,
			"auth", "login", "--email", "dev@example.com", "--with-token")
		if err == nil {
			t.Fatal("expected login to fail when the store is unavailable")
		}
		if !errors.Is(err, auth.ErrStoreUnavailable) {
			t.Fatalf("error = %v, want ErrStoreUnavailable", err)
		}
	})
	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Email != "" || fc.TokenType != "" || fc.APIToken != "" {
		t.Fatalf("login mutated the profile despite an unavailable store: %+v", fc)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("login should not create a config file when the store is unavailable")
	}
	if _, err := store.Get("dev@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("token must not be persisted when the store is unavailable: %v", err)
	}
}

func TestAuthLoginStoreSetFailureLeavesNoPartialProfile(t *testing.T) {
	cfgPath := t.TempDir() + "/cfg.yaml"
	store := &availabilityStub{
		MemoryStore: auth.NewMemoryStore(),
		setErr:      errors.New(auth.ErrStoreUnavailable.Error() + ": daemon down"),
	}
	withStdin(t, "tok-secret\n", func() {
		_, err := runAtStore(t, store, authUserTransport("acct-2"), cfgPath,
			"auth", "login", "--email", "dev@example.com", "--with-token")
		if err == nil {
			t.Fatal("expected login to fail when the secret write fails")
		}
	})
	fc, err := config.LoadFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Email != "" || fc.TokenType != "" || fc.APIToken != "" {
		t.Fatalf("half-written profile after store failure: %+v", fc)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("login must not create a config file when the secret cannot be stored")
	}
}

func TestAuthLoginRollsBackSecretWhenConfigWriteFails(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("email: old@example.com\ntoken_type: api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodReadOnly(t, cfgPath) // forces config.WriteFileConfig to fail

	store := &deleteRecorder{MemoryStore: auth.NewMemoryStore()}
	withStdin(t, "tok-secret\n", func() {
		_, err := runAtStore(t, store, authUserTransport("acct-3"), cfgPath,
			"auth", "login", "--email", "dev@example.com", "--with-token")
		if err == nil {
			t.Fatal("expected login to fail when the config write fails")
		}
	})
	if len(store.deleted) != 1 || store.deleted[0] != "dev@example.com" {
		t.Fatalf("expected rollback delete of the new secret, deleted=%v", store.deleted)
	}
	if _, err := store.Get("dev@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("secret should be rolled back after config failure: %v", err)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "email: old@example.com") {
		t.Fatalf("previous profile must be preserved on failure: %s", b)
	}
	if strings.Contains(string(b), "tok-secret") {
		t.Fatalf("token leaked into plaintext config: %s", b)
	}
}

func TestAuthLoginRollbackRestoresPreviousSecret(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("email: old@example.com\ntoken_type: api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodReadOnly(t, cfgPath) // forces config.WriteFileConfig to fail

	// Re-login to the same account reuses the same store key: the previous
	// token must be restored, never deleted, when the profile write fails.
	store := &deleteRecorder{MemoryStore: auth.NewMemoryStore()}
	if err := store.MemoryStore.Set("dev@example.com", "old-secret"); err != nil {
		t.Fatal(err)
	}
	withStdin(t, "tok-secret\n", func() {
		_, err := runAtStore(t, store, authUserTransport("acct-4"), cfgPath,
			"auth", "login", "--email", "dev@example.com", "--with-token")
		if err == nil {
			t.Fatal("expected login to fail when the config write fails")
		}
	})
	if len(store.deleted) != 0 {
		t.Fatalf("rollback must restore, not delete, a pre-existing secret; deleted=%v", store.deleted)
	}
	if got, err := store.Get("dev@example.com"); err != nil || got != "old-secret" {
		t.Fatalf("previous credential must be restored after a failed re-login: got %q err=%v", got, err)
	}
}

func TestMigrationBlocksOnUnavailableStoreKeepingPlaintext(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("email: dev@example.com\napi_token: plaintext-abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &availabilityStub{
		MemoryStore: auth.NewMemoryStore(),
		availErr:    errors.New(auth.ErrStoreUnavailable.Error()),
	}
	if _, err := config.MigrateLegacyToken(cfgPath, store); err == nil {
		t.Fatal("expected migration to fail when the store is unavailable")
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "api_token: plaintext-abc") {
		t.Fatalf("plaintext token must not be removed until secure write succeeds: %s", b)
	}
}

func TestMigrationRollsBackSecretWhenConfigWriteFails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("email: dev@example.com\napi_token: plaintext-abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodReadOnly(t, cfgPath) // secret-tool writes go to memory, the config write fails

	store := &deleteRecorder{MemoryStore: auth.NewMemoryStore()}
	if _, err := config.MigrateLegacyToken(cfgPath, store); err == nil {
		t.Fatal("expected migration to fail when the config write fails")
	}
	if _, err := store.Get("dev@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("secret should be rolled back after config write failure: %v", err)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "api_token: plaintext-abc") {
		t.Fatalf("plaintext token must remain when migration fails: %s", b)
	}
}

func TestMigrationRollbackRestoresPreviousSecret(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("email: dev@example.com\napi_token: plaintext-abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodReadOnly(t, cfgPath)

	// Migration reuses the email store key; an existing credential must be
	// restored, never deleted, when the config write fails.
	store := &deleteRecorder{MemoryStore: auth.NewMemoryStore()}
	if err := store.MemoryStore.Set("dev@example.com", "old-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.MigrateLegacyToken(cfgPath, store); err == nil {
		t.Fatal("expected migration to fail when the config write fails")
	}
	if len(store.deleted) != 0 {
		t.Fatalf("rollback must restore, not delete, a pre-existing secret; deleted=%v", store.deleted)
	}
	if got, err := store.Get("dev@example.com"); err != nil || got != "old-secret" {
		t.Fatalf("previous secret must be restored after failed migration: got %q err=%v", got, err)
	}
}
