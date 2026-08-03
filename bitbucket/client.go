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
	"strings"

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

// WithAuth injects the credential provider to use for requests. Defaults to
// Basic API-token auth if not provided.
func WithAuth(a auth.Provider) Option {
	return func(c *Client) { c.auth = a }
}

// NewClient builds a Client using the given auth provider.
func NewClient(a auth.Provider, opts ...Option) *Client {
	c := &Client{auth: a, http: http.DefaultClient}
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
	Body   any
}

// UploadFile describes one local file to include in a multipart upload.
// Name is the remote filename sent in the multipart part. If Name is empty,
// the local path's base name is used.
type UploadFile struct {
	Path string
	Name string
}

func buildURL(pathOrURL string) string {
	if strings.HasPrefix(pathOrURL, "https://") {
		return pathOrURL
	}
	if strings.HasPrefix(pathOrURL, "/") {
		return APIBaseURL + pathOrURL
	}
	return APIBaseURL + "/" + pathOrURL
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
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}
	fullURL := buildURL(pathOrURL)

	var bodyReader io.Reader
	var bodyBytes []byte
	if opts.Body != nil {
		var err error
		bodyBytes, err = json.Marshal(opts.Body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if err := c.applyAuth(req); err != nil {
		return err
	}
	if opts.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.do(req, out)
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
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish multipart body: %w", err)
	}

	fullURL := buildURL(pathOrURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, &body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if err := c.applyAuth(req); err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return c.do(req, out)
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

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", req.URL.String(), err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.httpError(req, resp.StatusCode, payload)
	}

	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
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

	for next != "" && len(values) < limit && pages < maxPages {
		var p page
		if err := c.Request(ctx, next, RequestOptions{}, &p); err != nil {
			return nil, err
		}
		values = append(values, p.Values...)
		next = p.Next
		pages++
	}

	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}
