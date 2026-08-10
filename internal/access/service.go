// Package access contains use-cases for Bitbucket access-management features.
//
// The package deliberately knows nothing about Cobra, command flags, or the
// process environment. Commands translate CLI input into these typed options
// and are responsible only for capability checks and presentation.
package access

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/selector"
)

// APIClient is the smallest external-system seam required by access services.
// *bitbucket.Client satisfies it, while tests can provide a request recorder.
type APIClient interface {
	Request(context.Context, string, bitbucket.RequestOptions, any) error
	Paginate(context.Context, string, int, int) ([]json.RawMessage, error)
	PaginateAll(context.Context, string, int) ([]json.RawMessage, error)
}

type Service struct{ client APIClient }

func New(client APIClient) *Service { return &Service{client: client} }

// WebhookTarget is the resolved API target for repository- or workspace-level
// hooks.
type WebhookTarget struct {
	Subject string
	Path    string
	Label   string
}

func ResolveWebhookTarget(repository, workspace string) (WebhookTarget, error) {
	repository, workspace = strings.TrimSpace(repository), strings.TrimSpace(workspace)
	if repository != "" && workspace != "" {
		return WebhookTarget{}, fmt.Errorf("--repository cannot be combined with --workspace")
	}
	if repository != "" {
		ref, err := selector.RepositorySelector(repository)
		if err != nil {
			return WebhookTarget{}, err
		}
		base := fmt.Sprintf("/repositories/%s/%s", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.Repo))
		return WebhookTarget{"repository", base + "/hooks", ref.Workspace + "/" + ref.Repo}, nil
	}
	if workspace == "" {
		return WebhookTarget{}, fmt.Errorf("one of --repository or --workspace is required")
	}
	if strings.ContainsAny(workspace, "/?#") {
		return WebhookTarget{}, fmt.Errorf("invalid workspace selector %q", workspace)
	}
	return WebhookTarget{"workspace", "/workspaces/" + bitbucket.EncodePathSegment(workspace) + "/hooks", workspace}, nil
}

func (s *Service) ListWebhooks(ctx context.Context, target WebhookTarget, limit int) ([]json.RawMessage, error) {
	return s.client.Paginate(ctx, target.Path+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), limit, bitbucket.DefaultMaxPages)
}

func (s *Service) ListAllWebhooks(ctx context.Context, target WebhookTarget) ([]json.RawMessage, error) {
	return s.client.PaginateAll(ctx, target.Path, bitbucket.DefaultMaxPages)
}

func (s *Service) ViewWebhook(ctx context.Context, target WebhookTarget, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.client.Request(ctx, target.Path+"/"+bitbucket.EncodePathSegment(id), bitbucket.RequestOptions{}, &raw)
	return raw, err
}

// WebhookCreate contains only API workflow data; secret input and validation
// policy are resolved by the command before this value is passed in.
type WebhookCreate struct {
	Description string
	URL         string
	Events      []string
	Active      bool
	Secret      string
	SecretSet   bool
}

func (o WebhookCreate) body() map[string]any {
	body := map[string]any{"description": o.Description, "url": o.URL, "events": o.Events, "active": o.Active}
	if o.SecretSet {
		body["secret"] = o.Secret
	}
	return body
}

func (s *Service) CreateWebhook(ctx context.Context, target WebhookTarget, options WebhookCreate) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.client.Request(ctx, target.Path, bitbucket.RequestOptions{Method: http.MethodPost, Body: options.body()}, &raw)
	return raw, err
}

type WebhookEdit struct {
	Description *string
	URL         *string
	Events      *[]string
	Active      *bool
	Secret      *string
}

