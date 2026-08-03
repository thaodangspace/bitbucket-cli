// Package auth provides the credential abstraction for bitbucket-cli:
// an AuthProvider that attaches a credential to outbound HTTP requests, and a
// SecretStore that persists secrets in the OS credential store.
//
// The wire semantics follow the Bitbucket Cloud authentication model:
//   - API token:   HTTP Basic with the Atlassian account email as the username
//     and the API token as the password.
//   - access token: HTTP Basic per Bitbucket's documented access-token format.
//   - OAuth bearer: an "Authorization: Bearer <token>" header.
//
// Tokens are never placed in logs, error excerpts, URLs, or process arguments.
package auth

import (
	"net/http"
	"strings"
)

// TokenType enumerates the supported credential kinds.
type TokenType string

// Supported token types, surfaced in `auth status` and stored in config.
const (
	TokenAPI    TokenType = "api"    // Atlassian account email + API token (Basic).
	TokenAccess TokenType = "access" // repository/project/workspace access token (Basic).
	TokenOAuth  TokenType = "oauth"  // OAuth bearer token.
	TokenNone   TokenType = ""       // no credential stored.
)

// Provider attaches an Authorization header to an outbound request.
type Provider interface {
	// Apply sets the Authorization (and any related) header on req.
	Apply(r *http.Request) error
	// Kind describes the credential type ("" or one of the TokenType values).
	Kind() string
}

// SecretRevealer is implemented by providers that can disclose their secret
// token so callers can redact it from errors and logs. Only the client uses
// this; the value is never printed.
type SecretRevealer interface {
	Secret() string
}

// BasicAuth is an HTTP Basic credential: username and password.
type BasicAuth struct {
	Username string
	Password string
	kind     string
}

// NewBasicAuth returns a Basic credential with the given Kind label.
func NewBasicAuth(kind TokenType, username, password string) *BasicAuth {
	return &BasicAuth{Username: username, Password: password, kind: string(kind)}
}

func (b *BasicAuth) Apply(r *http.Request) error {
	r.SetBasicAuth(b.Username, b.Password)
	return nil
}

func (b *BasicAuth) Kind() string { return b.kind }

// Secret returns the password (the token) so callers can redact it.
func (b *BasicAuth) Secret() string { return b.Password }

// BearerAuth is an OAuth bearer token credential.
type BearerAuth struct {
	Token string
}

func (b *BearerAuth) Apply(r *http.Request) error {
	r.Header.Set("Authorization", "Bearer "+b.Token)
	return nil
}

func (b *BearerAuth) Kind() string { return string(TokenOAuth) }

// Secret returns the bearer token so callers can redact it.
func (b *BearerAuth) Secret() string { return b.Token }

// newBasicToken builds the Basic credential used for API tokens: the
// Atlassian account email as username and the API token as password.
func apiTokenProvider(email, token string) Provider {
	return NewBasicAuth(TokenAPI, email, token)
}

// accessTokenProvider builds the Basic credential used for Bitbucket access
// tokens (repository/project/workspace). Per Bitbucket's documented access
// token Basic auth format, the token is sent as the username and the literal
// password marker is "x-token-auth".
func accessTokenProvider(token string) Provider {
	return NewBasicAuth(TokenAccess, token, "x-token-auth")
}

// ProviderFor returns the Provider for a token type and credential parts.
func ProviderFor(t TokenType, email, token string) Provider {
	switch t {
	case TokenOAuth:
		return &BearerAuth{Token: token}
	case TokenAccess:
		return accessTokenProvider(token)
	default:
		return apiTokenProvider(email, token)
	}
}

// Redact returns a copy of s with every occurrence of secret replaced by
// "<redacted>". It is used to keep tokens out of errors, logs, and output.
func Redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "<redacted>")
}

// ProviderKind reports the token-type label for a provider, falling back to a
// default when the provider does not expose one.
func ProviderKind(p Provider) string {
	if p == nil {
		return string(TokenNone)
	}
	return p.Kind()
}
