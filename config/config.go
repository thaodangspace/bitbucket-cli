// Package config resolves Bitbucket credentials and repo defaults from, in
// order of precedence: environment variables, a YAML config file
// (~/.config/bitbucket-cli.yaml by default), and finally the local git remote
// for the default workspace/repo.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/thaodangspace/bitbucket-cli/auth"
	"gopkg.in/yaml.v3"
)

// CredentialSource labels where the active token came from.
type CredentialSource string

// Supported credential sources.
const (
	SourceEnv      CredentialSource = "env"      // BITBUCKET_API_TOKEN
	SourceFile     CredentialSource = "file"     // legacy plaintext api_token in YAML
	SourceKeychain CredentialSource = "keychain" // OS credential store
)

// Config holds resolved Bitbucket credentials and optional repo defaults.
type Config struct {
	Email            string
	APIToken         string
	TokenType        string // auth token type: "api", "access", or "oauth"
	CredentialKey    string // non-secret profile key for bearer credentials
	CredentialSource CredentialSource
	Auth             auth.Provider
	DefaultWorkspace string
	DefaultRepo      string
	CloneProtocol    string
	HTTPTimeout      time.Duration
	HTTPTimeoutSet   bool
}

// RepoRef is an unresolved workspace/repo reference, typically from CLI flags.
type RepoRef struct {
	Workspace string
	RepoSlug  string
}

// ResolvedRepoRef is a fully resolved workspace/repo pair.
type ResolvedRepoRef struct {
	Workspace string
	RepoSlug  string
}

// FileConfig mirrors the YAML config file. All fields are optional and act as
// fallbacks for the corresponding environment variables. api_token is the
// legacy plaintext location and is migrated to the credential store by
// MigrateLegacyToken.
type FileConfig struct {
	Email            string `yaml:"email,omitempty"`
	APIToken         string `yaml:"api_token,omitempty"`
	TokenType        string `yaml:"token_type,omitempty"`
	CredentialKey    string `yaml:"credential_key,omitempty"`
	DefaultWorkspace string `yaml:"default_workspace,omitempty"`
	DefaultRepo      string `yaml:"default_repo,omitempty"`
	CloneProtocol    string `yaml:"clone_protocol,omitempty"`
	HTTPTimeout      string `yaml:"http_timeout,omitempty"`
}

// FileKeys are the keys settable in the config file, in display order.
var FileKeys = []string{"email", "token_type", "credential_key", "api_token", "default_workspace", "default_repo", "clone_protocol", "http_timeout"}

func (fc *FileConfig) field(key string) (*string, error) {
	switch key {
	case "email":
		return &fc.Email, nil
	case "api_token":
		return &fc.APIToken, nil
	case "token_type":
		return &fc.TokenType, nil
	case "credential_key":
		return &fc.CredentialKey, nil
	case "default_workspace":
		return &fc.DefaultWorkspace, nil
	case "default_repo":
		return &fc.DefaultRepo, nil
	case "clone_protocol":
		return &fc.CloneProtocol, nil
	case "http_timeout":
		return &fc.HTTPTimeout, nil
	default:
		return nil, fmt.Errorf("unknown config key %q (valid keys: %s)", key, strings.Join(FileKeys, ", "))
	}
}

// Get returns the stored value for key.
func (fc FileConfig) Get(key string) (string, error) {
	p, err := (&fc).field(key)
	if err != nil {
		return "", err
	}
	return *p, nil
}

// Set assigns value (trimmed) to key.
func (fc *FileConfig) Set(key, value string) error {
	p, err := fc.field(key)
	if err != nil {
		return err
	}
	*p = strings.TrimSpace(value)
	return nil
}

// DefaultConfigPath returns the config file location, honoring BITBUCKET_CONFIG
// from env, then XDG_CONFIG_HOME, falling back to ~/.config/bitbucket-cli.yaml.
func DefaultConfigPath(env map[string]string) string {
	if p := trimmed(env, "BITBUCKET_CONFIG"); p != "" {
		return p
	}
	if dir := trimmed(env, "XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "bitbucket-cli.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "bitbucket-cli.yaml")
}

// LoadFileConfig reads and parses the YAML config at path. A missing file
// yields an empty FileConfig and no error; malformed YAML is an error. An empty
// path returns an empty FileConfig.
func LoadFileConfig(path string) (FileConfig, error) {
	if strings.TrimSpace(path) == "" {
		return FileConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, nil
		}
		return FileConfig{}, fmt.Errorf("read config file %s: %w", path, err)
	}
	var fc FileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return FileConfig{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return fc, nil
}

