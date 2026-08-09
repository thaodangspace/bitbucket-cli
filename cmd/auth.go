package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/auth"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

// Auth commands: gh-style login/status/logout/token backed by the OS
// credential store. Tokens never appear in command arguments, logs, or output
// (except the explicit `auth token` opt-in).

func init() {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage bitbucket-cli authentication",
		Long: "Manage bitbucket-cli credentials (gh-style auth workflow). " +
			"Tokens are validated against Bitbucket before being saved to the OS " +
			"credential store; non-secret profile data lives in the YAML config file.",
	}

	// auth login — prompt or --with-token, validate, save.
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Bitbucket and save credentials securely",
		Long: "Authenticate with Bitbucket Cloud. API tokens prompt for the Atlassian " +
			"account email; access and OAuth bearer tokens do not require one. Reads the " +
			"token from stdin when --with-token is given and validates it before saving " +
			"to the OS credential store. Access tokens are validated against the selected " +
			"repository rather than /user. This is a write operation; use only when the " +
			"user has asked to log in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			email := strings.TrimSpace(flagLoginEmail)
			tt, err := parseTokenType(flagLoginTokenType)
			if err != nil {
				return fail(err)
			}

			path := configPath()
			if path == "" {
				return fail(fmt.Errorf("could not resolve a config file path"))
			}

			interactive := stdinIsTTY()
			// API tokens represent an Atlassian account and require an email.
			// Resource-scoped access tokens and OAuth bearer tokens do not.
			if tt == auth.TokenAPI && email == "" {
				if !interactive {
					return fail(fmt.Errorf("Provide --email when stdin is not a TTY for an API token."))
				}
				email, err = prompt("Bitbucket account email: ")
				if err != nil {
					return fail(err)
				}
				email = strings.TrimSpace(email)
			}
			if tt == auth.TokenAPI && email == "" {
				return fail(fmt.Errorf("email is required for an API token"))
			}

			var token string
			if flagLoginWithToken {
				b, rerr := io.ReadAll(os.Stdin)
				if rerr != nil {
					return fail(fmt.Errorf("read token from stdin: %w", rerr))
				}
				token = strings.TrimSpace(string(b))
			} else {
				if !interactive {
					return fail(fmt.Errorf("Refusing to read a token when stdin is not a TTY; use --with-token to read it from stdin."))
				}
				token, err = prompt("Bitbucket token: ")
				if err != nil {
					return fail(err)
				}
				token = strings.TrimSpace(token)
			}
			if token == "" {
				return fail(fmt.Errorf("token is required"))
			}

			provider := auth.ProviderFor(tt, email, token)
			if provider == nil {
				return fail(fmt.Errorf("unsupported token type %q", tt))
			}

			// Validate before persisting anything. Access tokens are resource
			// credentials, so probe the selected repository instead of /user.
			var validationErr error
			if tt == auth.TokenAccess {
				ref, err := loginValidationRepo(path)
				if err != nil {
					return fail(err)
				}
				validationErr = validateAccessCredentials(cmd.Context(), provider, ref)
			} else {
				validationErr = validateCredentials(cmd.Context(), provider)
			}
			if validationErr != nil {
				return fail(validationErr)
			}

			// Store identity only for account credentials. Access/OAuth token
			// profiles use stable credential keys and do not invent an account.
			if err := config.SetFileValue(path, "email", func() string {
				if tt == auth.TokenAPI {
					return email
				}
				return ""
			}()); err != nil {
				return fail(err)
			}
			if err := config.SetFileValue(path, "token_type", string(tt)); err != nil {
				return fail(err)
			}
			// A legacy api_token is an API-profile format. Remove it when
			// selecting a bearer profile so it cannot shadow the new keychain
			// credential; API login migrates it below for compatibility.
			if tt != auth.TokenAPI {
				if err := config.SetFileValue(path, "api_token", ""); err != nil {
					return fail(err)
				}
			}
			// One-time cleanup of a legacy plaintext API token. It is an API
			// profile format and must retain its email-based keychain key.
			if tt == auth.TokenAPI {
				if migrated, err := config.MigrateLegacyToken(path, auth.CurrentStore()); err != nil {
					return fail(err)
				} else if migrated {
					_, _ = fmt.Fprintln(os.Stderr, "Migrated legacy plaintext api_token to the credential store.")
				}
			}
			storeKey, err := config.CredentialStoreKey(tt, email)
			if err != nil {
				return fail(err)
			}
			if err := auth.CurrentStore().Set(storeKey, token); err != nil {
				return fail(err)
			}

			if flagPretty {
				if email != "" {
					_, err := fmt.Fprintf(os.Stdout, "Logged in to Bitbucket as %s.\n", email)
					return err
				}
				_, err := fmt.Fprintln(os.Stdout, "Logged in to Bitbucket.")
				return err
			}
			return output.RenderJSON(os.Stdout, map[string]any{"authenticated": true, "account": nullableString(email)})
		},
	}
	loginCmd.Flags().StringVar(&flagLoginEmail, "email", "", "Atlassian account email")
	loginCmd.Flags().BoolVar(&flagLoginWithToken, "with-token", false, "Read the token from stdin instead of prompting")
	loginCmd.Flags().StringVar(&flagLoginTokenType, "token-type", string(auth.TokenAPI), "Token type: api, access, or oauth")

	// auth status — report authentication state; never prints the token.
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Report authentication status and credential source",
		Long: "Report the authenticated Bitbucket account, where the credential came " +
			"from (env, config file, or credential store), the token type, and whether " +
			"the token can read the resolved repository. Never prints the token.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagStatusJSON {
				flagPretty = false
			}
			return runAuthStatus(cmd.Context())
		},
	}
	statusCmd.Flags().BoolVar(&flagStatusJSON, "json", false, "Force JSON output (default)")

	// auth logout — remove the stored profile; never touches env vars.
	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials from the config file and credential store",
		Long: "Log out of Bitbucket by removing the stored profile (email and token " +
			"from the config file plus the token from the credential store). Does not " +
			"mutate environment variables. This is a write operation; use only when " +
			"the user has asked to log out.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := configPath()
			fc, err := config.LoadFileConfig(path)
			if err != nil {
				return fail(err)
			}
			email := strings.TrimSpace(fc.Email)
			if email == "" {
				email = strings.TrimSpace(os.Getenv("BITBUCKET_EMAIL"))
			}
			tokenType := strings.TrimSpace(fc.TokenType)
			if tokenType == "" {
				tokenType = strings.TrimSpace(os.Getenv("BITBUCKET_TOKEN_TYPE"))
			}
			if tokenType == "" {
				tokenType = string(auth.TokenAPI)
			}
			tt, err := parseTokenType(tokenType)
			if err != nil {
				return fail(err)
			}
			storeKey, keyErr := config.CredentialStoreKey(tt, email)
			hasProfile := email != "" || strings.TrimSpace(fc.TokenType) != ""
			if keyErr != nil && !flagLogoutYes {
				return fail(fmt.Errorf("no stored profile to log out"))
			}
			if !hasProfile && !flagLogoutYes {
				return fail(fmt.Errorf("no stored profile to log out"))
			}

			label := email
			if label == "" {
				label = string(tt) + " credential"
			}
			if !flagLogoutYes && stdinIsTTY() {
				confirm, cerr := prompt(fmt.Sprintf("Log out of Bitbucket as %s? [y/N] ", label))
				if cerr != nil {
					return fail(cerr)
				}
				if !strings.EqualFold(strings.TrimSpace(confirm), "y") {
					if flagPretty {
						_, werr := fmt.Fprintln(os.Stdout, "Logout cancelled.")
						return werr
					}
					return output.RenderJSON(os.Stdout, map[string]any{"cancelled": true})
				}
			} else if !flagLogoutYes {
				return fail(fmt.Errorf("stdin is not a TTY; pass --yes to log out"))
			}

			if keyErr == nil {
				if err := auth.CurrentStore().Delete(storeKey); err != nil {
					return fail(err)
				}
			}
			changed := false
			if strings.TrimSpace(fc.Email) != "" {
				if err := config.SetFileValue(path, "email", ""); err != nil {
					return fail(err)
				}
				changed = true
			}
			if strings.TrimSpace(fc.TokenType) != "" {
				if err := config.SetFileValue(path, "token_type", ""); err != nil {
					return fail(err)
				}
				changed = true
			}
			if strings.TrimSpace(fc.APIToken) != "" {
				if err := config.SetFileValue(path, "api_token", ""); err != nil {
					return fail(err)
				}
				changed = true
			}

			if flagPretty {
				_, err := fmt.Fprintf(os.Stdout, "Logged out of Bitbucket.%s\n", func() string {
					if envSet() {
						return " Environment credentials remain in effect."
					}
					return ""
				}())
				return err
			}
			return output.RenderJSON(os.Stdout, map[string]any{"loggedOut": true, "account": nullableString(email), "changed": changed})
		},
	}
	logoutCmd.Flags().BoolVar(&flagLogoutYes, "yes", false, "Skip confirmation (required when stdin is not a TTY)")

	// auth token — opt-in token output for scripting.
	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Print the active token (opt-in, for scripting)",
		Long: "Print the resolved token to stdout. This is the only command that " +
			"reveals the token and is intended for scripts that need to build their " +
			"own Authorization headers.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fail(err)
			}
			_, err = fmt.Fprintln(os.Stdout, cfg.APIToken)
			return err
		},
	}

	authCmd.AddCommand(loginCmd, statusCmd, logoutCmd, tokenCmd)
	rootCmd.AddCommand(authCmd)
}

