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

// excerptLimit bounds how much of an error response body is surfaced.
const excerptLimit = 500

// RequiredScopes maps an API path to the documented Bitbucket API-token scopes
// that cover it, for surfacing actionable guidance on 403 responses. Bitbucket
// does not expose a universal scope-introspection endpoint, so this is a
// conservative, command-family-level registry rather than an exact claim.
func RequiredScopes(path string) string {
	switch {
	case strings.Contains(path, "/downloads"):
		return "write:repository:bitbucket"
	case strings.Contains(path, "/pullrequests/"):
		return "write:pullrequest:bitbucket"
	case strings.Contains(path, "/pullrequests"):
		return "pullrequest:write or pullrequest:read"
	case strings.Contains(path, "/refs/branches"), strings.Contains(path, "/commits"):
		return "repository:read"
	case strings.Contains(path, "/pipeline"):
		return "pipeline:read"
	default:
		return "repository:read"
	}
}

// HTTPError is a normalized non-2xx response from Bitbucket.
type HTTPError struct {
	Method     string `json:"method"`
	URL        string `json:"url"`
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Excerpt    string `json:"excerpt"`
}

func (e *HTTPError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "Bitbucket authentication failed. Run `bitbucket-cli auth login` or set BITBUCKET_EMAIL and BITBUCKET_API_TOKEN."
	case http.StatusForbidden:
		return fmt.Sprintf("Bitbucket authorization failed. The endpoint requires the %s scope; check the token's granted scopes.", RequiredScopes(e.URL))
	case http.StatusNotFound:
		return "Bitbucket resource not found. Check workspace, repo, and IDs."
	case http.StatusTooManyRequests:
		return "Bitbucket rate limit reached. Retry later."
	default:
		return fmt.Sprintf("Bitbucket request failed with %d %s: %s", e.Status, e.StatusText, e.Excerpt)
	}
}

// EncodePathSegment percent-encodes a single path segment (slashes included).
func EncodePathSegment(value string) string {
	return url.PathEscape(value)
}

// Client talks to the Bitbucket Cloud API using an auth.Provider.
type Client struct {
	auth auth.Provider
	http *http.Client
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient injects a custom *http.Client (used in tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithTimeout sets the HTTP client's total request timeout. A non-positive
// duration leaves the existing timeout unchanged.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 && c.http != nil {
			c.http.Timeout = timeout
		}
	}
}

// WithAuth injects the credential provider to use for requests. Defaults to
// Basic API-token auth if not provided.
func WithAuth(a auth.Provider) Option {
	return func(c *Client) { c.auth = a }
}

// NewClient builds a Client using the given auth provider.
func NewClient(a auth.Provider, opts ...Option) *Client {
	httpClient := *http.DefaultClient
	c := &Client{auth: a, http: &httpClient}
	for _, opt := range opts {
		opt(c)
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
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
			delay *= retryDelayFactor
		}

		req, err := c.buildRequest(ctx, method, pathOrURL, opts)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = fmt.Errorf("request to %s failed: %w", req.URL.String(), err)
			if attempt < attempts-1 {
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
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

func buildURL(pathOrURL string) (string, error) {
	if strings.Contains(pathOrURL, "://") {
		u, err := url.Parse(pathOrURL)
		if err != nil {
			return "", fmt.Errorf("parse request URL: %w", err)
		}
		if u.Scheme != "https" || strings.ToLower(u.Hostname()) != "api.bitbucket.org" || u.User != nil {
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
	return &HTTPError{
		Method:     req.Method,
		URL:        urlStr,
		Status:     status,
		StatusText: http.StatusText(status),
		Excerpt:    exc,
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
	if maxPages <= 0 {
		maxPages = DefaultMaxPages
	}

	var values []json.RawMessage
	next := pathOrURL
	pages := 0
	seen := map[string]bool{pathOrURL: true}

	for next != "" && len(values) < limit && pages < maxPages {
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

	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}