// WriteFileConfig writes fc as YAML to path, creating parent directories. The
// file is written with 0600 permissions since it may hold an API token.
func WriteFileConfig(path string, fc FileConfig) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no config file path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	data, err := yaml.Marshal(fc)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	return nil
}

// SetFileValue loads the config at path, sets key to value, and writes it back,
// preserving the file's other values.
func SetFileValue(path, key, value string) error {
	fc, err := LoadFileConfig(path)
	if err != nil {
		return err
	}
	if err := fc.Set(key, value); err != nil {
		return err
	}
	return WriteFileConfig(path, fc)
}

func trimmed(env map[string]string, key string) string {
	return strings.TrimSpace(env[key])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func resolveHTTPTimeout(env map[string]string, file FileConfig) (time.Duration, bool, error) {
	raw := firstNonEmpty(env["BITBUCKET_HTTP_TIMEOUT"], file.HTTPTimeout)
	if raw == "" {
		return 0, false, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration < 0 {
		return 0, false, fmt.Errorf("invalid HTTP timeout %q (use a non-negative duration such as 30s, or 0 to disable)", raw)
	}
	return duration, true, nil
}

// CredentialStoreKey returns the non-secret profile key used for a token type.
// API tokens retain email-based keychain entries for backward compatibility;
// resource-scoped credentials do not require an Atlassian user identity.
func CredentialStoreKey(tokenType auth.TokenType, email string) (string, error) {
	switch tokenType {
	case auth.TokenAPI:
		email = strings.TrimSpace(email)
		if email == "" {
			return "", fmt.Errorf("email is required for API-token credentials")
		}
		return email, nil
	case auth.TokenAccess:
		return "access-token", nil
	case auth.TokenOAuth:
		return "oauth-token", nil
	default:
		return "", fmt.Errorf("unsupported token type %q", tokenType)
	}
}

// ProfileKey derives a stable, non-secret identifier from a config path. It
// lets multiple BITBUCKET_CONFIG profiles share one credential service without
// sharing their bearer token entries.
func ProfileKey(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	sum := sha256.Sum256([]byte(path))
	return "profile-" + hex.EncodeToString(sum[:12])
}

// CredentialStoreKeyForProfile returns a namespaced key for a bearer profile.
// API-token keys intentionally remain email-based for backward compatibility.
func CredentialStoreKeyForProfile(tokenType auth.TokenType, email, profileKey string) (string, error) {
	if tokenType == auth.TokenAPI {
		return CredentialStoreKey(tokenType, email)
	}
	if tokenType != auth.TokenAccess && tokenType != auth.TokenOAuth {
		return "", fmt.Errorf("unsupported token type %q", tokenType)
	}
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return "", fmt.Errorf("credential profile key is required for %s credentials", tokenType)
	}
	return profileKey + ":" + string(tokenType), nil
}

func validTokenType(value string) bool {
	switch auth.TokenType(strings.TrimSpace(value)) {
	case auth.TokenAPI, auth.TokenAccess, auth.TokenOAuth:
		return true
	default:
		return false
	}
}

// LoadOptions tweak credential resolution (used in tests).
type LoadOptions struct {
	SecretStore auth.SecretStore
}

// LoadOption configures LoadConfig.
type LoadOption func(*LoadOptions)

// WithSecretStore injects a SecretStore to resolve keychain credentials.
func WithSecretStore(s auth.SecretStore) LoadOption {
	return func(o *LoadOptions) { o.SecretStore = s }
}

// resolveToken determines the active token following env > config file >
// credential store precedence. The second return is the CredentialSource.
func resolveToken(env map[string]string, file FileConfig, store auth.SecretStore, tokenType auth.TokenType, email, profileKey string) (string, CredentialSource, error) {
	if t := trimmed(env, "BITBUCKET_API_TOKEN"); t != "" {
		return t, SourceEnv, nil
	}
	if t := strings.TrimSpace(file.APIToken); t != "" {
		return t, SourceFile, nil
	}
	var key string
	var err error
	if tokenType == auth.TokenAPI {
		key, err = CredentialStoreKey(tokenType, email)
	} else {
		key, err = CredentialStoreKeyForProfile(tokenType, email, profileKey)
	}
	if err != nil {
		// Missing API email means there cannot be an API keychain lookup, but
		// it is valid for access/OAuth credentials and should be handled by
		// LoadConfig's credential-required error below.
		if tokenType == auth.TokenAPI && strings.TrimSpace(email) == "" {
			return "", "", nil
		}
		return "", "", err
	}
	if t, err := store.Get(key); err == nil {
		return strings.TrimSpace(t), SourceKeychain, nil
	} else if errors.Is(err, auth.ErrStoreUnavailable) {
		// The keychain backend is unusable (headless session, missing helper,
		// unsupported platform). Surface it so the CLI explains the real
		// failure and the safe environment fallback instead of the generic
		// missing-credential message. Env/file tokens were already tried above
		// and take precedence.
		return "", "", err
	} else if !errors.Is(err, auth.ErrNotFound) {
		// A broken credential store must not break env/file automation;
		// treat it as "no keychain token" rather than failing.
		return "", "", nil
	}
	// Profiles created before credential_key was persisted used the
	// token-type-global key. Keep a one-way compatibility fallback; new login
	// writes the namespaced key and therefore cannot collide.
	if tokenType != auth.TokenAPI && strings.TrimSpace(file.CredentialKey) == "" {
		legacyKey, legacyErr := CredentialStoreKey(tokenType, email)
		if legacyErr == nil {
			if t, getErr := store.Get(legacyKey); getErr == nil {
				return strings.TrimSpace(t), SourceKeychain, nil
			} else if errors.Is(getErr, auth.ErrStoreUnavailable) {
				return "", "", getErr
			}
		}
	}
	return "", "", nil
}

// LoadConfig builds a Config from the given environment map, the YAML config
// file at configPath (pass "" to skip), and the local git remote. gitCwd is the
// directory used for git remote auto-detection of the default workspace/repo
// (pass "" for the current process directory). Precedence is env > file > git.
// API credentials require an email; access and OAuth credentials are bearer
// tokens and do not require a user email.
func LoadConfig(env map[string]string, gitCwd, configPath string, opts ...LoadOption) (Config, error) {
	o := &LoadOptions{SecretStore: auth.CurrentStore()}
	for _, opt := range opts {
		opt(o)
	}

	file, err := LoadFileConfig(configPath)
	if err != nil {
		return Config{}, err
	}
	httpTimeout, httpTimeoutSet, err := resolveHTTPTimeout(env, file)
	if err != nil {
		return Config{}, err
	}

	email := firstNonEmpty(env["BITBUCKET_EMAIL"], file.Email)
	tokenType := firstNonEmpty(env["BITBUCKET_TOKEN_TYPE"], file.TokenType)
	if tokenType == "" {
		tokenType = string(auth.TokenAPI)
	}
	if !validTokenType(tokenType) {
		return Config{}, fmt.Errorf("invalid token type %q (use api, access, or oauth)", tokenType)
	}

	tt := auth.TokenType(tokenType)
	credentialKey := strings.TrimSpace(file.CredentialKey)
	if tt != auth.TokenAPI && credentialKey == "" {
		credentialKey = ProfileKey(configPath)
	}
	token, source, err := resolveToken(env, file, o.SecretStore, tt, email, credentialKey)
	if err != nil {
		return Config{}, err
	}

	if token == "" {
		if tt == auth.TokenAPI && email == "" {
			return Config{}, fmt.Errorf("Set BITBUCKET_EMAIL and BITBUCKET_API_TOKEN (via environment or %s) before using bitbucket-cli.", configHint(configPath))
		}
		return Config{}, fmt.Errorf("Set BITBUCKET_API_TOKEN or run `bitbucket-cli auth login --token-type %s` before using bitbucket-cli.", tokenType)
	}
	if tt == auth.TokenAPI && email == "" {
		return Config{}, fmt.Errorf("Set BITBUCKET_EMAIL for API-token credentials before using bitbucket-cli.")
	}

	workspace := firstNonEmpty(env["BITBUCKET_DEFAULT_WORKSPACE"], file.DefaultWorkspace)
	repo := firstNonEmpty(env["BITBUCKET_DEFAULT_REPO"], file.DefaultRepo)
	cloneProtocol := firstNonEmpty(env["BITBUCKET_CLONE_PROTOCOL"], file.CloneProtocol)
	if cloneProtocol != "" && cloneProtocol != "https" && cloneProtocol != "ssh" {
		return Config{}, fmt.Errorf("invalid clone protocol %q (use https or ssh)", cloneProtocol)
	}

	if workspace == "" || repo == "" {
		if ref, ok := GitRepoRefFrom(gitCwd); ok {
			if workspace == "" {
				workspace = ref.Workspace
			}
			if repo == "" {
				repo = ref.RepoSlug
			}
		}
	}

	return Config{
		Email:            email,
		APIToken:         token,
		TokenType:        tokenType,
		CredentialKey:    credentialKey,
		CredentialSource: source,
		Auth:             auth.ProviderFor(tt, email, token),
		DefaultWorkspace: workspace,
		DefaultRepo:      repo,
		CloneProtocol:    cloneProtocol,
		HTTPTimeout:      httpTimeout,
		HTTPTimeoutSet:   httpTimeoutSet,
	}, nil
}

// MigrateLegacyToken moves a plaintext api_token from the YAML config file into
// the secret store and removes it from the file, preserving every other key.
// It reports whether a migration happened. The migration is one-time: the
// plaintext token is only removed after the secure write succeeds, and a failed
// config-file write rolls back the just-stored secret so no token is stranded
// in two places.
func MigrateLegacyToken(path string, store auth.SecretStore) (bool, error) {
	fc, err := LoadFileConfig(path)
	if err != nil {
		return false, err
	}
	token := strings.TrimSpace(fc.APIToken)
	if token == "" {
		return false, nil
	}
	email := strings.TrimSpace(fc.Email)
	if email == "" {
		return false, fmt.Errorf("cannot migrate api_token to the credential store: config file %s has no email", path)
	}
	if av, ok := store.(auth.AvailabilityStore); ok {
		if err := av.Available(); err != nil {
			return false, err
		}
	}
	// Snapshot any secret already stored under email so a failed config write
	// restores it rather than deleting a previously valid credential.
	prevSecret, prevExisted, err := auth.SnapshotSecret(store, email)
	if err != nil {
		return false, fmt.Errorf("inspect previous credential: %w", err)
	}
	if err := store.Set(email, token); err != nil {
		return false, fmt.Errorf("save token to credential store: %w", err)
	}
	fc.APIToken = ""
	if err := WriteFileConfig(path, fc); err != nil {
		if rbErr := auth.RestoreSecret(store, email, prevSecret, prevExisted); rbErr != nil {
			return false, fmt.Errorf("remove plaintext token from %s: %w (additionally failed to restore the previous credential: %v)", path, err, rbErr)
		}
		return false, fmt.Errorf("remove plaintext token from %s: %w", path, err)
	}
	return true, nil
}

func configHint(path string) string {
	if strings.TrimSpace(path) == "" {
		return "a config file"
	}
	return path
}

// GitRepoRefFrom inspects the `origin` remote in gitCwd and returns the
// workspace/repo if it points at bitbucket.org. All errors (no git, no remote,
// non-Bitbucket remote, parse failures) are swallowed and reported as ok=false.
func GitRepoRefFrom(gitCwd string) (RepoRef, bool) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	if gitCwd != "" {
		cmd.Dir = gitCwd
	}
	out, err := cmd.Output()
	if err != nil {
		return RepoRef{}, false
	}

	url := strings.TrimSpace(string(out))
	if url == "" {
		return RepoRef{}, false
	}
	url = strings.TrimSuffix(url, ".git")

	if !strings.Contains(url, "bitbucket.org") {
		return RepoRef{}, false
	}

	var remaining string
	switch {
	case strings.Contains(url, "bitbucket.org:"):
		remaining = strings.SplitN(url, "bitbucket.org:", 2)[1]
	case strings.Contains(url, "bitbucket.org/"):
		remaining = strings.SplitN(url, "bitbucket.org/", 2)[1]
	default:
		return RepoRef{}, false
	}

	parts := strings.Split(remaining, "/")
	if len(parts) < 2 {
		return RepoRef{}, false
	}
	repoSlug := parts[len(parts)-1]
	workspace := parts[len(parts)-2]
	if workspace == "" || repoSlug == "" {
		return RepoRef{}, false
	}
	return RepoRef{Workspace: workspace, RepoSlug: repoSlug}, true
}

// ResolveRepoRef resolves a workspace/repo, preferring explicit input over the
// config defaults. It errors when neither source yields both values.
func ResolveRepoRef(input RepoRef, cfg Config) (ResolvedRepoRef, error) {
	workspace := strings.TrimSpace(input.Workspace)
	if workspace == "" {
		workspace = cfg.DefaultWorkspace
	}
	repoSlug := strings.TrimSpace(input.RepoSlug)
	if repoSlug == "" {
		repoSlug = cfg.DefaultRepo
	}

	if workspace == "" || repoSlug == "" {
		return ResolvedRepoRef{}, fmt.Errorf("Provide workspace and repo via --workspace/--repo, or set BITBUCKET_DEFAULT_WORKSPACE and BITBUCKET_DEFAULT_REPO, or run inside a Bitbucket git repository.")
	}

	return ResolvedRepoRef{Workspace: workspace, RepoSlug: repoSlug}, nil
}
