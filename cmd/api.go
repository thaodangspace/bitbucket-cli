package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

// The write-capable flag surface (--method/-X, -H, -f, -F, --input) is
// deliberately explicit; this command is a pass-through to Bitbucket REST 2.0.
var (
	apiMethod   string
	apiHeaders  []string
	apiRawField []string
	apiField    []string
	apiInput    string
	apiPaginate bool
	apiSlurp    bool
	apiInclude  bool
	apiSilent   bool
	apiOutput   string
	apiJQ       string
	apiTemplate string
	apiCache    string
	apiMaxPages int
)

func init() {
	apiCmd := &cobra.Command{
		Use:   "api <endpoint>",
		Short: "Make an arbitrary Bitbucket Cloud REST 2.0 request",
		Long: "Make an arbitrary request to the Bitbucket Cloud REST 2.0 API. This is a " +
			"write operation when --method is POST, PUT, PATCH, or DELETE: use it only when " +
			"the user has explicitly asked to change data. With --method GET it is read-only.\n\n" +
			"Endpoint resolution: pass a relative path such as '/repositories/{workspace}/{repo}/pullrequests' " +
			"or an absolute https://api.bitbucket.org/... URL. '{workspace}' and '{repo}' are expanded from " +
			"the usual flags/config/git remote. Absolute URLs on other hosts are rejected.",
		Args: cobra.ExactArgs(1),
		RunE: runAPI,
	}
	apiCmd.Flags().StringVarP(&apiMethod, "method", "X", "", "HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS); defaults to GET")
	apiCmd.Flags().StringArrayVarP(&apiHeaders, "header", "H", nil, "Request header in key:value form (repeatable)")
	apiCmd.Flags().StringArrayVarP(&apiRawField, "raw-field", "f", nil, "String field in key=value form (repeatable)")
	apiCmd.Flags().StringArrayVarP(&apiField, "field", "F", nil, "Typed field in key=value form; true/false/null and integers are typed, supports target[x]=y nesting and a[]=v arrays (repeatable)")
	apiCmd.Flags().StringVar(&apiInput, "input", "", "Raw request body from a file, or - for stdin")
	apiCmd.Flags().BoolVar(&apiPaginate, "paginate", false, "Follow Bitbucket 'next' pagination links for GET requests")
	apiCmd.Flags().BoolVar(&apiSlurp, "slurp", false, "With --paginate, apply --jq/--template to the accumulated array instead of each item")
	apiCmd.Flags().BoolVarP(&apiInclude, "include", "i", false, "Include response status and headers before the body")
	apiCmd.Flags().BoolVar(&apiSilent, "silent", false, "Suppress the response body")
	apiCmd.Flags().StringVarP(&apiOutput, "output", "o", "", "Write the raw response body to a file instead of stdout")
	apiCmd.Flags().StringVarP(&apiJQ, "jq", "q", "", "Apply a jq expression to JSON output (supports .foo, .foo.bar, [<index>], [])")
	apiCmd.Flags().StringVarP(&apiTemplate, "template", "t", "", "Format JSON output with a Go template (adds 'json' and 'pretty' helpers)")
	apiCmd.Flags().StringVar(&apiCache, "cache", "", "Cache GET responses for this duration (e.g. 30s, 2m)")
	apiCmd.Flags().IntVar(&apiMaxPages, "max-pages", bitbucket.DefaultMaxPages, "Maximum pages to follow with --paginate (0 means unlimited)")
	rootCmd.AddCommand(apiCmd)
}