// auth login flags.
var (
	flagLoginEmail     string
	flagLoginWithToken bool
	flagLoginTokenType string
	flagStatusJSON     bool
	flagLogoutYes      bool
)

// parseTokenType validates --token-type against the supported values.
func parseTokenType(s string) (auth.TokenType, error) {
	switch strings.TrimSpace(s) {
	case string(auth.TokenAPI):
		return auth.TokenAPI, nil
	case string(auth.TokenAccess):
		return auth.TokenAccess, nil
	case string(auth.TokenOAuth):
		return auth.TokenOAuth, nil
	default:
		return auth.TokenNone, fmt.Errorf("invalid --token-type %q: must be api, access, or oauth", s)
	}
}

// stdinIsTTY reports whether stdin is attached to a terminal.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// prompt reads a single line from stdin after writing prompt to stderr.
func prompt(p string) (string, error) {
	_, err := fmt.Fprint(os.Stderr, p)
	if err != nil {
		return "", err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read input: %w", err)
	}
	return line, nil
}

// validateCredentials checks account credentials against GET /2.0/user.
// Access-token validation is deliberately handled by validateAccessCredentials.
func validateCredentials(ctx context.Context, p auth.Provider) error {
	client := newAuthClient(p)
	var user struct {
		AccountID string `json:"account_id"`
	}
	if err := client.Request(ctx, "/user", bitbucket.RequestOptions{}, &user); err != nil {
		return err
	}
	if user.AccountID == "" {
		return fmt.Errorf("Bitbucket accepted the credential but returned no account ID")
	}
	return nil
}

