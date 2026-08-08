package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
	"gopkg.in/yaml.v3"
)

// Webhook and key commands deliberately keep sensitive values out of their
// output. Bitbucket may return a secret or public key in a response even when
// the caller did not ask for it, so sanitizing is done before rendering.
func sanitizeWebhookValue(value any, fullURL bool) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || lower == "password" || lower == "token" {
				continue
			}
			if lower == "url" {
				if s, ok := child.(string); ok && !fullURL {
					child = redactWebhookURL(s)
				}
			}
			out[key] = sanitizeWebhookValue(child, fullURL)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = sanitizeWebhookValue(child, fullURL)
		}
		return out
	default:
		return value
	}
}

func redactWebhookError(err error, secret string) error {
	if secret == "" || err == nil {
		return err
	}
	var httpErr *bitbucket.HTTPError
	if errors.As(err, &httpErr) {
		copy := *httpErr
		copy.URL = strings.ReplaceAll(copy.URL, secret, "<redacted>")
		copy.Excerpt = strings.ReplaceAll(copy.Excerpt, secret, "<redacted>")
		return &copy
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), secret, "<redacted>"))
}

func outputFieldSelected(name string) bool {
	for _, field := range splitFields(flagJSON) {
		if field == name {
			return true
		}
	}
	return false
}

func sanitizeWebhookJSON(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	clean, err := json.Marshal(sanitizeWebhookValue(value, outputFieldSelected("url") || flagFormat == "raw"))
	if err != nil {
		return raw
	}
	return clean
}

func redactWebhookURL(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		return "<invalid-url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func webhookTarget(repository, workspace string) (subject, base, label string, err error) {
	repository = strings.TrimSpace(repository)
	workspace = strings.TrimSpace(workspace)
	if repository != "" && workspace != "" {
		return "", "", "", fmt.Errorf("--repository cannot be combined with --workspace")
	}
	if repository != "" {
		ref, e := parseRepositorySelector(repository)
		if e != nil {
			return "", "", "", e
		}
		base = fmt.Sprintf("/repositories/%s/%s", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.RepoSlug))
		return "repository", base + "/hooks", ref.Workspace + "/" + ref.RepoSlug, nil
	}
	if workspace == "" {
		return "", "", "", fmt.Errorf("one of --repository or --workspace is required")
	}
	if strings.ContainsAny(workspace, "/?#") {
		return "", "", "", fmt.Errorf("invalid workspace selector %q", workspace)
	}
	return "workspace", "/workspaces/" + bitbucket.EncodePathSegment(workspace) + "/hooks", workspace, nil
}

func validateWebhookURL(value string, allowInsecureLocalhost, allowPrivate bool) error {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return fmt.Errorf("invalid webhook URL %q", value)
	}
	if u.User != nil {
		return fmt.Errorf("webhook URL must not contain embedded credentials")
	}
	if u.Scheme != "https" {
		if !(u.Scheme == "http" && allowInsecureLocalhost && strings.EqualFold(u.Hostname(), "localhost")) {
			return fmt.Errorf("webhook URL must use https (http://localhost requires --allow-insecure-localhost)")
		}
	}
	if privateWebhookHost(u.Hostname()) && !allowPrivate {
		return fmt.Errorf("webhook URL resolves to a loopback, private, or link-local destination; use --allow-private to confirm")
	}
	// DNS is checked as well as literal addresses. Lookup failure is left to
	// Bitbucket/the user's DNS, but a known private answer requires consent.
	if net.ParseIP(u.Hostname()) == nil {
		lookupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ips, lookupErr := net.DefaultResolver.LookupIP(lookupCtx, "ip", u.Hostname())
		cancel()
		if lookupErr == nil {
			for _, ip := range ips {
				if privateWebhookHost(ip.String()) && !allowPrivate {
					return fmt.Errorf("webhook URL host %q resolves to a loopback, private, or link-local destination; use --allow-private to confirm", u.Hostname())
				}
			}
		}
	}
	return nil
}

func privateWebhookHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func eventCatalog(ctx context.Context, client *bitbucket.Client, subject string, noCache bool) ([]string, error) {
	cacheDir := os.Getenv("BITBUCKET_CACHE_DIR")
	cachePath := ""
	if !noCache && os.Getenv("BITBUCKET_DISABLE_CACHE") == "" && cacheDir != "" {
		cachePath = cacheDir + "/webhook-events-" + subject + ".json"
		if data, err := os.ReadFile(cachePath); err == nil {
			var cached struct {
				Fetched time.Time `json:"fetched"`
				Events  []string  `json:"events"`
			}
			if json.Unmarshal(data, &cached) == nil && time.Since(cached.Fetched) < 10*time.Minute && len(cached.Events) > 0 {
				return cached.Events, nil
			}
		}
	}
	var raw json.RawMessage
	if err := client.Request(ctx, "/hook_events/"+bitbucket.EncodePathSegment(subject), bitbucket.RequestOptions{}, &raw); err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode webhook event catalog: %w", err)
	}
	if envelope, ok := value.(map[string]any); ok {
		if values, exists := envelope["values"]; exists {
			value = values
		}
	}
	var events []string
	if list, ok := value.([]any); ok {
		for _, item := range list {
			switch item := item.(type) {
			case string:
				events = append(events, item)
			case map[string]any:
				for _, key := range []string{"event", "key", "name"} {
					if event, ok := item[key].(string); ok && event != "" {
						events = append(events, event)
						break
					}
				}
			}
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("webhook event catalog for %s was empty or had an unsupported shape", subject)
	}
	events = uniqueStrings(events)
	if cachePath != "" {
		data, _ := json.Marshal(struct {
			Fetched time.Time `json:"fetched"`
			Events  []string  `json:"events"`
		}{time.Now().UTC(), events})
		_ = os.MkdirAll(cacheDir, 0700)
		_ = os.WriteFile(cachePath, data, 0600)
	}
	return events, nil
}