// runAPI implements `api <endpoint>`.
func runAPI(cmd *cobra.Command, args []string) error {
	if apiJQ != "" && apiTemplate != "" {
		return fail(fmt.Errorf("--jq and --template are mutually exclusive"))
	}

	method := strings.ToUpper(apiMethod)
	if method == "" {
		method = http.MethodGet
	}
	if !validMethod(method) {
		return fail(fmt.Errorf("invalid --method %q (use GET, POST, PUT, PATCH, DELETE, HEAD, or OPTIONS)", method))
	}
	if apiPaginate && method != http.MethodGet {
		return fail(fmt.Errorf("--paginate is only supported with GET requests"))
	}
	if apiPaginate && apiMaxPages < 0 {
		return fail(fmt.Errorf("--max-pages must be positive or 0 for unlimited pagination"))
	}
	if apiOutput != "" && (apiJQ != "" || apiTemplate != "") {
		return fail(fmt.Errorf("--output cannot be combined with --jq or --template"))
	}
	cacheTTL := parseCacheDuration(apiCache)
	if strings.TrimSpace(apiCache) != "" && cacheTTL <= 0 {
		return fail(fmt.Errorf("invalid --cache duration %q", apiCache))
	}

	cfg, client, err := newClient()
	if err != nil {
		return fail(err)
	}
	endpoint, err := resolveAPIEndpoint(args[0], cfg)
	if err != nil {
		return fail(err)
	}

	headers := http.Header{}
	for _, h := range apiHeaders {
		key, val, herr := parseHeaderFlag(h)
		if herr != nil {
			return fail(herr)
		}
		headers.Add(key, val)
	}

	fields, err := parseFieldFlags()
	if err != nil {
		return fail(err)
	}

	sendInBody := isBodyMethod(method) && apiInput == ""
	opts := bitbucket.RequestOptions{Method: method, Headers: headers}
	if sendInBody {
		if len(fields) > 0 {
			body, berr := buildFieldBody(fields)
			if berr != nil {
				return fail(berr)
			}
			opts.Body = body
		}
	} else {
		query, qerr := buildFieldQuery(fields)
		if qerr != nil {
			return fail(qerr)
		}
		opts.Query = query
		if apiInput != "" {
			raw, rerr := readInput(apiInput, os.Stdin)
			if rerr != nil {
				return fail(rerr)
			}
			opts.RawBody = raw
		}
	}

	cacheKey := ""
	if cacheTTL > 0 && method == http.MethodGet {
		cacheKey = makeAPICacheKey(cfg, endpoint, opts)
	}
	if apiPaginate {
		return runAPIPaginate(ctx(cmd), client, endpoint, opts, apiMaxPages, cacheTTL, cacheKey)
	}

	if cacheKey != "" {
		if r, ok := apiCacheGet(cacheKey); ok {
			defer r.Body.Close()
			return apiEmit(r)
		}
	}

	resp, err := client.Do(ctx(cmd), endpoint, opts)
	if err != nil {
		return fail(err)
	}

	if cacheKey != "" && resp.StatusCode < 300 {
		payload, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return fail(fmt.Errorf("read cached response body: %w", rerr))
		}
		apiCachePut(cacheKey, resp.StatusCode, resp.Header, payload, cacheTTL)
		return apiEmit(&bitbucket.Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		})
	}
	defer resp.Body.Close()
	return apiEmit(resp)
}

// runAPIPaginate follows `next` links for GET requests. Like gh api,
// pagination emits each response page by default and emits an array of pages
// when --slurp is set. Filters run per page unless --slurp is set.
func runAPIPaginate(ctx context.Context, client *bitbucket.Client, endpoint string, opts bitbucket.RequestOptions, maxPages int, cacheTTL time.Duration, cacheKey string) error {
	var responses []any
	cursor := endpoint
	first := true
	seen := map[string]bool{endpoint: true}
	var firstStatus int
	var firstHeader http.Header

	for pages := 0; maxPages == 0 || pages < maxPages; pages++ {
		if !first {
			// Bitbucket's next link is already a complete URL, including the
			// server-selected cursor. Do not carry the first request's fields.
			opts.Query = nil
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}

		pageCacheKey := ""
		if cacheKey != "" {
			pageCacheKey = cacheKey + "\npage=" + cursor
		}
		var payload []byte
		var status int
		var header http.Header
		if pageCacheKey != "" {
			if cached, ok := apiCacheGet(pageCacheKey); ok {
				status, header = cached.StatusCode, cached.Header
				payload, _ = io.ReadAll(cached.Body)
				cached.Body.Close()
			}
		}
		if payload == nil {
			resp, err := client.Do(ctx, cursor, opts)
			if err != nil {
				return fail(err)
			}
			status, header = resp.StatusCode, resp.Header.Clone()
			payload, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fail(fmt.Errorf("read paginated response body: %w", err))
			}
			if pageCacheKey != "" {
				apiCachePut(pageCacheKey, status, header, payload, cacheTTL)
			}
		}
		if first {
			firstStatus = status
			firstHeader = header
		}

		var page any
		if uerr := json.Unmarshal(payload, &page); uerr != nil {
			return fail(fmt.Errorf("--paginate requires a JSON response with a 'values' array: %w", uerr))
		}
		pageMap, ok := page.(map[string]any)
		if !ok {
			return fail(fmt.Errorf("--paginate requires a JSON response with a 'values' array"))
		}
		if _, ok := pageMap["values"].([]any); !ok {
			return fail(fmt.Errorf("--paginate requires a JSON response with a 'values' array"))
		}
		responses = append(responses, page)
		nextValue, _ := pageMap["next"].(string)
		if nextValue == "" {
			break
		}
		if seen[nextValue] {
			return fail(fmt.Errorf("pagination loop detected at %s", nextValue))
		}
		seen[nextValue] = true
		cursor = nextValue
		first = false
		if maxPages > 0 && pages == maxPages-1 {
			return fail(fmt.Errorf("pagination exceeded the maximum of %d pages", maxPages))
		}
	}

	if apiInclude {
		if err := writeHeaders(os.Stdout, firstStatus, firstHeader); err != nil {
			return fail(err)
		}
	}
	if apiSilent {
		return nil
	}
	if apiJQ != "" {
		return emitJQPaginated(responses, apiJQ, apiSlurp)
	}
	if apiTemplate != "" {
		return emitTemplatePaginated(responses, apiTemplate, apiSlurp)
	}
	if apiOutput != "" {
		return writePaginatedToFile(responses, apiOutput, apiSlurp)
	}
	if apiSlurp {
		return output.RenderJSON(os.Stdout, responses)
	}
	for _, page := range responses {
		if err := output.RenderJSON(os.Stdout, page); err != nil {
			return err
		}
	}
	return nil
}