// loginValidationRepo resolves an access-token probe target without requiring
// credentials to be loaded from the config first.
func loginValidationRepo(path string) (config.ResolvedRepoRef, error) {
	fc, err := config.LoadFileConfig(path)
	if err != nil {
		return config.ResolvedRepoRef{}, err
	}
	env := envMap()
	cfg := config.Config{
		DefaultWorkspace: strings.TrimSpace(env["BITBUCKET_DEFAULT_WORKSPACE"]),
		DefaultRepo:      strings.TrimSpace(env["BITBUCKET_DEFAULT_REPO"]),
	}
	if cfg.DefaultWorkspace == "" {
		cfg.DefaultWorkspace = strings.TrimSpace(fc.DefaultWorkspace)
	}
	if cfg.DefaultRepo == "" {
		cfg.DefaultRepo = strings.TrimSpace(fc.DefaultRepo)
	}
	ref, _, err := resolveRepo(cfg)
	if err != nil {
		return config.ResolvedRepoRef{}, fmt.Errorf("Access-token validation requires a repository context; pass -R workspace/repo or run inside a Bitbucket repository.")
	}
	return ref, nil
}

// validateAccessCredentials probes a concrete repository, which works for
// repository-, project-, and workspace-scoped access tokens without inventing
// an Atlassian user identity.
func validateAccessCredentials(ctx context.Context, p auth.Provider, ref config.ResolvedRepoRef) error {
	client := newAuthClient(p)
	path := fmt.Sprintf("/repositories/%s/%s",
		bitbucket.EncodePathSegment(ref.Workspace),
		bitbucket.EncodePathSegment(ref.RepoSlug))
	return client.Request(ctx, path, bitbucket.RequestOptions{}, nil)
}

// newAuthClient builds a client with the test transport when injected.
func newAuthClient(p auth.Provider) *bitbucket.Client {
	var opts []bitbucket.Option
	if testTransport != nil {
		opts = append(opts, bitbucket.WithHTTPClient(&http.Client{Transport: testTransport}))
	}
	return bitbucket.NewClient(p, opts...)
}