func validateWebhookEvents(ctx context.Context, client *bitbucket.Client, subject string, requested []string, allowUnknown, noCache bool) ([]string, error) {
	requested = uniqueStrings(requested)
	if len(requested) == 0 {
		return nil, fmt.Errorf("at least one --event is required")
	}
	if allowUnknown {
		return requested, nil
	}
	valid, err := eventCatalog(ctx, client, subject, noCache)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, event := range valid {
		known[event] = true
	}
	for _, event := range requested {
		if !known[event] {
			return nil, fmt.Errorf("unknown webhook event %q (did you mean %q?); use --allow-unknown-event to bypass validation", event, eventSuggestion(event, valid))
		}
	}
	return requested, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func eventSuggestion(value string, valid []string) string {
	best := ""
	bestDistance := 999
	for _, candidate := range valid {
		d := levenshtein(strings.ToLower(value), strings.ToLower(candidate))
		if d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	if bestDistance > 8 {
		return "a valid catalog event"
	}
	return best
}

func levenshtein(a, b string) int {
	row := make([]int, len(b)+1)
	for i := range row {
		row[i] = i
	}
	for i, ra := range a {
		previous := row[0]
		row[0] = i + 1
		for j, rb := range b {
			old := row[j+1]
			cost := 0
			if ra != rb {
				cost = 1
			}
			row[j+1] = minAccessInt(row[j+1]+1, row[j]+1, previous+cost)
			previous = old
		}
	}
	return row[len(b)]
}
func minAccessInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func readWebhookSecret(useStdin, prompt bool, envName string) (string, bool, error) {
	if (useStdin || prompt) && envName != "" {
		return "", false, fmt.Errorf("--secret-stdin/--secret-prompt and --secret-env are mutually exclusive")
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read webhook secret from stdin: %w", err)
		}
		secret := strings.TrimSpace(string(data))
		if secret == "" {
			return "", false, fmt.Errorf("webhook secret from stdin must not be empty")
		}
		return secret, true, nil
	}
	if envName != "" {
		if strings.ContainsAny(envName, "= \t\r\n") {
			return "", false, fmt.Errorf("--secret-env expects an environment variable name")
		}
		secret := os.Getenv(envName)
		if secret == "" {
			return "", false, fmt.Errorf("webhook secret environment variable %q is empty", envName)
		}
		return secret, true, nil
	}
	if !prompt {
		return "", false, nil
	}
	if _, err := os.Stdin.Stat(); err != nil {
		return "", false, err
	}
	if err := exec.Command("sh", "-c", "stty -echo < /dev/tty").Run(); err != nil {
		return "", false, fmt.Errorf("secret prompt requires a TTY")
	}
	defer exec.Command("sh", "-c", "stty echo < /dev/tty").Run()
	fmt.Fprint(os.Stderr, "Webhook secret: ")
	secret, err := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", false, fmt.Errorf("webhook secret must not be empty")
	}
	return secret, true, nil
}

func webhookBody(description, targetURL string, events []string, active bool, activeSet bool, secret string, secretSet bool) map[string]any {
	body := map[string]any{"description": description, "url": targetURL, "events": events}
	if activeSet {
		body["active"] = active
	}
	if secretSet {
		body["secret"] = secret
	}
	return body
}