// apiEmit renders a single (successful) response according to the output flags.
func apiEmit(resp *bitbucket.Response) error {
	status := resp.StatusCode
	ctype := strings.ToLower(resp.Header.Get("Content-Type"))

	if apiOutput != "" {
		if apiInclude {
			if err := writeHeaders(os.Stdout, status, resp.Header); err != nil {
				return fail(err)
			}
		}
		f, err := os.Create(apiOutput)
		if err != nil {
			return fail(fmt.Errorf("create output file %q: %w", apiOutput, err))
		}
		_, copyErr := io.Copy(f, resp.Body)
		closeErr := f.Close()
		if copyErr != nil {
			return fail(fmt.Errorf("write output file %q: %w", apiOutput, copyErr))
		}
		if closeErr != nil {
			return fail(fmt.Errorf("close output file %q: %w", apiOutput, closeErr))
		}
		return nil
	}

	if apiInclude {
		if err := writeHeaders(os.Stdout, status, resp.Header); err != nil {
			return fail(err)
		}
	}
	if apiSilent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if apiJQ != "" || apiTemplate != "" {
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return fail(fmt.Errorf("read response body: %w", err))
		}
		return emitJSONTransform(payload, resp.Header.Get("Content-Type"))
	}

	if status == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	// Bitbucket normally supplies Content-Type, but a proxy or a test transport
	// may omit it. Sniff only that case; text/plain JSON must remain raw text.
	if strings.Contains(ctype, "json") {
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return fail(fmt.Errorf("read response body: %w", err))
		}
		return renderJSONOrRaw(payload)
	}
	if ctype == "" {
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return fail(fmt.Errorf("read response body: %w", err))
		}
		trimmed := strings.TrimSpace(string(payload))
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			return renderJSONOrRaw(payload)
		}
		_, err = io.Copy(os.Stdout, bytes.NewReader(payload))
		return err
	}

	_, err := io.Copy(os.Stdout, resp.Body)
	return err
}

func emitJSONTransform(payload []byte, contentType string) error {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return fail(fmt.Errorf("--jq/--template require a JSON response (content-type %q): %w", contentType, err))
	}
	if apiJQ != "" {
		return emitJQ(v, apiJQ)
	}
	return emitTemplate(v, apiTemplate)
}

func renderJSONOrRaw(payload []byte) error {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		_, copyErr := io.Copy(os.Stdout, bytes.NewReader(payload))
		return copyErr
	}
	return output.RenderJSON(os.Stdout, v)
}

// --- endpoint resolution ---