func (s *Service) EditWebhook(ctx context.Context, target WebhookTarget, id string, edit WebhookEdit) (json.RawMessage, error) {
	var current map[string]any
	path := target.Path + "/" + bitbucket.EncodePathSegment(id)
	if err := s.client.Request(ctx, path, bitbucket.RequestOptions{}, &current); err != nil {
		return nil, err
	}
	body := map[string]any{}
	for _, key := range []string{"description", "url", "events", "active"} {
		if value, ok := current[key]; ok {
			body[key] = value
		}
	}
	if edit.Description != nil {
		body["description"] = *edit.Description
	}
	if edit.URL != nil {
		body["url"] = *edit.URL
	}
	if edit.Events != nil {
		body["events"] = *edit.Events
	}
	if edit.Active != nil {
		body["active"] = *edit.Active
	}
	if edit.Secret != nil {
		body["secret"] = *edit.Secret
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("at least one webhook field must be supplied")
	}
	var raw json.RawMessage
	err := s.client.Request(ctx, path, bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw)
	return raw, err
}

func (s *Service) DeleteWebhook(ctx context.Context, target WebhookTarget, id string) error {
	return s.client.Request(ctx, target.Path+"/"+bitbucket.EncodePathSegment(id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil)
}

// WebhookSpec and WebhookOperation are the structured inputs and audit plan
// for declarative reconciliation. They intentionally contain no presentation
// strings or Cobra state.
type WebhookSpec struct {
	UUID        string
	Description string
	URL         string
	Active      *bool
	Events      []string
}

type WebhookOperation struct {
	Action      string `json:"action"`
	UUID        string `json:"uuid,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	MatchURL    string `json:"-"`
}

func webhookBody(spec WebhookSpec) map[string]any {
	body := map[string]any{"description": spec.Description, "url": spec.URL, "events": UniqueStrings(spec.Events)}
	if spec.Active != nil {
		body["active"] = *spec.Active
	}
	return body
}

// ApplyWebhooks executes a previously reviewed declarative plan and returns
// structured audit data. Planning and confirmation remain command concerns.
func (s *Service) ApplyWebhooks(ctx context.Context, target WebhookTarget, specs []WebhookSpec, operations []WebhookOperation) map[string]any {
	result := map[string]any{"operations": operations, "applied": []any{}, "errors": []any{}}
	for _, operation := range operations {
		if operation.Action == "no-op" {
			continue
		}
		var spec *WebhookSpec
		for i := range specs {
			if (specs[i].UUID != "" && specs[i].UUID == operation.UUID) ||
				(specs[i].UUID == "" && operation.Action != "delete" && specs[i].Description == operation.Description && strings.TrimRight(specs[i].URL, "/") == strings.TrimRight(operation.MatchURL, "/")) {
				spec = &specs[i]
				break
			}
		}
		var err error
		var raw json.RawMessage
		switch operation.Action {
		case "create":
			if spec != nil {
				err = s.client.Request(ctx, target.Path, bitbucket.RequestOptions{Method: http.MethodPost, Body: webhookBody(*spec)}, &raw)
			}
		case "update":
			if spec != nil {
				err = s.client.Request(ctx, target.Path+"/"+bitbucket.EncodePathSegment(operation.UUID), bitbucket.RequestOptions{Method: http.MethodPut, Body: webhookBody(*spec)}, &raw)
			}
		case "delete":
			err = s.client.Request(ctx, target.Path+"/"+bitbucket.EncodePathSegment(operation.UUID), bitbucket.RequestOptions{Method: http.MethodDelete}, nil)
		default:
			err = fmt.Errorf("unsupported webhook operation %q", operation.Action)
		}
		if err != nil {
			result["errors"] = append(result["errors"].([]any), map[string]any{"operation": operation, "error": err.Error()})
			continue
		}
		result["applied"] = append(result["applied"].([]any), operation)
	}
	return result
}

// EventCatalogCache is an optional boundary for the CLI's local cache. The
// service does not know where or how the cache is persisted.
type EventCatalogCache interface {
	Get(subject string) ([]string, bool)
	Put(subject string, events []string)
}

type EventCatalogOptions struct{ Cache EventCatalogCache }

func (s *Service) EventCatalog(ctx context.Context, subject string, options EventCatalogOptions) ([]string, error) {
	if options.Cache != nil {
		if events, ok := options.Cache.Get(subject); ok && len(events) > 0 {
			return events, nil
		}
	}
	var raw json.RawMessage
	if err := s.client.Request(ctx, "/hook_events/"+bitbucket.EncodePathSegment(subject), bitbucket.RequestOptions{}, &raw); err != nil {
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
	events = UniqueStrings(events)
	if len(events) == 0 {
		return nil, fmt.Errorf("webhook event catalog for %s was empty or had an unsupported shape", subject)
	}
	if options.Cache != nil {
		options.Cache.Put(subject, events)
	}
	return events, nil
}

func UniqueStrings(values []string) []string {
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

func ValidateWebhookEvents(ctx context.Context, service *Service, subject string, requested []string, allowUnknown bool, options EventCatalogOptions) ([]string, error) {
	requested = UniqueStrings(requested)
	if len(requested) == 0 {
		return nil, fmt.Errorf("at least one --event is required")
	}
	if allowUnknown {
		return requested, nil
	}
	valid, err := service.EventCatalog(ctx, subject, options)
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

func eventSuggestion(value string, valid []string) string {
	best := ""
	bestDistance := 999
	for _, candidate := range valid {
		distance := levenshtein(strings.ToLower(value), strings.ToLower(candidate))
		if distance < bestDistance {
			best, bestDistance = candidate, distance
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
			row[j+1] = min(row[j+1]+1, row[j]+1, previous+cost)
			previous = old
		}
	}
	return row[len(b)]
}

func min(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

// PublicKey is normalized public-key material plus stable metadata.
type PublicKey struct{ Material, Algorithm, Fingerprint string }

func ParsePublicKey(data []byte) (PublicKey, error) {
	text := strings.TrimSpace(string(data))
	if strings.Contains(text, "PRIVATE KEY") {
		return PublicKey{}, fmt.Errorf("private-key material is not accepted")
	}
	line := ""
	for _, candidate := range strings.Split(text, "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && !strings.HasPrefix(candidate, "#") {
			line = candidate
			break
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return PublicKey{}, fmt.Errorf("expected an OpenSSH public key (algorithm base64 [comment])")
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return PublicKey{}, fmt.Errorf("invalid public-key base64: %w", err)
	}
	if len(blob) < 4 {
		return PublicKey{}, fmt.Errorf("invalid public-key blob")
	}
	n := int(binary.BigEndian.Uint32(blob[:4]))
	if n <= 0 || n+4 > len(blob) {
		return PublicKey{}, fmt.Errorf("invalid public-key algorithm field")
	}
	algorithm := string(blob[4 : 4+n])
	if algorithm != fields[0] {
		return PublicKey{}, fmt.Errorf("public-key algorithm %q does not match its blob", fields[0])
	}
	digest := sha256.Sum256(blob)
	material := fields[0] + " " + fields[1]
	if len(fields) > 2 {
		material += " " + strings.Join(fields[2:], " ")
	}
	return PublicKey{material, algorithm, "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])}, nil
}

func accountSelector(account map[string]any) (string, error) {
	for _, key := range []string{"uuid", "account_id", "nickname", "username"} {
		if value := strings.TrimSpace(fmt.Sprint(account[key])); value != "" && value != "<nil>" {
			return value, nil
		}
	}
	return "", fmt.Errorf("resolved user did not contain an account UUID, account ID, or nickname")
}

func keySelector(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "/?#") {
		return "", fmt.Errorf("invalid %s %q", label, value)
	}
	return value, nil
}

func userKeyPath(user, id string) string {
	path := "/users/" + bitbucket.EncodePathSegment(user) + "/ssh-keys"
	if id != "" {
		path += "/" + bitbucket.EncodePathSegment(id)
	}
	return path
}

func (s *Service) ResolveSSHUser(ctx context.Context, user string) (string, error) {
	if strings.TrimSpace(user) != "" {
		return keySelector(user, "--user selector")
	}
	var account map[string]any
	if err := s.client.Request(ctx, "/user", bitbucket.RequestOptions{}, &account); err != nil {
		return "", fmt.Errorf("resolve authenticated user: %w", err)
	}
	resolved, err := accountSelector(account)
	if err != nil {
		return "", fmt.Errorf("resolve authenticated user: %w", err)
	}
	return resolved, nil
}

func (s *Service) ListSSHKeys(ctx context.Context, user string, limit int) ([]json.RawMessage, error) {
	return s.client.Paginate(ctx, userKeyPath(user, "")+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), limit, bitbucket.DefaultMaxPages)
}
func (s *Service) ViewSSHKey(ctx context.Context, user, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.client.Request(ctx, userKeyPath(user, id), bitbucket.RequestOptions{}, &raw)
	return raw, err
}
func (s *Service) AddSSHKey(ctx context.Context, user string, key PublicKey, label, expires string) (json.RawMessage, error) {
	values, err := s.ListSSHKeys(ctx, user, 0)
	if err != nil {
		return nil, err
	}
	if KeyDuplicate(values, key.Fingerprint) {
		return nil, fmt.Errorf("SSH key already exists (fingerprint %s)", key.Fingerprint)
	}
	opts := bitbucket.RequestOptions{Method: http.MethodPost, Body: map[string]any{"key": key.Material, "label": label}}
	if expires != "" {
		opts.Query = url.Values{"expires_on": {expires}}
	}
	var raw json.RawMessage
	err = s.client.Request(ctx, userKeyPath(user, ""), opts, &raw)
	return raw, err
}
func (s *Service) EditSSHKey(ctx context.Context, user, id, label string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.client.Request(ctx, userKeyPath(user, id), bitbucket.RequestOptions{Method: http.MethodPut, Body: map[string]any{"label": label}}, &raw)
	return raw, err
}
func (s *Service) DeleteSSHKey(ctx context.Context, user, id string) error {
	return s.client.Request(ctx, userKeyPath(user, id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil)
}

func KeyDuplicate(values []json.RawMessage, fingerprint string) bool {
	for _, raw := range values {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if value, ok := m["fingerprint"].(string); ok && value == fingerprint {
			return true
		}
		if key, ok := m["key"].(string); ok {
			if parsed, err := ParsePublicKey([]byte(key)); err == nil && parsed.Fingerprint == fingerprint {
				return true
			}
		}
	}
	return false
}

func ResolveDeployTarget(repository string) (string, error) {
	ref, err := selector.RepositorySelector(repository)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/repositories/%s/%s/deploy-keys", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.Repo)), nil
}
func (s *Service) ListDeployKeys(ctx context.Context, path string, limit int) ([]json.RawMessage, error) {
	return s.client.Paginate(ctx, path+"?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), limit, bitbucket.DefaultMaxPages)
}
func (s *Service) AddDeployKey(ctx context.Context, path string, key PublicKey, label string) (json.RawMessage, error) {
	values, err := s.ListDeployKeys(ctx, path, 0)
	if err != nil {
		return nil, err
	}
	if KeyDuplicate(values, key.Fingerprint) {
		return nil, fmt.Errorf("deploy key already exists in this repository (fingerprint %s)", key.Fingerprint)
	}
	var raw json.RawMessage
	err = s.client.Request(ctx, path, bitbucket.RequestOptions{Method: http.MethodPost, Body: map[string]any{"key": key.Material, "label": label}}, &raw)
	return raw, err
}
func (s *Service) DeleteDeployKey(ctx context.Context, path, id string) error {
	id = strings.TrimSpace(id)
	numericID, err := strconv.Atoi(id)
	if err != nil || numericID <= 0 {
		return fmt.Errorf("invalid deploy key id %q", id)
	}
	return s.client.Request(ctx, path+"/"+bitbucket.EncodePathSegment(strconv.Itoa(numericID)), bitbucket.RequestOptions{Method: http.MethodDelete}, nil)
}