func init() {
	webhookCmd := &cobra.Command{Use: "webhook", Short: "Manage Bitbucket webhooks"}
	var eventSubject string
	var eventNoCache bool
	eventsCmd := &cobra.Command{Use: "events [subject]", Short: "List webhook event keys", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		subject := eventSubject
		if len(args) == 1 {
			subject = args[0]
		}
		if subject == "" {
			subject = "repository"
		}
		if !contains([]string{"repository", "workspace", "user"}, subject) {
			return fail(fmt.Errorf("invalid webhook event subject %q", subject))
		}
		_, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		// /hook_events is a public catalog and intentionally has no
		// capability/scope preflight.
		values, err := eventCatalog(ctx(cmd), client, subject, eventNoCache)
		if err != nil {
			return fail(err)
		}
		items := make([]json.RawMessage, 0, len(values))
		for _, value := range values {
			raw, _ := json.Marshal(map[string]string{"event": value})
			items = append(items, raw)
		}
		return emitListFields(items, output.WebhookEventFields, output.WebhookEventSummary, "No webhook events found.")
	}}
	eventsCmd.Flags().StringVar(&eventSubject, "subject", "", "Catalog subject: repository, workspace, or user")
	eventsCmd.Flags().BoolVar(&eventNoCache, "no-event-cache", false, "Do not read or write the event catalog cache")

	var listRepository, listWorkspace string
	var listLimit int
	listCmd := &cobra.Command{Use: "list", Short: "List webhooks", Long: capabilityHelp("webhook.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.read"); err != nil {
			return fail(err)
		}
		_, path, _, err := webhookTarget(firstNonEmptyCLI(listRepository, flagRepository), firstNonEmptyCLI(listWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		values, err := client.Paginate(ctx(cmd), path+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), listLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(sanitizeWebhookValues(values), output.WebhookFields, output.WebhookSummary, "No webhooks found.")
	}}
	addWebhookTargetFlags(listCmd, &listRepository, &listWorkspace)
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum webhooks to return")

	var viewRepository, viewWorkspace string
	viewCmd := &cobra.Command{Use: "view <uuid>", Short: "View a webhook", Long: capabilityHelp("webhook.read"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.read"); err != nil {
			return fail(err)
		}
		_, path, _, err := webhookTarget(firstNonEmptyCLI(viewRepository, flagRepository), firstNonEmptyCLI(viewWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), path+"/"+bitbucket.EncodePathSegment(args[0]), bitbucket.RequestOptions{}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(sanitizeWebhookJSON(raw), output.WebhookFields, output.WebhookSummary)
	}}
	addWebhookTargetFlags(viewCmd, &viewRepository, &viewWorkspace)

	var createRepository, createWorkspace, createURL, createDescription, createSecretEnv string
	var createEvents []string
	var createActive, createActiveSet, createSecretStdin, createSecretPrompt, createAllowPrivate, createAllowHTTP, createAllowUnknown, createNoCache bool
	createCmd := &cobra.Command{Use: "create --url <https-url> --event <key>...", Short: "Create a webhook", Long: "Create a webhook. This is a write operation; run only when explicitly requested. " + capabilityHelp("webhook.write"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(createURL) == "" {
			return fail(fmt.Errorf("--url is required"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.write"); err != nil {
			return fail(err)
		}
		subject, path, _, err := webhookTarget(firstNonEmptyCLI(createRepository, flagRepository), firstNonEmptyCLI(createWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		if err = validateWebhookURL(createURL, createAllowHTTP, createAllowPrivate); err != nil {
			return fail(err)
		}
		events, err := validateWebhookEvents(ctx(cmd), client, subject, createEvents, createAllowUnknown, createNoCache)
		if err != nil {
			return fail(err)
		}
		secret, secretSet, err := readWebhookSecret(createSecretStdin, createSecretPrompt, createSecretEnv)
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		// Bitbucket's hook default is active; send the explicit value so the
		// declarative behavior is stable across API versions.
		createActiveSet = true
		body := webhookBody(createDescription, createURL, events, createActive, createActiveSet, secret, secretSet)
		if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
			return fail(redactWebhookError(err, secret))
		}
		return emitObjectFields(sanitizeWebhookJSON(raw), output.WebhookFields, output.WebhookSummary)
	}}
	addWebhookTargetFlags(createCmd, &createRepository, &createWorkspace)
	addWebhookWriteFlags(createCmd, &createURL, &createDescription, &createEvents, &createActive, &createActiveSet, &createSecretStdin, &createSecretPrompt, &createSecretEnv, &createAllowPrivate, &createAllowHTTP, &createAllowUnknown, &createNoCache)

	var editRepository, editWorkspace, editURL, editDescription, editSecretEnv string
	var editEvents []string
	var editActive, editSecretStdin, editSecretPrompt, editAllowPrivate, editAllowHTTP, editAllowUnknown, editNoCache bool
	editCmd := &cobra.Command{Use: "edit <uuid>", Short: "Edit a webhook", Long: "Edit a webhook. This is a write operation; only explicitly supplied fields are changed. " + capabilityHelp("webhook.write"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.write"); err != nil {
			return fail(err)
		}
		subject, path, _, err := webhookTarget(firstNonEmptyCLI(editRepository, flagRepository), firstNonEmptyCLI(editWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		var current map[string]any
		if err := client.Request(ctx(cmd), path+"/"+bitbucket.EncodePathSegment(args[0]), bitbucket.RequestOptions{}, &current); err != nil {
			return fail(err)
		}
		body := map[string]any{}
		for _, key := range []string{"description", "url", "events", "active"} {
			if value, ok := current[key]; ok {
				body[key] = value
			}
		}
		if cmd.Flags().Changed("url") {
			if err := validateWebhookURL(editURL, editAllowHTTP, editAllowPrivate); err != nil {
				return fail(err)
			}
			body["url"] = editURL
		}
		if cmd.Flags().Changed("description") {
			body["description"] = editDescription
		}
		if cmd.Flags().Changed("event") {
			events, e := validateWebhookEvents(ctx(cmd), client, subject, editEvents, editAllowUnknown, editNoCache)
			if e != nil {
				return fail(e)
			}
			body["events"] = events
		}
		if cmd.Flags().Changed("active") {
			body["active"] = editActive
		}
		secret, secretSet, err := readWebhookSecret(editSecretStdin, editSecretPrompt, editSecretEnv)
		if err != nil {
			return fail(err)
		}
		if secretSet {
			body["secret"] = secret
		}
		if len(body) == 0 || (!cmd.Flags().Changed("url") && !cmd.Flags().Changed("description") && !cmd.Flags().Changed("event") && !cmd.Flags().Changed("active") && !secretSet) {
			return fail(fmt.Errorf("at least one webhook field must be supplied"))
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), path+"/"+bitbucket.EncodePathSegment(args[0]), bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
			return fail(redactWebhookError(err, secret))
		}
		return emitObjectFields(sanitizeWebhookJSON(raw), output.WebhookFields, output.WebhookSummary)
	}}
	addWebhookTargetFlags(editCmd, &editRepository, &editWorkspace)
	addWebhookEditFlags(editCmd, &editURL, &editDescription, &editEvents, &editActive, &editSecretStdin, &editSecretPrompt, &editSecretEnv, &editAllowPrivate, &editAllowHTTP, &editAllowUnknown, &editNoCache)

	var deleteRepository, deleteWorkspace string
	var deleteYes bool
	deleteCmd := &cobra.Command{Use: "delete <uuid> --yes", Short: "Delete a webhook", Long: "Delete a webhook. This is a destructive write operation and requires --yes. " + capabilityHelp("webhook.delete"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !deleteYes {
			return fail(fmt.Errorf("refusing to delete webhook %q without --yes", args[0]))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.delete"); err != nil {
			return fail(err)
		}
		_, path, label, err := webhookTarget(firstNonEmptyCLI(deleteRepository, flagRepository), firstNonEmptyCLI(deleteWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(cmd), path+"/"+bitbucket.EncodePathSegment(args[0]), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		raw, _ := json.Marshal(map[string]any{"deleted": true, "uuid": args[0], "subject": label})
		return emitObject(raw, nil)
	}}
	addWebhookTargetFlags(deleteCmd, &deleteRepository, &deleteWorkspace)
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")

	var exportRepository, exportWorkspace, exportOutput string
	exportCmd := &cobra.Command{Use: "export --output <file>", Short: "Export webhooks as declarative YAML or JSON", Long: capabilityHelp("webhook.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if exportOutput == "" {
			return fail(fmt.Errorf("--output is required"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.read"); err != nil {
			return fail(err)
		}
		subject, path, label, err := webhookTarget(firstNonEmptyCLI(exportRepository, flagRepository), firstNonEmptyCLI(exportWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		values, err := client.PaginateAll(ctx(cmd), path, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		doc := exportWebhookDocument(subject, label, values)
		if err := writeWebhookDocument(exportOutput, doc); err != nil {
			return fail(err)
		}
		raw, _ := json.Marshal(doc)
		return emitObject(raw, nil)
	}}
	addWebhookTargetFlags(exportCmd, &exportRepository, &exportWorkspace)
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Output YAML or JSON file (required)")

	var applyFile, applyRepository, applyWorkspace string
	var applyDryRun, applyPrune, applyYes, applyAllowPrivate, applyAllowHTTP, applyAllowUnknown, applyNoCache bool
	applyCmd := &cobra.Command{Use: "apply --file <yaml|json>", Short: "Apply declarative webhook configuration", Long: "Apply webhook configuration. This is a write operation; --dry-run performs no mutations and --prune requires --yes. " + capabilityHelp("webhook.apply"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if applyFile == "" {
			return fail(fmt.Errorf("--file is required"))
		}
		doc, err := readWebhookDocument(applyFile)
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "webhook.read"); err != nil {
			return fail(err)
		}
		if applyPrune && !applyDryRun {
			if err = requireCapability(cfg, "webhook.delete"); err != nil {
				return fail(err)
			}
		}
		repository := firstNonEmptyCLI(applyRepository, flagRepository)
		workspace := firstNonEmptyCLI(applyWorkspace, flagWorkspace)
		if doc.Repository != "" {
			if repository != "" && repository != doc.Repository {
				return fail(fmt.Errorf("file repository %q conflicts with --repository %q", doc.Repository, repository))
			}
			repository = doc.Repository
		}
		if doc.Workspace != "" {
			if workspace != "" && workspace != doc.Workspace {
				return fail(fmt.Errorf("file workspace %q conflicts with --workspace %q", doc.Workspace, workspace))
			}
			workspace = doc.Workspace
		}
		subject, path, label, err := webhookTarget(repository, workspace)
		if err != nil {
			return fail(err)
		}
		if doc.Subject != "" && doc.Subject != subject {
			return fail(fmt.Errorf("file subject %q does not match target %q", doc.Subject, subject))
		}
		for i := range doc.Hooks {
			if err := validateWebhookURL(doc.Hooks[i].URL, applyAllowHTTP, applyAllowPrivate); err != nil {
				return fail(fmt.Errorf("hooks[%d]: %w", i, err))
			}
			events, e := validateWebhookEvents(ctx(cmd), client, subject, doc.Hooks[i].Events, applyAllowUnknown, applyNoCache)
			if e != nil {
				return fail(fmt.Errorf("hooks[%d]: %w", i, e))
			}
			doc.Hooks[i].Events = events
		}
		existing, err := client.PaginateAll(ctx(cmd), path, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		operations := planWebhookApply(doc.Hooks, existing, applyPrune)
		if applyPrune && !applyYes && hasDeleteOperation(operations) {
			return fail(fmt.Errorf("refusing to prune webhooks without --yes"))
		}
		if applyDryRun {
			raw, _ := json.Marshal(map[string]any{"subject": subject, "target": label, "dry_run": true, "operations": operations})
			return emitObject(raw, nil)
		}
		if err := requireCapability(cfg, "webhook.write"); err != nil {
			return fail(err)
		}
		result := executeWebhookApply(ctx(cmd), client, path, doc.Hooks, existing, operations)
		result["subject"], result["target"] = subject, label
		raw, _ := json.Marshal(result)
		if len(result["errors"].([]any)) > 0 {
			_ = emitObject(raw, nil)
			return fail(fmt.Errorf("webhook apply completed with partial failures"))
		}
		return emitObject(raw, nil)
	}}
	applyCmd.Flags().StringVar(&applyFile, "file", "", "YAML or JSON configuration file (required)")
	addWebhookTargetFlags(applyCmd, &applyRepository, &applyWorkspace)
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Report operations without changing Bitbucket")
	applyCmd.Flags().BoolVar(&applyPrune, "prune", false, "Delete hooks absent from the file")
	applyCmd.Flags().BoolVar(&applyYes, "yes", false, "Confirm pruning")
	applyCmd.Flags().BoolVar(&applyAllowPrivate, "allow-private", false, "Confirm non-public webhook destinations")
	applyCmd.Flags().BoolVar(&applyAllowHTTP, "allow-insecure-localhost", false, "Allow http://localhost destinations")
	applyCmd.Flags().BoolVar(&applyAllowUnknown, "allow-unknown-event", false, "Skip event catalog validation")
	applyCmd.Flags().BoolVar(&applyNoCache, "no-event-cache", false, "Do not read or write the event catalog cache")

	webhookCmd.AddCommand(eventsCmd, listCmd, viewCmd, createCmd, editCmd, deleteCmd, exportCmd, applyCmd)
	rootCmd.AddCommand(webhookCmd)
}

func addWebhookTargetFlags(cmd *cobra.Command, repository, workspace *string) {
	cmd.Flags().StringVar(repository, "repository", "", "Repository selector (workspace/repo or URL)")
	cmd.Flags().StringVar(workspace, "workspace", "", "Workspace slug or UUID")
}
func addWebhookWriteFlags(cmd *cobra.Command, targetURL, description *string, events *[]string, active, activeSet, stdin, prompt *bool, env *string, private, insecure, unknown, noCache *bool) {
	cmd.Flags().StringVar(targetURL, "url", "", "HTTPS webhook URL (required)")
	cmd.Flags().StringVar(description, "description", "", "Webhook description")
	cmd.Flags().StringSliceVar(events, "event", nil, "Webhook event key (repeatable)")
	cmd.Flags().BoolVar(active, "active", true, "Whether the webhook is active")
	cmd.Flags().BoolVar(activeSet, "active-set", false, "Send the active value (including false)")
	cmd.Flags().BoolVar(stdin, "secret-stdin", false, "Read the secret from stdin; never pass it as an argument")
	cmd.Flags().BoolVar(prompt, "secret-prompt", false, "Read the secret from a hidden TTY prompt")
	cmd.Flags().StringVar(env, "secret-env", "", "Read the secret from this environment-variable name")
	cmd.Flags().BoolVar(private, "allow-private", false, "Confirm non-public webhook destinations")
	cmd.Flags().BoolVar(insecure, "allow-insecure-localhost", false, "Allow http://localhost destinations")
	cmd.Flags().BoolVar(unknown, "allow-unknown-event", false, "Skip event catalog validation")
	cmd.Flags().BoolVar(noCache, "no-event-cache", false, "Do not read or write the event catalog cache")
}
func addWebhookEditFlags(cmd *cobra.Command, targetURL, description *string, events *[]string, active, stdin, prompt *bool, env *string, private, insecure, unknown, noCache *bool) {
	cmd.Flags().StringVar(targetURL, "url", "", "New HTTPS webhook URL")
	cmd.Flags().StringVar(description, "description", "", "New webhook description")
	cmd.Flags().StringSliceVar(events, "event", nil, "Replace webhook event keys (repeatable)")
	cmd.Flags().BoolVar(active, "active", false, "Whether the webhook is active")
	cmd.Flags().BoolVar(stdin, "secret-stdin", false, "Read a replacement secret from stdin")
	cmd.Flags().BoolVar(prompt, "secret-prompt", false, "Read a replacement secret from a hidden TTY prompt")
	cmd.Flags().StringVar(env, "secret-env", "", "Read a replacement secret from this environment-variable name")
	cmd.Flags().BoolVar(private, "allow-private", false, "Confirm non-public webhook destinations")
	cmd.Flags().BoolVar(insecure, "allow-insecure-localhost", false, "Allow http://localhost destinations")
	cmd.Flags().BoolVar(unknown, "allow-unknown-event", false, "Skip event catalog validation")
	cmd.Flags().BoolVar(noCache, "no-event-cache", false, "Do not read or write the event catalog cache")
}
func sanitizeWebhookValues(values []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(values))
	for i, raw := range values {
		out[i] = sanitizeWebhookJSON(raw)
	}
	return out
}

// Declarative webhook file format. SecretRef is intentionally unresolved: the
// file never contains a webhook secret value.
type webhookDocument struct {
	Subject    string        `yaml:"subject" json:"subject"`
	Repository string        `yaml:"repository,omitempty" json:"repository,omitempty"`
	Workspace  string        `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Hooks      []webhookSpec `yaml:"hooks" json:"hooks"`
}
type webhookSpec struct {
	UUID        string   `yaml:"uuid,omitempty" json:"uuid,omitempty"`
	Description string   `yaml:"description" json:"description"`
	URL         string   `yaml:"url" json:"url"`
	Active      *bool    `yaml:"active,omitempty" json:"active,omitempty"`
	Events      []string `yaml:"events" json:"events"`
	SecretRef   string   `yaml:"secret_ref,omitempty" json:"secret_ref,omitempty"`
}

func exportWebhookDocument(subject, label string, values []json.RawMessage) webhookDocument {
	doc := webhookDocument{Subject: subject}
	if subject == "repository" {
		doc.Repository = label
	} else {
		doc.Workspace = label
	}
	for _, raw := range values {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		spec := webhookSpec{}
		spec.UUID, _ = m["uuid"].(string)
		spec.Description, _ = m["description"].(string)
		spec.URL, _ = m["url"].(string)
		spec.URL = redactWebhookURL(spec.URL)
		if active, ok := m["active"].(bool); ok {
			spec.Active = &active
		}
		if events, ok := m["events"].([]any); ok {
			for _, event := range events {
				if s, ok := event.(string); ok {
					spec.Events = append(spec.Events, s)
				}
			}
		}
		if _, ok := m["secret"]; ok {
			spec.SecretRef = "unresolved:" + spec.UUID
		}
		doc.Hooks = append(doc.Hooks, spec)
	}
	sort.Slice(doc.Hooks, func(i, j int) bool {
		if doc.Hooks[i].UUID != doc.Hooks[j].UUID {
			return doc.Hooks[i].UUID < doc.Hooks[j].UUID
		}
		return doc.Hooks[i].URL < doc.Hooks[j].URL
	})
	return doc
}
func writeWebhookDocument(path string, doc webhookDocument) error {
	var data []byte
	var err error
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		data, err = yaml.Marshal(doc)
	} else {
		data, err = json.MarshalIndent(doc, "", "  ")
		data = append(data, '\n')
	}
	if err != nil {
		return fmt.Errorf("encode webhook export: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write webhook export: %w", err)
	}
	return nil
}
func readWebhookDocument(path string) (webhookDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return webhookDocument{}, fmt.Errorf("read webhook file: %w", err)
	}
	var value any
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		if err := yaml.Unmarshal(data, &value); err != nil {
			return webhookDocument{}, fmt.Errorf("parse webhook YAML: %w", err)
		}
	} else if err := json.Unmarshal(data, &value); err != nil {
		return webhookDocument{}, fmt.Errorf("parse webhook JSON: %w", err)
	}
	normalized, _ := json.Marshal(normalizeYAML(value))
	var doc webhookDocument
	if err := json.Unmarshal(normalized, &doc); err != nil {
		return webhookDocument{}, fmt.Errorf("decode webhook file: %w", err)
	}
	if doc.Subject == "" {
		return webhookDocument{}, fmt.Errorf("webhook file subject is required")
	}
	return doc, nil
}
func normalizeHookURL(value string) string {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return strings.TrimRight(value, "/")
	}
	// Reconciliation must preserve query parameters: a query token is part of
	// the configured destination. Only display helpers are allowed to redact it.
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.User = nil
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}
func hookMap(value json.RawMessage) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(value, &m)
	return m
}
func hookEvents(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return uniqueStrings(out)
}
func hookMatches(spec webhookSpec, current map[string]any) bool {
	if spec.UUID != "" {
		uuid, _ := current["uuid"].(string)
		return uuid == spec.UUID
	}
	description, _ := current["description"].(string)
	targetURL, _ := current["url"].(string)
	return strings.TrimSpace(description) == strings.TrimSpace(spec.Description) && normalizeHookURL(targetURL) == normalizeHookURL(spec.URL)
}
func hookEqual(spec webhookSpec, current map[string]any) bool {
	description, _ := current["description"].(string)
	targetURL, _ := current["url"].(string)
	if description != spec.Description || normalizeHookURL(targetURL) != normalizeHookURL(spec.URL) {
		return false
	}
	if spec.Active != nil {
		active, ok := current["active"].(bool)
		if !ok || active != *spec.Active {
			return false
		}
	}
	return strings.Join(uniqueStrings(hookEvents(current["events"])), "\x00") == strings.Join(uniqueStrings(spec.Events), "\x00")
}
func hookBody(spec webhookSpec) map[string]any {
	body := map[string]any{"description": spec.Description, "url": spec.URL, "events": uniqueStrings(spec.Events)}
	if spec.Active != nil {
		body["active"] = *spec.Active
	}
	return body
}

type webhookOperation struct {
	Action      string `json:"action"`
	UUID        string `json:"uuid,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	matchURL    string `json:"-"`
}

func planWebhookApply(specs []webhookSpec, existing []json.RawMessage, prune bool) []webhookOperation {
	ops := []webhookOperation{}
	matched := map[string]bool{}
	for _, spec := range specs {
		found := map[string]any{}
		for _, raw := range existing {
			current := hookMap(raw)
			if hookMatches(spec, current) {
				found = current
				break
			}
		}
		uuid, _ := found["uuid"].(string)
		action := "create"
		if uuid != "" {
			matched[uuid] = true
			action = "update"
			if hookEqual(spec, found) {
				action = "no-op"
			}
		}
		ops = append(ops, webhookOperation{Action: action, UUID: uuidOr(spec.UUID, uuid), Description: spec.Description, URL: redactWebhookURL(spec.URL), matchURL: spec.URL})
	}
	if prune {
		for _, raw := range existing {
			current := hookMap(raw)
			uuid, _ := current["uuid"].(string)
			if uuid != "" && !matched[uuid] {
				description, _ := current["description"].(string)
				targetURL, _ := current["url"].(string)
				ops = append(ops, webhookOperation{Action: "delete", UUID: uuid, Description: description, URL: redactWebhookURL(targetURL), matchURL: targetURL})
			}
		}
	}
	return ops
}
func uuidOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func hasDeleteOperation(ops []webhookOperation) bool {
	for _, op := range ops {
		if op.Action == "delete" {
			return true
		}
	}
	return false
}
func executeWebhookApply(ctx context.Context, client *bitbucket.Client, path string, specs []webhookSpec, existing []json.RawMessage, operations []webhookOperation) map[string]any {
	result := map[string]any{"operations": operations, "applied": []any{}, "errors": []any{}}
	for _, op := range operations {
		if op.Action == "no-op" {
			continue
		}
		var spec *webhookSpec
		for i := range specs {
			if specs[i].UUID == op.UUID || (specs[i].UUID == "" && op.Action != "delete" && specs[i].Description == op.Description && normalizeHookURL(specs[i].URL) == normalizeHookURL(op.matchURL)) {
				spec = &specs[i]
				break
			}
		}
		var err error
		var raw json.RawMessage
		switch op.Action {
		case "create":
			if spec != nil {
				err = client.Request(ctx, path, bitbucket.RequestOptions{Method: http.MethodPost, Body: hookBody(*spec)}, &raw)
			}
		case "update":
			if spec != nil {
				err = client.Request(ctx, path+"/"+bitbucket.EncodePathSegment(op.UUID), bitbucket.RequestOptions{Method: http.MethodPut, Body: hookBody(*spec)}, &raw)
			}
		case "delete":
			err = client.Request(ctx, path+"/"+bitbucket.EncodePathSegment(op.UUID), bitbucket.RequestOptions{Method: http.MethodDelete}, nil)
		}
		if err != nil {
			result["errors"] = append(result["errors"].([]any), map[string]any{"operation": op, "error": err.Error()})
			continue
		}
		result["applied"] = append(result["applied"].([]any), op)
	}
	_ = existing
	return result
}

// SSH public-key helpers.
type parsedPublicKey struct{ Material, Algorithm, Fingerprint string }

func parsePublicKey(data []byte) (parsedPublicKey, error) {
	text := strings.TrimSpace(string(data))
	if strings.Contains(text, "PRIVATE KEY") || strings.Contains(text, "OPENSSH PRIVATE KEY") {
		return parsedPublicKey{}, fmt.Errorf("private-key material is not accepted")
	}
	var line string
	for _, candidate := range strings.Split(text, "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && !strings.HasPrefix(candidate, "#") {
			line = candidate
			break
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return parsedPublicKey{}, fmt.Errorf("expected an OpenSSH public key (algorithm base64 [comment])")
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return parsedPublicKey{}, fmt.Errorf("invalid public-key base64: %w", err)
	}
	if len(blob) < 4 {
		return parsedPublicKey{}, fmt.Errorf("invalid public-key blob")
	}
	n := int(binary.BigEndian.Uint32(blob[:4]))
	if n <= 0 || n+4 > len(blob) {
		return parsedPublicKey{}, fmt.Errorf("invalid public-key algorithm field")
	}
	algorithm := string(blob[4 : 4+n])
	if algorithm != fields[0] {
		return parsedPublicKey{}, fmt.Errorf("public-key algorithm %q does not match its blob", fields[0])
	}
	digest := sha256.Sum256(blob)
	material := fields[0] + " " + fields[1]
	if len(fields) > 2 {
		material += " " + strings.Join(fields[2:], " ")
	}
	return parsedPublicKey{Material: material, Algorithm: algorithm, Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])}, nil
}
func keyInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	if path == "" {
		return nil, fmt.Errorf("--file is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	return data, nil
}
func expiryValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if date, err := time.Parse("2006-01-02", value); err == nil {
		return date.UTC().Format(time.RFC3339), nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", fmt.Errorf("invalid expiry %q (use YYYY-MM-DD or RFC3339 with timezone)", value)
	}
	return parsed.UTC().Format(time.RFC3339), nil
}
func decorateKey(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if key, ok := m["key"].(string); ok {
		if parsed, err := parsePublicKey([]byte(key)); err == nil {
			m["algorithm"], m["fingerprint"] = parsed.Algorithm, parsed.Fingerprint
		}
	}
	clean, _ := json.Marshal(sanitizeKeyValue(m))
	return clean
}
func sanitizeKeyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			if strings.EqualFold(key, "key") && !outputFieldSelected("key") {
				continue
			}
			out[key] = sanitizeKeyValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = sanitizeKeyValue(child)
		}
		return out
	default:
		return value
	}
}
func decoratedKeys(values []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(values))
	for i, raw := range values {
		out[i] = decorateKey(raw)
	}
	return out
}
func keyPath(user, id string) string {
	path := "/users/" + bitbucket.EncodePathSegment(user) + "/ssh-keys"
	if id != "" {
		path += "/" + bitbucket.EncodePathSegment(id)
	}
	return path
}

func resolveSSHUser(ctx context.Context, client *bitbucket.Client, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector != "" {
		return keyUser(selector)
	}
	var account map[string]any
	if err := client.Request(ctx, "/user", bitbucket.RequestOptions{}, &account); err != nil {
		return "", fmt.Errorf("resolve authenticated user: %w", err)
	}
	resolved, err := accountSelector(account)
	if err != nil {
		return "", fmt.Errorf("resolve authenticated user: %w", err)
	}
	return resolved, nil
}

func keyID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "/?#") {
		return "", fmt.Errorf("invalid SSH key UUID %q", value)
	}
	return value, nil
}
func keyUser(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "/?#") {
		return "", fmt.Errorf("invalid --user selector %q", value)
	}
	return value, nil
}
func keyList(ctx context.Context, client *bitbucket.Client, user string, limit int) ([]json.RawMessage, error) {
	return client.Paginate(ctx, keyPath(user, "")+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), limit, bitbucket.DefaultMaxPages)
}
func keyDuplicate(values []json.RawMessage, fingerprint string) bool {
	for _, raw := range values {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if value, ok := m["fingerprint"].(string); ok && value == fingerprint {
			return true
		}
		if key, ok := m["key"].(string); ok {
			if parsed, err := parsePublicKey([]byte(key)); err == nil && parsed.Fingerprint == fingerprint {
				return true
			}
		}
	}
	return false
}