// resolveAPIEndpoint normalizes an endpoint: it accepts absolute
// https://api.bitbucket.org URLs and relative REST 2.0 paths, rejects absolute
// URLs on other hosts, and expands the {workspace}/{repo} placeholders.
func resolveAPIEndpoint(endpoint string, cfg config.Config) (string, error) {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return "", fmt.Errorf("endpoint is required")
	}

	u, parseErr := url.Parse(ep)
	if parseErr != nil {
		return "", fmt.Errorf("parse endpoint URL: %w", parseErr)
	}
	isAbsolute := u.IsAbs()
	if isAbsolute {
		if strings.ToLower(u.Hostname()) != "api.bitbucket.org" {
			return "", fmt.Errorf("endpoint host %q is not the configured Bitbucket API host (api.bitbucket.org)", u.Host)
		}
		if u.Scheme != "https" || u.Port() != "" || u.User != nil {
			return "", fmt.Errorf("endpoint must use https://api.bitbucket.org (got %q)", u.Host)
		}
	}

	needsRef := strings.Contains(ep, "{workspace}") || strings.Contains(ep, "{repo}")
	ws, repo := "", ""
	if needsRef {
		ref, _, err := resolveRepo(cfg)
		if err != nil {
			return "", err
		}
		ws = bitbucket.EncodePathSegment(ref.Workspace)
		repo = bitbucket.EncodePathSegment(ref.RepoSlug)
	}

	out := strings.ReplaceAll(ep, "{workspace}", ws)
	out = strings.ReplaceAll(out, "{repo}", repo)
	if !isAbsolute {
		out = "/" + strings.TrimLeft(out, "/")
	}
	return out, nil
}

// --- fields ---

// apiFieldSpec is one -f/-F field.
type apiFieldSpec struct {
	rawKey string
	tokens []string
	value  string
	typed  bool
}

// parseFieldFlags splits every -f/-F flag into a spec. -F marks the value as
// typed JSON; -f always sends a string.
func parseFieldFlags() ([]apiFieldSpec, error) {
	var specs []apiFieldSpec
	for _, f := range apiRawField {
		key, val, err := splitField(f)
		if err != nil {
			return nil, err
		}
		tokens, terr := parseFieldKey(key)
		if terr != nil {
			return nil, terr
		}
		specs = append(specs, apiFieldSpec{rawKey: key, tokens: tokens, value: val, typed: false})
	}
	for _, f := range apiField {
		key, val, err := splitField(f)
		if err != nil {
			return nil, err
		}
		tokens, terr := parseFieldKey(key)
		if terr != nil {
			return nil, terr
		}
		specs = append(specs, apiFieldSpec{rawKey: key, tokens: tokens, value: val, typed: true})
	}
	return specs, nil
}

// splitField separates a "key=value" flag on the first '='.
func splitField(f string) (string, string, error) {
	i := strings.Index(f, "=")
	if i <= 0 {
		return "", "", fmt.Errorf("invalid field %q: expected key=value", f)
	}
	return f[:i], f[i+1:], nil
}

// parseFieldKey turns a key with bracket syntax into path tokens. An empty
// token marks an array append (from a trailing []). e.g. "target[ref_type]"
// -> ["target","ref_type"], "variables[]" -> ["variables","" ],
// "items[][label]" -> ["items","","label"].
func parseFieldKey(key string) ([]string, error) {
	if key == "" {
		return nil, fmt.Errorf("empty field key")
	}
	var toks []string
	var buf strings.Builder
	i := 0
	for i < len(key) {
		switch key[i] {
		case '[':
			if buf.Len() > 0 {
				toks = append(toks, buf.String())
				buf.Reset()
			}
			j := strings.IndexByte(key[i+1:], ']')
			if j < 0 {
				return nil, fmt.Errorf("unmatched '[' in field key %q", key)
			}
			toks = append(toks, key[i+1:i+1+j])
			i = i + j + 2
		case ']':
			return nil, fmt.Errorf("unmatched ']' in field key %q", key)
		default:
			buf.WriteByte(key[i])
			i++
		}
	}
	if buf.Len() > 0 {
		toks = append(toks, buf.String())
	}
	return toks, nil
}

