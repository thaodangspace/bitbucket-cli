// Package config resolves Bitbucket credentials and repo defaults from, in
// order of precedence: environment variables, a YAML config file
// (~/.config/bitbucket-cli.yaml by default), and finally the local git remote
// for the default workspace/repo.
package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	CredentialSource CredentialSource
	Auth             auth.Provider
	DefaultWorkspace string
	DefaultRepo      string
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
	DefaultWorkspace string `yaml:"default_workspace,omitempty"`
	DefaultRepo      string `yaml:"default_repo,omitempty"`
}

// FileKeys are the keys settable in the config file, in display order.
var FileKeys = []string{"email", "token_type", "api_token", "default_workspace", "default_repo"}

func (fc *FileConfig) field(key string) (*string, error) {
	switch key {
	case "email":
		return &fc.Email, nil
	case "api_token":
		return &fc.APIToken, nil
	case "token_type":
		return &fc.TokenType, nil
	case "default_workspace":
		return &fc.DefaultWorkspace, nil
	case "default_repo":
		return &fc.DefaultRepo, nil
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
func resolveToken(env map[string]string, file FileConfig, store auth.SecretStore, email string) (string, CredentialSource, error) {
	if t := trimmed(env, "BITBUCKET_API_TOKEN"); t != "" {
		return t, SourceEnv, nil
	}
	if t := strings.TrimSpace(file.APIToken); t != "" {
		return t, SourceFile, nil
	}
	if email != "" {
		if t, err := store.Get(email); err == nil {
			return strings.TrimSpace(t), SourceKeychain, nil
		} else if !errors.Is(err, auth.ErrNotFound) {
			// A broken credential store must not break env/file automation;
			// treat it as "no keychain token" rather than failing.
			return "", "", nil
		}
	}
	return "", "", nil
}

// LoadConfig builds a Config from the given environment map, the YAML config
// file at configPath (pass "" to skip), and the local git remote. gitCwd is the
// directory used for git remote auto-detection of the default workspace/repo
// (pass "" for the current process directory). Precedence is env > file > git.
// Email and the API token are required; everything else is optional.
func LoadConfig(env map[string]string, gitCwd, configPath string, opts ...LoadOption) (Config, error) {
	o := &LoadOptions{SecretStore: auth.CurrentStore()}
	for _, opt := range opts {
		opt(o)
	}

	file, err := LoadFileConfig(configPath)
	if err != nil {
		return Config{}, err
	}

	email := firstNonEmpty(env["BITBUCKET_EMAIL"], file.Email)

	token, source, err := resolveToken(env, file, o.SecretStore, email)
	if err != nil {
		return Config{}, err
	}

	if email == "" || token == "" {
		return Config{}, fmt.Errorf("Set BITBUCKET_EMAIL and BITBUCKET_API_TOKEN (via environment or %s) before using bitbucket-cli.", configHint(configPath))
	}

	tokenType := firstNonEmpty(env["BITBUCKET_TOKEN_TYPE"], file.TokenType)
	if tokenType == "" {
		tokenType = string(auth.TokenAPI)
	}

	workspace := firstNonEmpty(env["BITBUCKET_DEFAULT_WORKSPACE"], file.DefaultWorkspace)
	repo := firstNonEmpty(env["BITBUCKET_DEFAULT_REPO"], file.DefaultRepo)

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
		CredentialSource: source,
		Auth:             auth.ProviderFor(auth.TokenType(tokenType), email, token),
		DefaultWorkspace: workspace,
		DefaultRepo:      repo,
	}, nil
}

// MigrateLegacyToken moves a plaintext api_token from the YAML config file into
// the secret store and removes it from the file, preserving every other key.
// It reports whether a migration happened. The migration is one-time: after a
// successful write the file no longer contains api_token.
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
	if err := store.Set(email, token); err != nil {
		return false, fmt.Errorf("save token to credential store: %w", err)
	}
	fc.APIToken = ""
	if err := WriteFileConfig(path, fc); err != nil {
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