func init() {
	sshCmd := &cobra.Command{Use: "ssh-key", Short: "Manage account SSH keys"}
	var sshUser string
	var sshLimit int
	sshList := &cobra.Command{Use: "list", Short: "List account SSH keys", Long: capabilityHelp("ssh-key.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "ssh-key.read"); err != nil {
			return fail(err)
		}
		user, err := resolveSSHUser(ctx(cmd), client, sshUser)
		if err != nil {
			return fail(err)
		}
		values, err := keyList(ctx(cmd), client, user, sshLimit)
		if err != nil {
			return fail(err)
		}
		return emitListFields(decoratedKeys(values), output.SSHKeyFields, output.SSHKeySummary, "No SSH keys found.")
	}}
	sshList.Flags().StringVar(&sshUser, "user", "", "User selector for read operations (defaults to the authenticated user)")
	sshList.Flags().IntVar(&sshLimit, "limit", bitbucket.DefaultLimit, "Maximum keys to return")
	var sshViewUser string
	sshView := &cobra.Command{Use: "view <id>", Short: "View an account SSH key", Long: capabilityHelp("ssh-key.read"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := keyID(args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "ssh-key.read"); err != nil {
			return fail(err)
		}
		user, err := resolveSSHUser(ctx(cmd), client, sshViewUser)
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), keyPath(user, id), bitbucket.RequestOptions{}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(decorateKey(raw), output.SSHKeyFields, output.SSHKeySummary)
	}}
	sshView.Flags().StringVar(&sshViewUser, "user", "", "User selector for read operations")
	var sshAddUser, sshFile, sshLabel, sshExpires string
	sshAdd := &cobra.Command{Use: "add --file <public-key>", Short: "Add an account SSH key", Long: "Add an account SSH key. This is a write operation; private-key input is rejected.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if sshAddUser != "" {
			return fail(fmt.Errorf("writing another user's SSH keys is not implied; omit --user"))
		}
		data, err := keyInput(sshFile)
		if err != nil {
			return fail(err)
		}
		parsed, err := parsePublicKey(data)
		if err != nil {
			return fail(err)
		}
		expires, err := expiryValue(sshExpires)
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "ssh-key.write"); err != nil {
			return fail(err)
		}
		user, err := resolveSSHUser(ctx(cmd), client, "")
		if err != nil {
			return fail(err)
		}
		values, err := keyList(ctx(cmd), client, user, 0)
		if err != nil {
			return fail(err)
		}
		if keyDuplicate(values, parsed.Fingerprint) {
			return fail(fmt.Errorf("SSH key already exists (fingerprint %s)", parsed.Fingerprint))
		}
		body := map[string]any{"key": parsed.Material, "label": sshLabel}
		opts := bitbucket.RequestOptions{Method: http.MethodPost, Body: body}
		if expires != "" {
			opts.Query = url.Values{"expires_on": {expires}}
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), keyPath(user, ""), opts, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(decorateKey(raw), output.SSHKeyFields, output.SSHKeySummary)
	}}
	sshAdd.Flags().StringVar(&sshAddUser, "user", "", "Unsupported for writes; account SSH keys default to the authenticated user")
	sshAdd.Flags().StringVar(&sshFile, "file", "", "OpenSSH public-key file, or - for stdin (required)")
	sshAdd.Flags().StringVar(&sshLabel, "label", "", "Key label")
	sshAdd.Flags().StringVar(&sshExpires, "expires", "", "Expiry date (YYYY-MM-DD) or RFC3339")
	var sshEditUser, sshEditLabel string
	sshEdit := &cobra.Command{Use: "edit <id>", Short: "Edit an account SSH key", Long: "Edit an account SSH key. This is a write operation; only explicitly supplied fields change.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if sshEditUser != "" {
			return fail(fmt.Errorf("writing another user's SSH keys is not implied; omit --user"))
		}
		id, err := keyID(args[0])
		if err != nil {
			return fail(err)
		}
		if !cmd.Flags().Changed("label") {
			return fail(fmt.Errorf("--label must be supplied"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "ssh-key.write"); err != nil {
			return fail(err)
		}
		user, err := resolveSSHUser(ctx(cmd), client, "")
		if err != nil {
			return fail(err)
		}
		body := map[string]any{}
		if cmd.Flags().Changed("label") {
			body["label"] = sshEditLabel
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), keyPath(user, id), bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(decorateKey(raw), output.SSHKeyFields, output.SSHKeySummary)
	}}
	sshEdit.Flags().StringVar(&sshEditUser, "user", "", "Unsupported for writes; omit this flag")
	sshEdit.Flags().StringVar(&sshEditLabel, "label", "", "New key label")
	var sshDeleteUser string
	var sshDeleteYes bool
	sshDelete := &cobra.Command{Use: "delete <id> --yes", Short: "Delete an account SSH key", Long: "Delete an account SSH key. This is a destructive write operation and requires --yes.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !sshDeleteYes {
			return fail(fmt.Errorf("refusing to delete SSH key %q without --yes", args[0]))
		}
		if sshDeleteUser != "" {
			return fail(fmt.Errorf("writing another user's SSH keys is not implied; omit --user"))
		}
		id, err := keyID(args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "ssh-key.delete"); err != nil {
			return fail(err)
		}
		user, err := resolveSSHUser(ctx(cmd), client, "")
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(cmd), keyPath(user, id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		raw, _ := json.Marshal(map[string]any{"deleted": true, "uuid": id})
		return emitObject(raw, nil)
	}}
	sshDelete.Flags().StringVar(&sshDeleteUser, "user", "", "Unsupported for writes; omit this flag")
	sshDelete.Flags().BoolVar(&sshDeleteYes, "yes", false, "Confirm deletion")
	sshCmd.AddCommand(sshList, sshView, sshAdd, sshEdit, sshDelete)
	rootCmd.AddCommand(sshCmd)
}