// resolveFieldValue returns the typed JSON value and the plain string value for
// a field spec (reading @path/@- values from disk/stdin when used). For a raw
// string field both are the literal value; for a typed field the typed value
// reflects true/false/null/integer literals.
func resolveFieldValue(f apiFieldSpec, stdin io.Reader) (typedVal any, stringVal string, err error) {
	s := f.value
	if strings.HasPrefix(s, "@") {
		name := strings.TrimPrefix(s, "@")
		var b []byte
		if name == "-" {
			b, err = io.ReadAll(stdin)
		} else {
			b, err = os.ReadFile(name)
		}
		if err != nil {
			return nil, "", fmt.Errorf("read field value %q: %w", s, err)
		}
		s = string(b)
	}
	if f.typed {
		v, verr := typedValue(s)
		return v, s, verr
	}
	return s, s, nil
}

// typedValue parses true/false/null and integers, otherwise returns a string.
func typedValue(s string) (any, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i, nil
	}
	return s, nil
}

// buildFieldBody assembles a nested JSON body from -f/-F fields.
func buildFieldBody(fields []apiFieldSpec) (any, error) {
	root := &fieldNode{}
	for _, f := range fields {
		val, _, err := resolveFieldValue(f, os.Stdin)
		if err != nil {
			return nil, err
		}
		if err := setFieldLeaf(root, f.tokens, 0, val); err != nil {
			return nil, err
		}
	}
	if root.children == nil {
		return map[string]any{}, nil
	}
	v := root.materialize()
	return v, nil
}

// buildFieldQuery flattens -f/-F fields into query parameters, preserving the
// literal key (including bracket notation) and ordinal order.
func buildFieldQuery(fields []apiFieldSpec) (url.Values, error) {
	q := url.Values{}
	for _, f := range fields {
		_, s, err := resolveFieldValue(f, os.Stdin)
		if err != nil {
			return nil, err
		}
		q.Add(f.rawKey, s)
	}
	return q, nil
}

// fieldNode is an intermediate representation for building nested JSON bodies.
type fieldNode struct {
	children map[string]*fieldNode
	items    []*fieldNode
	array    bool
	value    any
	hasValue bool
}

func setFieldLeaf(n *fieldNode, toks []string, i int, val any) error {
	if i == len(toks) {
		if n.children != nil || n.items != nil || n.array {
			return fmt.Errorf("field shape conflict: cannot replace an object or array with a scalar")
		}
		// Repeating the exact same key is a deliberate overwrite, matching the
		// usual last-value-wins behavior of command-line field flags.
		n.value = val
		n.hasValue = true
		return nil
	}
	tok := toks[i]
	if tok == "" {
		if n.hasValue || n.children != nil {
			return fmt.Errorf("field shape conflict: cannot append an array item to a scalar or object")
		}
		n.array = true
		if i == len(toks)-1 {
			n.items = append(n.items, &fieldNode{value: val, hasValue: true})
			return nil
		}
		child := &fieldNode{children: map[string]*fieldNode{}}
		n.items = append(n.items, child)
		return setFieldLeaf(child, toks, i+1, val)
	}
	if n.hasValue || n.items != nil || n.array {
		return fmt.Errorf("field shape conflict: cannot add an object key to a scalar or array")
	}
	if n.children == nil {
		n.children = map[string]*fieldNode{}
	}
	child, ok := n.children[tok]
	if !ok {
		child = &fieldNode{}
		n.children[tok] = child
	}
	return setFieldLeaf(child, toks, i+1, val)
}

func (n *fieldNode) materialize() any {
	switch {
	case n.array:
		out := make([]any, 0, len(n.items))
		for _, it := range n.items {
			out = append(out, it.materialize())
		}
		return out
	case n.children != nil:
		out := make(map[string]any, len(n.children))
		for k, ch := range n.children {
			out[k] = ch.materialize()
		}
		return out
	case n.hasValue:
		return n.value
	default:
		return map[string]any{}
	}
}

// --- input & headers ---

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func parseHeaderFlag(h string) (string, string, error) {
	i := strings.Index(h, ":")
	if i <= 0 {
		return "", "", fmt.Errorf("invalid header %q: expected key:value", h)
	}
	key, value := strings.TrimSpace(h[:i]), strings.TrimSpace(h[i+1:])
	if key == "" || strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
		return "", "", fmt.Errorf("invalid header %q", h)
	}
	return key, value, nil
}

func validMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isBodyMethod(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch
}

