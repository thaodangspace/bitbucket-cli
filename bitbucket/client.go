// Package bitbucket is a thin client for the Bitbucket Cloud REST 2.0 API.
// It mirrors the request, pagination, and error semantics of the original Pi
// extension client.
package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thaodangspace/bitbucket-cli/auth"
)

// APIBaseURL is the Bitbucket Cloud REST 2.0 base.
const APIBaseURL = "https://api.bitbucket.org/2.0"

// Default pagination caps, matching the original extension.
const (
	DefaultLimit    = 20
	DefaultPageLen  = 50
	DefaultMaxPages = 10
)

// Request and transport defaults prevent stalled connections from hanging
// ordinary CLI operations forever. Streaming requests use the transport
// phase limits but do not use DefaultRequestTimeout as a total lifetime.
const (
	DefaultRequestTimeout        = 30 * time.Second
	DefaultDialTimeout           = 10 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 30 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second
)

// excerptLimit bounds how much of an error response body is surfaced.
const excerptLimit = 500

// ScopeHint is the credential-aware, conservative permission guidance for an
// endpoint. Scopes are remediation hints, not live scope introspection.
type ScopeHint struct {
	CredentialKind auth.TokenType `json:"credentialKind"`
	Scopes         []string       `json:"scopes,omitempty"`
	Note           string         `json:"note,omitempty"`
}

// EndpointPermissions keeps permission names for each Bitbucket credential
// model separate. The names are not interchangeable: API tokens use the
// Atlassian `read:...:bitbucket` family, while bearer credentials use their
// resource/OAuth permission family.
type EndpointPermissions struct {
	APITokenScopes    []string
	AccessTokenScopes []string
	OAuthScopes       []string
	Note              string
}

const scopeHintNote = "This is documented remediation guidance, not live scope introspection; resource or event configuration may require additional permissions."

// RequiredScopes returns the documented permission hint for a GET operation.
func RequiredScopes(kind auth.TokenType, path string) ScopeHint {
	return RequiredScopesFor(kind, http.MethodGet, path)
}

// RequiredScopesFor returns the documented permission hint for an HTTP
// operation, selected for the active credential kind. Unknown kinds deliberately
// receive no credential-specific scopes rather than a potentially misleading
// guess.
func RequiredScopesFor(kind auth.TokenType, method, path string) ScopeHint {
	permissions := endpointPermissions(method, path)
	hint := ScopeHint{CredentialKind: kind, Note: permissions.Note}
	switch kind {
	case auth.TokenAPI:
		hint.Scopes = append([]string(nil), permissions.APITokenScopes...)
	case auth.TokenAccess:
		hint.Scopes = append([]string(nil), permissions.AccessTokenScopes...)
	case auth.TokenOAuth:
		hint.Scopes = append([]string(nil), permissions.OAuthScopes...)
	default:
		hint.Note = "The credential type is unknown; no credential-specific permission hint is available."
	}
	return hint
}

func permission(api, access, oauth []string) EndpointPermissions {
	return EndpointPermissions{APITokenScopes: api, AccessTokenScopes: access, OAuthScopes: oauth, Note: scopeHintNote}
}