func deployBase(repository string) (string, config.ResolvedRepoRef, error) {
	ref, err := parseRepositorySelector(repository)
	if err != nil {
		return "", config.ResolvedRepoRef{}, err
	}
	return fmt.Sprintf("/repositories/%s/%s/deploy-keys", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.RepoSlug)), ref, nil
}

func init() {
	deployCmd := &cobra.Command{Use: "deploy-key", Short: "Manage repository deploy keys (read-only Git access)"}
	var listRepository string
	var deployLimit int
	deployList := &cobra.Command{Use: "list", Short: "List repository deploy keys", Long: capabilityHelp("deploy-key.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "deploy-key.read"); err != nil {
			return fail(err)
		}
		path, _, err := deployBase(firstNonEmptyCLI(listRepository, flagRepository))
		if err != nil {
			return fail(err)
		}
		values, err := client.Paginate(ctx(cmd), path+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), deployLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(decoratedKeys(values), output.DeployKeyFields, output.DeployKeySummary, "No deploy keys found.")
	}}
	deployList.Flags().StringVarP(&listRepository, "repository", "R", "", "Repository selector (workspace/repo or URL)")
	deployList.Flags().IntVar(&deployLimit, "limit", bitbucket.DefaultLimit, "Maximum deploy keys to return")
	var addRepository, deployFile, deployLabel string
	deployAdd := &cobra.Command{Use: "add --file <public-key>", Short: "Add a repository deploy key", Long: "Add a repository deploy key. Deploy keys are read-only for Git access; private-key input is rejected.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := keyInput(deployFile)
		if err != nil {
			return fail(err)
		}
		parsed, err := parsePublicKey(data)
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "deploy-key.write"); err != nil {
			return fail(err)
		}
		path, _, err := deployBase(firstNonEmptyCLI(addRepository, flagRepository))
		if err != nil {
			return fail(err)
		}
		values, err := client.Paginate(ctx(cmd), path+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), 0, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		if keyDuplicate(values, parsed.Fingerprint) {
			return fail(fmt.Errorf("deploy key already exists in this repository (fingerprint %s)", parsed.Fingerprint))
		}
		body := map[string]any{"key": parsed.Material, "label": deployLabel}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(decorateKey(raw), output.DeployKeyFields, output.DeployKeySummary)
	}}
	deployAdd.Flags().StringVarP(&addRepository, "repository", "R", "", "Repository selector (required)")
	deployAdd.Flags().StringVar(&deployFile, "file", "", "OpenSSH public-key file, or - for stdin (required)")
	deployAdd.Flags().StringVar(&deployLabel, "label", "", "Key label")
	var delRepository string
	var delYes bool
	deployDelete := &cobra.Command{Use: "delete <id> --yes", Short: "Delete a repository deploy key", Long: "Delete a repository deploy key. This is a destructive write operation and requires --yes.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !delYes {
			return fail(fmt.Errorf("refusing to delete deploy key %q without --yes", args[0]))
		}
		id, err := parsePositiveID("deploy key id", args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err = requireCapability(cfg, "deploy-key.delete"); err != nil {
			return fail(err)
		}
		path, _, err := deployBase(firstNonEmptyCLI(delRepository, flagRepository))
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(cmd), path+"/"+fmt.Sprint(id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		raw, _ := json.Marshal(map[string]any{"deleted": true, "id": id})
		return emitObject(raw, nil)
	}}
	deployDelete.Flags().StringVarP(&delRepository, "repository", "R", "", "Repository selector (required)")
	deployDelete.Flags().BoolVar(&delYes, "yes", false, "Confirm deletion")
	deployCmd.AddCommand(deployList, deployAdd, deployDelete)
	rootCmd.AddCommand(deployCmd)
}