func parseCacheDuration(s string) time.Duration {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

func writeHeaders(w io.Writer, status int, h http.Header) error {
	fmt.Fprintf(w, "HTTP/1.1 %d %s\n", status, http.StatusText(status))
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h.Values(k) {
			fmt.Fprintf(w, "%s: %s\n", k, v)
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// --- jq / template ---

func emitJQ(v any, expr string) error {
	results, err := jqEvaluate(expr, v)
	if err != nil {
		return fail(fmt.Errorf("jq: %w", err))
	}
	enc := json.NewEncoder(os.Stdout)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return fail(err)
		}
	}
	return nil
}

func emitJQPaginated(items []any, expr string, slurp bool) error {
	if slurp {
		return emitJQ(items, expr)
	}
	enc := json.NewEncoder(os.Stdout)
	for _, it := range items {
		res, err := jqEvaluate(expr, it)
		if err != nil {
			return fail(fmt.Errorf("jq: %w", err))
		}
		for _, r := range res {
			if err := enc.Encode(r); err != nil {
				return fail(err)
			}
		}
	}
	return nil
}

func emitTemplate(v any, expr string) error {
	tpl, err := template.New("out").Funcs(template.FuncMap{
		"json":   func(x any) (string, error) { b, e := json.Marshal(x); return string(b), e },
		"pretty": func(x any) (string, error) { b, e := json.MarshalIndent(x, "", "  "); return string(b), e },
	}).Parse(expr)
	if err != nil {
		return fail(fmt.Errorf("template: %w", err))
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, v); err != nil {
		return fail(fmt.Errorf("template: %w", err))
	}
	fmt.Fprint(os.Stdout, buf.String())
	return nil
}

func emitTemplatePaginated(items []any, expr string, slurp bool) error {
	if slurp {
		return emitTemplate(items, expr)
	}
	for _, it := range items {
		if err := emitTemplate(it, expr); err != nil {
			return err
		}
	}
	return nil
}

func writePaginatedToFile(pages []any, path string, slurp bool) error {
	f, err := os.Create(path)
	if err != nil {
		return fail(fmt.Errorf("create output file %q: %w", path, err))
	}
	defer f.Close()
	if slurp {
		return output.RenderJSON(f, pages)
	}
	for _, page := range pages {
		if err := output.RenderJSON(f, page); err != nil {
			return err
		}
	}
	return nil
}

// --- jq subset ---

type jqAccessor interface{ apply(v any) []any }

type jqKey struct{ name string }
type jqIndex struct{ idx int }
type jqIterate struct{}

func (a jqKey) apply(v any) []any {
	if m, ok := v.(map[string]any); ok {
		if val, exists := m[a.name]; exists {
			return []any{val}
		}
	}
	return nil
}

func (a jqIndex) apply(v any) []any {
	if arr, ok := v.([]any); ok && a.idx >= 0 && a.idx < len(arr) {
		return []any{arr[a.idx]}
	}
	return nil
}

func (a jqIterate) apply(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			out = append(out, val)
		}
		return out
	default:
		return nil
	}
}

// jqEvaluate evaluates a small jq subset: dot paths (.a.b), array indexing
// (.[<n>]), iteration (.[]), object values (.[]), and top-level pipes separated
// by '|'. Each accessor fans out over the current result set, so a jq result
// may be more than one value.
func jqEvaluate(expr string, input any) ([]any, error) {
	stages := strings.Split(expr, "|")
	cur := []any{input}
	for _, st := range stages {
		st = strings.TrimSpace(st)
		accs, err := parseJQAccessors(st)
		if err != nil {
			return nil, err
		}
		var next []any
		for _, v := range cur {
			res, aerr := applyJQAccessors(v, accs)
			if aerr != nil {
				return nil, aerr
			}
			next = append(next, res...)
		}
		cur = next
	}
	return cur, nil
}

func applyJQAccessors(v any, accs []jqAccessor) ([]any, error) {
	cur := []any{v}
	for _, a := range accs {
		var next []any
		for _, item := range cur {
			next = append(next, a.apply(item)...)
		}
		cur = next
	}
	return cur, nil
}