// runAuthStatus renders authentication state. It never fails for missing
// credentials: it reports them.
func runAuthStatus(ctx context.Context) error {
	env := envMap()
	path := configPath()
	cfg, loadErr := config.LoadConfig(env, "", path)

	email := ""
	if loadErr == nil {
		// Resource-scoped credentials do not have an Atlassian account identity.
		if cfg.TokenType != string(auth.TokenAccess) {
			email = cfg.Email
		}
	} else {
		fc, _ := config.LoadFileConfig(path)
		email = strings.TrimSpace(fc.Email)
	}

	loggedIn := loadErr == nil
	source := ""
	tokenType := ""
	defaultRepo := ""
	if loggedIn {
		source = string(cfg.CredentialSource)
		tokenType = cfg.TokenType
		if cfg.DefaultWorkspace != "" && cfg.DefaultRepo != "" {
			defaultRepo = cfg.DefaultWorkspace + "/" + cfg.DefaultRepo
		}
	}

	// Probe account credentials at /user and resource credentials at the
	// resolved repository. Both probes are read-only and best effort.
	user := map[string]any{}
	repoAccess := any(nil)
	if loggedIn {
		client := newAuthClient(cfg.Auth)
		if cfg.TokenType != string(auth.TokenAccess) {
			var u map[string]any
			if err := client.Request(ctx, "/user", bitbucket.RequestOptions{}, &u); err == nil {
				user = u
			} else {
				user = map[string]any{"error": redactString(err.Error(), cfg.APIToken)}
			}
		}
		if cfg.DefaultWorkspace != "" && cfg.DefaultRepo != "" {
			repoPath := fmt.Sprintf("/repositories/%s/%s",
				bitbucket.EncodePathSegment(cfg.DefaultWorkspace),
				bitbucket.EncodePathSegment(cfg.DefaultRepo))
			var repo map[string]any
			if err := client.Request(ctx, repoPath, bitbucket.RequestOptions{}, &repo); err == nil {
				repoAccess = true
			} else if he, ok := err.(*bitbucket.HTTPError); ok && he.Status == http.StatusForbidden {
				repoAccess = "forbidden"
			} else {
				repoAccess = "unreadable"
			}
		}
	}

	// Detect a legacy plaintext token pending migration.
	migratable := false
	if fc, err := config.LoadFileConfig(path); err == nil && strings.TrimSpace(fc.APIToken) != "" {
		migratable = true
	}

	displayName := email
	if n, ok := user["display_name"].(string); ok && n != "" {
		displayName = n
	}
	accountID := ""
	if a, ok := user["account_id"].(string); ok {
		accountID = a
	}

	body := map[string]any{
		"loggedIn":         loggedIn,
		"account":          nullableString(email),
		"displayName":      nullableString(displayName),
		"accountId":        nullableString(accountID),
		"credentialSource": nullableString(source),
		"tokenType":        nullableString(tokenType),
		"defaultRepo":      nullableString(defaultRepo),
		"repoAccess":       repoAccess,
		"migratable":       migratable,
	}

	if flagPretty {
		if !loggedIn {
			_, err := fmt.Fprintln(os.Stdout, "Not logged in to Bitbucket.")
			return err
		}
		lines := []string{}
		if displayName != "" {
			lines = append(lines, fmt.Sprintf("Logged in to Bitbucket as %s", displayName))
		} else {
			lines = append(lines, "Logged in to Bitbucket")
		}
		lines = append(lines,
			fmt.Sprintf("Credential source: %s", source),
			fmt.Sprintf("Token type: %s", tokenType),
		)
		if defaultRepo != "" {
			lines = append(lines, fmt.Sprintf("Default repo: %s", defaultRepo))
		}
		if repoAccess == true {
			lines = append(lines, "Repo access: readable")
		} else if repoAccess != nil {
			lines = append(lines, fmt.Sprintf("Repo access: %v", repoAccess))
		}
		if migratable {
			lines = append(lines, "Legacy plaintext api_token found; run `auth login` to migrate it.")
		}
		return output.RenderLines(os.Stdout, lines, "")
	}
	return output.RenderJSON(os.Stdout, body)
}

// redactString masks a known secret within an error string.
func redactString(s, secret string) string {
	return auth.Redact(s, secret)
}

// envSet reports whether credential environment variables are present.
func envSet() bool {
	return strings.TrimSpace(os.Getenv("BITBUCKET_EMAIL")) != "" &&
		strings.TrimSpace(os.Getenv("BITBUCKET_API_TOKEN")) != ""
}