func endpointPermissions(method, path string) EndpointPermissions {
	readRepo := permission([]string{"read:repository:bitbucket"}, []string{"repository:read"}, []string{"repository"})
	writeRepo := permission([]string{"read:repository:bitbucket", "write:repository:bitbucket"}, []string{"repository:read", "repository:write"}, []string{"repository", "repository:write"})
	adminRepo := permission([]string{"admin:repository:bitbucket"}, []string{"repository:admin"}, []string{"repository:admin"})
	deleteRepo := permission([]string{"delete:repository:bitbucket"}, []string{"repository:delete"}, []string{"repository:delete"})

	switch {
	case strings.Contains(path, "/hook_events"):
		return EndpointPermissions{Note: "The public event catalog normally requires no authentication."}
	case strings.Contains(path, "/hooks"):
		switch method {
		case http.MethodGet:
			return permission([]string{"read:webhook:bitbucket"}, []string{"webhook:read"}, []string{"webhook"})
		case http.MethodDelete:
			return permission([]string{"delete:webhook:bitbucket"}, []string{"webhook:delete"}, []string{"webhook:write"})
		default:
			return permission([]string{"read:webhook:bitbucket", "write:webhook:bitbucket"}, []string{"webhook:read", "webhook:write"}, []string{"webhook", "webhook:write"})
		}
	case strings.HasSuffix(strings.Split(path, "?")[0], "/user"):
		return permission([]string{"read:user:bitbucket"}, []string{"account:read"}, []string{"account"})
	case strings.Contains(path, "/ssh-keys"):
		switch method {
		case http.MethodGet:
			return permission([]string{"read:ssh-key:bitbucket"}, []string{"account:read"}, []string{"account"})
		case http.MethodDelete:
			return permission([]string{"delete:ssh-key:bitbucket"}, []string{"account:write"}, []string{"account:write"})
		default:
			return permission([]string{"read:ssh-key:bitbucket", "write:ssh-key:bitbucket"}, []string{"account:read", "account:write"}, []string{"account", "account:write"})
		}
	case strings.Contains(path, "/deploy-keys"):
		switch method {
		case http.MethodGet:
			return permission([]string{"admin:repository:bitbucket"}, []string{"repository:admin"}, []string{"repository:admin"})
		case http.MethodDelete:
			return permission([]string{"delete:ssh-key:bitbucket", "admin:repository:bitbucket"}, []string{"repository:admin"}, []string{"repository:admin"})
		default:
			return permission([]string{"write:ssh-key:bitbucket", "admin:repository:bitbucket"}, []string{"repository:admin"}, []string{"repository:admin"})
		}
	case strings.Contains(path, "/permissions-config/"):
		if method == http.MethodGet {
			return readRepo
		}
		if method == http.MethodDelete {
			return permission([]string{"admin:repository:bitbucket", "delete:permission:bitbucket"}, []string{"repository:admin"}, []string{"repository:admin"})
		}
		return permission([]string{"admin:repository:bitbucket", "write:permission:bitbucket"}, []string{"repository:admin"}, []string{"repository:admin"})
	case strings.Contains(path, "/permissions/repositories"):
		return permission([]string{"admin:workspace:bitbucket"}, []string{"workspace:admin"}, []string{"workspace:admin"})
	case strings.Contains(path, "/workspaces/") && strings.Contains(path, "/projects"):
		if method == http.MethodGet {
			return permission([]string{"read:project:bitbucket"}, []string{"project:read"}, []string{"project"})
		}
		return permission([]string{"admin:project:bitbucket"}, []string{"project:admin"}, []string{"project:admin"})
	case strings.Contains(path, "/workspaces/") && strings.Contains(path, "/members"):
		return permission([]string{"read:workspace:bitbucket"}, []string{"workspace:read"}, []string{"workspace"})
	case strings.Contains(path, "/reports") || strings.Contains(path, "/annotations"), strings.Contains(path, "/statuses"):
		if method == http.MethodGet {
			return readRepo
		}
		return writeRepo
	case strings.Contains(path, "/commit/") && (strings.Contains(path, "/approve") || strings.Contains(path, "/comments")):
		if method == http.MethodGet {
			return readRepo
		}
		return writeRepo
	case strings.Contains(path, "/downloads"):
		return permission([]string{"write:repository:bitbucket"}, []string{"repository:write"}, []string{"repository:write"})
	case strings.Contains(path, "/pullrequests"):
		if method == http.MethodGet {
			return permission([]string{"read:pullrequest:bitbucket"}, []string{"pullrequest:read"}, []string{"pullrequest"})
		}
		return permission([]string{"read:pullrequest:bitbucket", "write:pullrequest:bitbucket"}, []string{"pullrequest:read", "pullrequest:write"}, []string{"pullrequest", "pullrequest:write"})
	case strings.Contains(path, "/refs/branches"), strings.Contains(path, "/refs/tags"), strings.Contains(path, "/commits"):
		if method == http.MethodGet {
			return readRepo
		}
		return permission([]string{"write:repository:bitbucket"}, []string{"repository:write"}, []string{"repository:write"})
	case strings.Contains(path, "/branch-restrictions"), strings.Contains(path, "/branching-model/settings"):
		return adminRepo
	case strings.Contains(path, "/default-reviewers"):
		if method == http.MethodGet {
			return permission([]string{"read:pullrequest:bitbucket"}, []string{"pullrequest:read"}, []string{"pullrequest"})
		}
		return adminRepo
	case strings.Contains(path, "/pipelines-config/variables") || (strings.Contains(path, "/deployments/") && strings.Contains(path, "/variables")):
		if method == http.MethodGet {
			return permission([]string{"read:pipeline:bitbucket"}, []string{"pipeline:read"}, []string{"pipeline:read"})
		}
		return permission([]string{"admin:pipeline:bitbucket"}, []string{"pipeline:write"}, []string{"pipeline:write"})
	case strings.Contains(path, "/pipelines-config/runners"):
		switch method {
		case http.MethodGet:
			return permission([]string{"read:runner:bitbucket"}, []string{"runner:read"}, []string{"runner"})
		case http.MethodPost, http.MethodPut:
			return permission([]string{"read:runner:bitbucket", "write:runner:bitbucket"}, []string{"runner:read", "runner:write"}, []string{"runner", "runner:write"})
		default:
			return permission([]string{"write:runner:bitbucket"}, []string{"runner:write"}, []string{"runner:write"})
		}
	case strings.Contains(path, "/pipeline"):
		if method == http.MethodGet {
			return permission([]string{"read:pipeline:bitbucket"}, []string{"pipeline:read"}, []string{"pipeline:read"})
		}
		return permission([]string{"write:pipeline:bitbucket"}, []string{"pipeline:write"}, []string{"pipeline:write"})
	case strings.Contains(path, "/repositories/") && strings.HasSuffix(strings.Split(strings.Split(path, "?")[0], "/repositories/")[1], "/forks") && method == http.MethodPost:
		return writeRepo
	case repositoryResourcePath(path) && method == http.MethodPost, repositoryResourcePath(path) && method == http.MethodPut:
		return adminRepo
	case repositoryResourcePath(path) && method == http.MethodDelete:
		return deleteRepo
	case strings.Contains(path, "/repositories") && method != http.MethodGet:
		return permission([]string{"write:repository:bitbucket"}, []string{"repository:write"}, []string{"repository:write"})
	case strings.Contains(path, "/workspaces"):
		return permission([]string{"read:workspace:bitbucket"}, []string{"workspace:read"}, []string{"workspace"})
	case method == http.MethodGet:
		return readRepo
	default:
		return adminRepo
	}
}