func parseJQAccessors(expr string) ([]jqAccessor, error) {
	if expr == "" {
		return nil, fmt.Errorf("empty expression")
	}
	if expr == "." {
		return nil, nil
	}
	if !strings.HasPrefix(expr, ".") {
		return nil, fmt.Errorf("unsupported jq expression %q (only .path, .a.b, [<index>], and [] are supported)", expr)
	}
	var accs []jqAccessor
	rest := expr[1:]
	for rest != "" {
		switch rest[0] {
		case '[':
			j := strings.IndexByte(rest, ']')
			if j < 0 {
				return nil, fmt.Errorf("unmatched '[' in jq expression %q", expr)
			}
			inner := strings.TrimSpace(rest[1:j])
			rest = rest[j+1:]
			if inner == "" {
				accs = append(accs, jqIterate{})
				continue
			}
			idx, err := strconv.Atoi(inner)
			if err != nil {
				return nil, fmt.Errorf("unsupported jq index %q (only integers or [] are supported)", inner)
			}
			accs = append(accs, jqIndex{idx: idx})
		default:
			end := 0
			for end < len(rest) && rest[end] != '.' && rest[end] != '[' {
				end++
			}
			if end == 0 {
				return nil, fmt.Errorf("unsupported jq expression %q", expr)
			}
			accs = append(accs, jqKey{name: rest[:end]})
			rest = rest[end:]
			if rest != "" && rest[0] == '.' {
				rest = rest[1:]
			}
		}
	}
	return accs, nil
}

// --- persistent GET cache ---

// The memory layer avoids disk I/O for repeated requests in one process. The
// disk layer makes --cache useful across normal CLI invocations.
type apiCacheEntry struct {
	status  int
	header  http.Header
	body    []byte
	expires time.Time
}

type apiCacheFile struct {
	Status  int         `json:"status"`
	Header  http.Header `json:"header"`
	Body    []byte      `json:"body"`
	Expires time.Time   `json:"expires"`
}

var apiCacheStore = struct {
	sync.Mutex
	m map[string]apiCacheEntry
}{m: map[string]apiCacheEntry{}}

func makeAPICacheKey(cfg config.Config, endpoint string, opts bitbucket.RequestOptions) string {
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.APIToken)))
	var headers strings.Builder
	keys := make([]string, 0, len(opts.Headers))
	for key := range opts.Headers {
		keys = append(keys, strings.ToLower(key))
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range opts.Headers.Values(key) {
			fmt.Fprintf(&headers, "%s:%s\n", key, value)
		}
	}
	return fmt.Sprintf("GET\n%s\n%s\n%s\n%s\n%s", endpoint, opts.Query.Encode(), headers.String(), cfg.TokenType, tokenHash)
}

func apiCacheGet(key string) (*bitbucket.Response, bool) {
	now := time.Now()
	apiCacheStore.Lock()
	e, ok := apiCacheStore.m[key]
	if ok && now.Before(e.expires) {
		apiCacheStore.Unlock()
		return cachedResponse(e), true
	}
	if ok {
		delete(apiCacheStore.m, key)
	}
	apiCacheStore.Unlock()

	path, err := apiCachePath(key)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var diskFile apiCacheFile
	if err := json.Unmarshal(data, &diskFile); err != nil || !now.Before(diskFile.Expires) {
		_ = os.Remove(path)
		return nil, false
	}
	disk := apiCacheEntry{status: diskFile.Status, header: diskFile.Header, body: diskFile.Body, expires: diskFile.Expires}
	apiCacheStore.Lock()
	apiCacheStore.m[key] = disk
	apiCacheStore.Unlock()
	return cachedResponse(disk), true
}

func cachedResponse(e apiCacheEntry) *bitbucket.Response {
	return &bitbucket.Response{
		StatusCode: e.status,
		Header:     e.header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(e.body)),
	}
}

func apiCachePut(key string, status int, header http.Header, body []byte, ttl time.Duration) {
	entry := apiCacheEntry{
		status:  status,
		header:  header.Clone(),
		body:    append([]byte(nil), body...),
		expires: time.Now().Add(ttl),
	}
	apiCacheStore.Lock()
	apiCacheStore.m[key] = entry
	apiCacheStore.Unlock()

	path, err := apiCachePath(key)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(apiCacheFile{
		Status:  entry.status,
		Header:  entry.header,
		Body:    entry.body,
		Expires: entry.expires,
	})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".api-cache-")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, path)
}

func apiCachePath(key string) (string, error) {
	root := os.Getenv("BITBUCKET_CACHE_DIR")
	if root == "" {
		var err error
		root, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	return filepath.Join(root, "bitbucket-cli", "api", hash+".json"), nil
}