func repositoryResourcePath(path string) bool {
	base := strings.Split(path, "?")[0]
	parts := strings.Split(strings.Trim(strings.TrimPrefix(base, APIBaseURL), "/"), "/")
	for i := range parts {
		if parts[i] == "repositories" {
			return len(parts)-i-1 == 2
		}
	}
	return false
}

// HTTPError is a normalized non-2xx response from Bitbucket. RequiredScopes
// is selected when the error is constructed, using the active provider kind.
type HTTPError struct {
	Method         string   `json:"method"`
	URL            string   `json:"url"`
	Status         int      `json:"status"`
	StatusText     string   `json:"statusText"`
	Excerpt        string   `json:"excerpt"`
	CredentialKind string   `json:"credentialKind,omitempty"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
	ScopeNote      string   `json:"scopeNote,omitempty"`
}

func (e *HTTPError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "Bitbucket authentication failed. Run `bitbucket-cli auth login` or set BITBUCKET_EMAIL and BITBUCKET_API_TOKEN."
	case http.StatusForbidden:
		label := credentialLabel(auth.TokenType(e.CredentialKind))
		if label == "" {
			return "Bitbucket authorization failed. The credential type is unknown; check the request and account access."
		}
		if len(e.RequiredScopes) == 0 {
			if strings.Contains(e.ScopeNote, "public event catalog") {
				return "Bitbucket authorization failed for a public endpoint; check the request and account access."
			}
			return fmt.Sprintf("Bitbucket authorization failed for %s. No credential-specific permission hint is available; check the request and account access.", label)
		}
		message := fmt.Sprintf("Bitbucket authorization failed for %s. This operation normally requires %s; check the token's granted permissions.", label, strings.Join(e.RequiredScopes, " and "))
		if e.ScopeNote != "" {
			message += " " + e.ScopeNote
		}
		return message
	case http.StatusNotFound:
		return "Bitbucket resource not found. Check workspace, repo, and IDs."
	case http.StatusTooManyRequests:
		return "Bitbucket rate limit reached. Retry later."
	default:
		return fmt.Sprintf("Bitbucket request failed with %d %s: %s", e.Status, e.StatusText, e.Excerpt)
	}
}

func credentialLabel(kind auth.TokenType) string {
	switch kind {
	case auth.TokenAPI:
		return "an API token"
	case auth.TokenAccess:
		return "a Bitbucket access token"
	case auth.TokenOAuth:
		return "an OAuth token"
	default:
		return ""
	}
}

// EncodePathSegment percent-encodes a single path segment (slashes included).
func EncodePathSegment(value string) string {
	return url.PathEscape(value)
}

// Client talks to the Bitbucket Cloud API using an auth.Provider.
type Client struct {
	auth           auth.Provider
	http           *http.Client
	requestTimeout time.Duration
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient injects a custom *http.Client (used in tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithRequestTimeout sets the total timeout for ordinary requests. A zero
// duration explicitly disables the ordinary total timeout; transport phase
// timeouts still apply. Streaming requests ignore this value.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout >= 0 {
			c.requestTimeout = timeout
		}
	}
}

// WithTimeout is retained for compatibility and is an alias for
// WithRequestTimeout. It no longer sets http.Client.Timeout so that streaming
// requests can remain open beyond the ordinary request lifetime.
func WithTimeout(timeout time.Duration) Option {
	return WithRequestTimeout(timeout)
}

// WithAuth injects the credential provider to use for requests. Defaults to
// Basic API-token auth if not provided.
func WithAuth(a auth.Provider) Option {
	return func(c *Client) { c.auth = a }
}

// NewClient builds a Client using the given auth provider.
func NewClient(a auth.Provider, opts ...Option) *Client {
	httpClient := *http.DefaultClient
	c := &Client{auth: a, http: &httpClient, requestTimeout: DefaultRequestTimeout}
	for _, opt := range opts {
		opt(c)
	}
	// Total request lifetime is controlled per request via context so
	// streaming responses are not cut off by http.Client.Timeout.
	c.http.Timeout = 0
	configureTransport(c.http)
	if c.http.CheckRedirect == nil {
		c.http.CheckRedirect = safeRedirect
	}
	if c.auth == nil {
		// No provider configured: an API token requires an email, so fall
		// back to an empty Basic credential (the API will reject it). This
		// keeps NewClient usable when only transport config matters (tests).
		c.auth = auth.NewBasicAuth(auth.TokenAPI, "", "")
	}
	return c
}

// RequestOptions configures a single request.
type RequestOptions struct {
	Method string
	// Streaming disables the ordinary total request timeout. Transport
	// connection and response-header protections still apply.
	Streaming bool
	// Body is JSON-encoded into the request body. It must not be set together
	// with RawBody.
	Body any
	// RawBody is sent verbatim as the request body, taking precedence over
	// Body. Use it for raw JSON strings, diffs, or text payloads.
	RawBody []byte
	// Headers are additional request headers. A default Accept header of
	// "application/json" and, when a body is present, "Content-Type:
	// application/json" are applied only when not already set here.
	Headers http.Header
	// Query are extra query parameters appended to the resolved URL.
	Query url.Values
}

// Response is the raw result of a request: the status, response headers, and a
// streamed body the caller is responsible for closing.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// retryableStatuses are HTTP statuses retried for idempotent methods.
var retryableStatuses = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:          true, // 502
	http.StatusServiceUnavailable:  true, // 503
	http.StatusGatewayTimeout:      true, // 504
}

// maxRetries bounds how often an idempotent request is retried.
const maxRetries = 2

// initialRetryDelay and retryDelayFactor back off retries exponentially.
const (
	initialRetryDelay = 200 * time.Millisecond
	retryDelayFactor  = 2
)

// isIdempotent reports whether a method is safe to retry automatically.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// buildRequest assembles an *http.Request from method/path/options, applying
// options headers, a default Accept header, auth, and Content-Type for bodies.
// The returned request is safe to reuse across retry attempts.
func (c *Client) buildRequest(ctx context.Context, method, pathOrURL string, opts RequestOptions) (*http.Request, error) {
	fullURL, err := buildURL(pathOrURL)
	if err != nil {
		return nil, err
	}
	if len(opts.Query) > 0 {
		u, err := url.Parse(fullURL)
		if err != nil {
			return nil, fmt.Errorf("parse request URL: %w", err)
		}
		q := u.Query()
		for k, vs := range opts.Query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
		fullURL = u.String()
	}

	var bodyReader io.Reader
	if opts.RawBody != nil {
		bodyReader = bytes.NewReader(opts.RawBody)
	} else if opts.Body != nil {
		bodyBytes, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, vs := range opts.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	if (opts.RawBody != nil || opts.Body != nil) && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.applyAuth(req); err != nil {
		return nil, err
	}
	return req, nil
}

// Do performs a request and returns the raw Response without decoding the body.
// Non-2xx responses are returned as a *HTTPError with a redacted, truncated
// excerpt. Safe (idempotent) methods are retried on transient errors with
// backoff, honoring Retry-After for 429s. The caller must close Response.Body.
//
// Do underpins Request and Paginate, which stay convenience wrappers.
func (c *Client) Do(ctx context.Context, pathOrURL string, opts RequestOptions) (*Response, error) {
	if opts.Body != nil && opts.RawBody != nil {
		return nil, fmt.Errorf("request Body and RawBody are mutually exclusive")
	}
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}

	requestCtx := ctx
	var cancel context.CancelFunc
	if !opts.Streaming && c.requestTimeout > 0 {
		// WithTimeout uses the earlier of the parent deadline and this
		// operation timeout, so caller cancellation is never extended.
		requestCtx, cancel = context.WithTimeout(ctx, c.requestTimeout)
	}
	keepContext := false
	defer func() {
		if cancel != nil && !keepContext {
			cancel()
		}
	}()

	attempts := 1
	if isIdempotent(method) {
		attempts = 1 + maxRetries
	}
	delay := initialRetryDelay
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-requestCtx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, requestCtx.Err()
			case <-timer.C:
			}
			delay *= retryDelayFactor
		}

		req, err := c.buildRequest(requestCtx, method, pathOrURL, opts)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctxErr := requestCtx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = fmt.Errorf("request to %s failed: %w", req.URL.String(), err)
			if attempt < attempts-1 {
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if cancel != nil {
				// The request context also controls response-body reads. Keep
				// it alive until the caller finishes with the body.
				resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
			}
			keepContext = true
			return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, nil
		}

		payload, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read error response body: %w", readErr)
			if attempt < attempts-1 {
				continue
			}
			return nil, lastErr
		}

		if retryableStatuses[resp.StatusCode] && attempt < attempts-1 {
			if retryAfter := retryAfterDelay(resp.Header.Get("Retry-After")); retryAfter > 0 {
				delay = retryAfter
			}
			lastErr = c.httpError(req, resp.StatusCode, payload)
			continue
		}
		return nil, c.httpError(req, resp.StatusCode, payload)
	}
	return nil, lastErr
}

func retryAfterDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	if delay := time.Until(when); delay > 0 {
		return delay
	}
	return 0
}

// UploadFile describes one local file to include in a multipart upload.
// Name is the remote filename sent in the multipart part. If Name is empty,
// the local path's base name is used.
type UploadFile struct {
	Path string
	Name string
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.cancel()
	}
	return n, err
}

func (b *cancelOnCloseBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}

func configureTransport(client *http.Client) {
	if client == nil {
		return
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	base, ok := transport.(*http.Transport)
	if !ok {
		// A custom RoundTripper owns its own connection policy. Request
		// contexts still enforce ordinary operation deadlines.
		return
	}

	configured := base.Clone()
	configured.DialContext = (&net.Dialer{
		Timeout:   DefaultDialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	configured.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	configured.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	configured.IdleConnTimeout = DefaultIdleConnTimeout
	configured.ExpectContinueTimeout = DefaultExpectContinueTimeout
	client.Transport = configured
}

func safeRedirect(req *http.Request, _ []*http.Request) error {
	if req.URL.Scheme != "https" || (strings.ToLower(req.URL.Hostname()) != "api.bitbucket.org" && strings.ToLower(req.URL.Hostname()) != "bitbucket.org") || req.URL.Port() != "" || req.URL.User != nil {
		return fmt.Errorf("refusing redirect to non-Bitbucket host %q", req.URL.String())
	}
	return nil
}

func buildURL(pathOrURL string) (string, error) {
	u, err := url.Parse(pathOrURL)
	if err != nil {
		return "", fmt.Errorf("parse request URL: %w", err)
	}
	if u.IsAbs() {
		if u.Scheme != "https" || strings.ToLower(u.Hostname()) != "api.bitbucket.org" || u.Port() != "" || u.User != nil {
			return "", fmt.Errorf("request URL must use https://api.bitbucket.org (got %q)", pathOrURL)
		}
		return u.String(), nil
	}
	if strings.HasPrefix(pathOrURL, "/") {
		return APIBaseURL + pathOrURL, nil
	}
	return APIBaseURL + "/" + pathOrURL, nil
}

func excerpt(b []byte) string {
	s := string(b)
	if len(s) > excerptLimit {
		return s[:excerptLimit] + "..."
	}
	return s
}

// Request performs an API call and decodes the JSON response into out (which
// may be nil to discard the body). It returns an *HTTPError for non-2xx
// responses.
func (c *Client) Request(ctx context.Context, pathOrURL string, opts RequestOptions, out any) error {
	resp, err := c.Do(ctx, pathOrURL, opts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// UploadFiles performs a multipart/form-data POST containing local files. Each
// file is sent using fieldName, matching Bitbucket's Downloads API convention
// of one or more "files" fields.
func (c *Client) UploadFiles(ctx context.Context, pathOrURL, fieldName string, files []UploadFile, out any) error {
	if fieldName == "" {
		return fmt.Errorf("multipart field name is required")
	}
	if len(files) == 0 {
		return fmt.Errorf("at least one upload file is required")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		name := file.Name
		if name == "" {
			name = filepath.Base(file.Path)
		}
		part, err := writer.CreateFormFile(fieldName, name)
		if err != nil {
			return fmt.Errorf("create multipart file field for %q: %w", name, err)
		}
		f, err := os.Open(file.Path)
		if err != nil {
			return fmt.Errorf("open upload file %q: %w", file.Path, err)
		}
		_, copyErr := io.Copy(part, f)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("read upload file %q: %w", file.Path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close upload file %q: %w", file.Path, closeErr)
		}
	}
	contentType := writer.FormDataContentType()
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish multipart body: %w", err)
	}

	resp, err := c.Do(ctx, pathOrURL, RequestOptions{
		Method:  http.MethodPost,
		RawBody: body.Bytes(),
		Headers: http.Header{"Content-Type": {contentType}},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) applyAuth(req *http.Request) error {
	if c.auth == nil {
		return nil
	}
	if err := c.auth.Apply(req); err != nil {
		return fmt.Errorf("apply auth: %w", err)
	}
	return nil
}

// httpError builds a normalized HTTPError, redacting any configured secret from
// the URL and the response excerpt so tokens never reach error output.
func (c *Client) httpError(req *http.Request, status int, payload []byte) *HTTPError {
	secret := c.secret()
	urlStr := req.URL.String()
	exc := excerpt(payload)
	if secret != "" {
		urlStr = auth.Redact(urlStr, secret)
		exc = auth.Redact(exc, secret)
	}
	hint := RequiredScopesFor(auth.TokenType(auth.ProviderKind(c.auth)), req.Method, req.URL.String())
	return &HTTPError{
		Method:         req.Method,
		URL:            urlStr,
		Status:         status,
		StatusText:     http.StatusText(status),
		Excerpt:        exc,
		CredentialKind: string(hint.CredentialKind),
		RequiredScopes: hint.Scopes,
		ScopeNote:      hint.Note,
	}
}

func (c *Client) secret() string {
	if s, ok := c.auth.(auth.SecretRevealer); ok {
		return s.Secret()
	}
	return ""
}

// page is the standard Bitbucket paginated envelope.
type page struct {
	Values []json.RawMessage `json:"values"`
	Next   string            `json:"next"`
}

// Paginate follows `next` links, accumulating raw values until limit is
// reached or maxPages is exhausted. Each element is a raw JSON object the
// caller decodes or passes through.
func (c *Client) Paginate(ctx context.Context, pathOrURL string, limit, maxPages int) ([]json.RawMessage, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return c.paginate(ctx, pathOrURL, limit, maxPages)
}

// PaginateAll follows every `next` link up to maxPages. It is intended for
// commands whose contract promises a complete result rather than the CLI's
// default bounded result size.
func (c *Client) PaginateAll(ctx context.Context, pathOrURL string, maxPages int) ([]json.RawMessage, error) {
	return c.paginate(ctx, pathOrURL, 0, maxPages)
}

func (c *Client) paginate(ctx context.Context, pathOrURL string, limit, maxPages int) ([]json.RawMessage, error) {
	if maxPages <= 0 {
		maxPages = DefaultMaxPages
	}

	var values []json.RawMessage
	next := pathOrURL
	pages := 0
	seen := map[string]bool{pathOrURL: true}

	for next != "" && (limit == 0 || len(values) < limit) && pages < maxPages {
		var p page
		if err := c.Request(ctx, next, RequestOptions{}, &p); err != nil {
			return nil, err
		}
		values = append(values, p.Values...)
		if p.Next == "" {
			break
		}
		if seen[p.Next] {
			return nil, fmt.Errorf("pagination loop detected at %s", p.Next)
		}
		seen[p.Next] = true
		next = p.Next
		pages++
	}

	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}
