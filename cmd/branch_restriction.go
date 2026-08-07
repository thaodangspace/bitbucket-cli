package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
	"gopkg.in/yaml.v3"
)

var restrictionKinds = []string{"push", "delete", "force", "restrict_merges", "require_tasks_to_be_completed", "require_approvals_to_merge", "require_review_group_approvals_to_merge", "require_default_reviewer_approvals_to_merge", "require_no_changes_requested", "require_passing_builds_to_merge", "require_commits_behind", "reset_pullrequest_approvals_on_change", "smart_reset_pullrequest_approvals", "reset_pullrequest_changes_requested_on_change", "require_all_dependencies_merged", "enforce_merge_checks", "allow_auto_merge_when_builds_pass", "require_all_comments_resolved"}
var numericRestrictionKinds = map[string]bool{"require_approvals_to_merge": true, "require_review_group_approvals_to_merge": true, "require_default_reviewer_approvals_to_merge": true, "require_passing_builds_to_merge": true, "require_commits_behind": true}

func init() {
	cmd := &cobra.Command{Use: "branch-restriction", Short: "Branch restriction commands"}
	var kind, pattern, branchType, exportFile string
	var limit int
	listCmd := &cobra.Command{Use: "list", Short: "List branch restrictions", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
		if kind != "" {
			if err := validateRestrictionKind(kind); err != nil {
				return fail(err)
			}
			q.Set("kind", kind)
		}
		if pattern != "" {
			q.Set("pattern", pattern)
		}
		if branchType != "" {
			if err := validateBranchType(branchType); err != nil {
				return fail(err)
			}
			q.Set("branch_type", branchType)
		}
		values, err := client.Paginate(ctx(c), base+"/branch-restrictions?"+q.Encode(), limit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		if branchType != "" {
			values = filterRestrictionsByBranchType(values, branchType)
		}
		if exportFile != "" {
			if exportFile == "-" {
				if err := emitListFields(values, output.RestrictionFields, output.RestrictionSummary, "No branch restrictions found."); err != nil {
					return fail(err)
				}
				return nil
			}
			if err := exportRestrictions(exportFile, values); err != nil {
				return fail(err)
			}
		}
		return emitListFields(values, output.RestrictionFields, output.RestrictionSummary, "No branch restrictions found.")
	}}
	listCmd.Flags().StringVar(&kind, "kind", "", "Restriction kind")
	listCmd.Flags().StringVar(&pattern, "pattern", "", "Glob pattern")
	listCmd.Flags().StringVar(&branchType, "branch-type", "", "Branching-model branch type")
	listCmd.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum restrictions to return")
	listCmd.Flags().StringVar(&exportFile, "export", "", "Export restrictions to a YAML or JSON file (omit the value for stdout)")
	listCmd.Flags().Lookup("export").NoOptDefVal = "-"

	viewCmd := &cobra.Command{Use: "view <id>", Short: "View a branch restriction", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		id, err := parsePositiveID("restriction id", args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(c), fmt.Sprintf("%s/branch-restrictions/%d", base, id), bitbucket.RequestOptions{}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(raw, output.RestrictionFields, output.RestrictionSummary)
	}}

	var fromFile string
	var createKind, createPattern, createBranchType string
	var users, groups []string
	var value, approvals, builds int
	createCmd := &cobra.Command{Use: "create --kind <kind> (--pattern <glob>|--branch-type <type>)", Short: "Create a branch restriction", Long: "Create a branch restriction. This is a write operation; run it only when explicitly requested.", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		body, err := restrictionBody(c, cfg, client, createKind, createPattern, createBranchType, users, groups, value, approvals, builds, fromFile)
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(c), base+"/branch-restrictions", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(raw, output.RestrictionFields, output.RestrictionSummary)
	}}
	addRestrictionFlags(createCmd, &createKind, &createPattern, &createBranchType, &users, &groups, &value, &approvals, &builds, &fromFile)

	var editFile string
	var editKind, editPattern, editBranchType string
	var editUsers, editGroups []string
	var editValue, editApprovals, editBuilds int
	editCmd := &cobra.Command{Use: "edit <id> [flags]", Short: "Edit a branch restriction", Long: "Edit a branch restriction. This is a write operation; only explicitly supplied fields are changed.", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		id, err := parsePositiveID("restriction id", args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		var current map[string]any
		if err := client.Request(ctx(c), fmt.Sprintf("%s/branch-restrictions/%d", base, id), bitbucket.RequestOptions{}, &current); err != nil {
			return fail(err)
		}
		body := cloneMap(current)
		if editFile != "" {
			loaded, e := loadPolicyFile(editFile)
			if e != nil {
				return fail(e)
			}
			for k, v := range loaded {
				body[k] = v
			}
		}
		if c.Flags().Changed("kind") {
			body["kind"] = editKind
		}
		if c.Flags().Changed("pattern") {
			body["pattern"] = editPattern
			delete(body, "branch_type")
			body["branch_match_kind"] = "glob"
		}
		if c.Flags().Changed("branch-type") {
			body["branch_type"] = editBranchType
			delete(body, "pattern")
			body["branch_match_kind"] = "branching_model"
		}
		if c.Flags().Changed("user") {
			body["users"], err = resolveRestrictionUsers(ctx(c), cfg, client, editUsers)
			if err != nil {
				return fail(err)
			}
		}
		if c.Flags().Changed("group") {
			body["groups"], err = resolveRestrictionGroups(ctx(c), cfg, client, editGroups)
			if err != nil {
				return fail(err)
			}
		}
		if c.Flags().Changed("value") {
			body["value"] = editValue
		}
		if c.Flags().Changed("approvals") {
			body["value"] = editApprovals
		}
		if c.Flags().Changed("builds") {
			body["value"] = editBuilds
		}
		if !c.Flags().Changed("from-file") && !c.Flags().Changed("kind") && !c.Flags().Changed("pattern") && !c.Flags().Changed("branch-type") && !c.Flags().Changed("user") && !c.Flags().Changed("group") && !c.Flags().Changed("value") && !c.Flags().Changed("approvals") && !c.Flags().Changed("builds") {
			return fail(fmt.Errorf("at least one restriction field or --from-file must be supplied"))
		}
		if err := validateRestrictionBody(body); err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(c), fmt.Sprintf("%s/branch-restrictions/%d", base, id), bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(raw, output.RestrictionFields, output.RestrictionSummary)
	}}
	addRestrictionFlags(editCmd, &editKind, &editPattern, &editBranchType, &editUsers, &editGroups, &editValue, &editApprovals, &editBuilds, &editFile)

	var deleteYes bool
	deleteCmd := &cobra.Command{Use: "delete <id> --yes", Short: "Delete a branch restriction", Long: "Delete a branch restriction. This is a destructive write operation and requires --yes.", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		id, err := parsePositiveID("restriction id", args[0])
		if err != nil {
			return fail(err)
		}
		if !deleteYes {
			return fail(fmt.Errorf("refusing to delete restriction %d without --yes", id))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(c), fmt.Sprintf("%s/branch-restrictions/%d", base, id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return emitObject(json.RawMessage(fmt.Sprintf(`{"deleted":%d}`, id)), nil)
	}}
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")
	cmd.AddCommand(listCmd, viewCmd, createCmd, editCmd, deleteCmd)
	rootCmd.AddCommand(cmd)
}

func addRestrictionFlags(cmd *cobra.Command, kind, pattern, branchType *string, users, groups *[]string, value, approvals, builds *int, file *string) {
	cmd.Flags().StringVar(kind, "kind", "", "Restriction kind")
	cmd.Flags().StringVar(pattern, "pattern", "", "Glob pattern")
	cmd.Flags().StringVar(branchType, "branch-type", "", "Branching-model branch type")
	cmd.Flags().StringSliceVar(users, "user", nil, "User selector (repeatable)")
	cmd.Flags().StringSliceVar(groups, "group", nil, "Group selector (repeatable)")
	cmd.Flags().IntVar(value, "value", 0, "Kind-specific numeric requirement")
	cmd.Flags().IntVar(approvals, "approvals", 0, "Required approvals")
	cmd.Flags().IntVar(builds, "builds", 0, "Required passing builds")
	cmd.Flags().StringVar(file, "from-file", "", "Read policy fields from YAML or JSON")
}

func restrictionBody(cmd *cobra.Command, cfg config.Config, client *bitbucket.Client, kind, pattern, branchType string, users, groups []string, value, approvals, builds int, file string) (map[string]any, error) {
	body := map[string]any{}
	if file != "" {
		loaded, err := loadPolicyFile(file)
		if err != nil {
			return nil, err
		}
		body = loaded
	}
	if cmd.Flags().Changed("kind") || kind != "" {
		body["kind"] = kind
	}
	if cmd.Flags().Changed("pattern") || pattern != "" {
		body["pattern"] = pattern
		delete(body, "branch_type")
		body["branch_match_kind"] = "glob"
	}
	if cmd.Flags().Changed("branch-type") || branchType != "" {
		body["branch_type"] = branchType
		delete(body, "pattern")
		body["branch_match_kind"] = "branching_model"
	}
	if cmd.Flags().Changed("user") {
		var err error
		body["users"], err = resolveRestrictionUsers(ctx(cmd), cfg, client, users)
		if err != nil {
			return nil, err
		}
	}
	if cmd.Flags().Changed("group") {
		var err error
		body["groups"], err = resolveRestrictionGroups(ctx(cmd), cfg, client, groups)
		if err != nil {
			return nil, err
		}
	}
	if cmd.Flags().Changed("value") {
		body["value"] = value
	}
	if cmd.Flags().Changed("approvals") {
		body["value"] = approvals
	}
	if cmd.Flags().Changed("builds") {
		body["value"] = builds
	}
	if err := validateRestrictionBody(body); err != nil {
		return nil, err
	}
	return body, nil
}

func validateRestrictionKind(kind string) error {
	if !contains(restrictionKinds, strings.ToLower(kind)) {
		return fmt.Errorf("invalid restriction kind %q", kind)
	}
	return nil
}
func validateBranchType(value string) error {
	if !contains([]string{"feature", "bugfix", "release", "hotfix", "development", "production"}, strings.ToLower(value)) {
		return fmt.Errorf("invalid branch type %q", value)
	}
	return nil
}
func validateRestrictionBody(body map[string]any) error {
	kind, _ := body["kind"].(string)
	kind = strings.ToLower(strings.TrimSpace(kind))
	body["kind"] = kind
	if err := validateRestrictionKind(kind); err != nil {
		return err
	}
	mode, _ := body["branch_match_kind"].(string)
	if mode == "" {
		if body["branch_type"] != nil {
			mode = "branching_model"
		} else if body["pattern"] != nil {
			mode = "glob"
		}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	body["branch_match_kind"] = mode
	if mode != "glob" && mode != "branching_model" {
		return fmt.Errorf("exactly one match mode is required: provide --pattern or --branch-type")
	}
	hasPattern := nonEmptyRestrictionField(body, "pattern")
	hasType := nonEmptyRestrictionField(body, "branch_type")
	if hasPattern == hasType {
		return fmt.Errorf("exactly one of --pattern or --branch-type is required")
	}
	if mode == "glob" {
		if err := validateNonEmpty("pattern", fmt.Sprint(body["pattern"])); err != nil {
			return err
		}
	}
	if mode == "branching_model" {
		if err := validateBranchType(fmt.Sprint(body["branch_type"])); err != nil {
			return err
		}
	}
	if kind == "push" || kind == "restrict_merges" {
		if _, ok := body["users"]; !ok {
			body["users"] = []any{}
		}
		if _, ok := body["groups"]; !ok {
			body["groups"] = []any{}
		}
	}
	if (hasRestrictionEntries(body["users"]) || hasRestrictionEntries(body["groups"])) && kind != "push" && kind != "restrict_merges" {
		return fmt.Errorf("users and groups are exceptions supported only for push and restrict_merges restrictions")
	}
	if !numericRestrictionKinds[kind] {
		if _, supplied := body["value"]; supplied {
			return fmt.Errorf("restriction kind %q does not accept a numeric value", kind)
		}
	}
	if numericRestrictionKinds[kind] {
		n, ok := body["value"]
		if !ok {
			return fmt.Errorf("restriction kind %q requires --value (or --approvals/--builds)", kind)
		}
		number, ok := numericValue(n)
		if !ok || number < 0 {
			return fmt.Errorf("restriction kind %q requires a non-negative numeric value", kind)
		}
	}
	return nil
}
func nonEmptyRestrictionField(body map[string]any, key string) bool {
	value, ok := body[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	default:
		return strings.TrimSpace(fmt.Sprint(value)) != ""
	}
}
func hasRestrictionEntries(value any) bool {
	if list, ok := value.([]any); ok {
		return len(list) > 0
	}
	if list, ok := value.([]map[string]any); ok {
		return len(list) > 0
	}
	return value != nil
}
func validateNonEmpty(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", label)
	}
	return nil
}
func numericValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), n == float64(int(n))
	case string:
		i, e := strconv.Atoi(n)
		return i, e == nil
	}
	return 0, false
}

func loadPolicyFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}
	var value any
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		if err := yaml.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("parse policy YAML: %w", err)
		}
	} else if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("parse policy JSON: %w", err)
	}
	normalized := normalizeYAML(value)
	body, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("policy file must contain an object")
	}
	return body, nil
}
func normalizeYAML(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			out[k] = normalizeYAML(x)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, x := range v {
			out[fmt.Sprint(k)] = normalizeYAML(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = normalizeYAML(x)
		}
		return out
	default:
		return value
	}
}
func filterRestrictionsByBranchType(values []json.RawMessage, branchType string) []json.RawMessage {
	out := []json.RawMessage{}
	for _, raw := range values {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil && strings.EqualFold(fmt.Sprint(m["branch_type"]), branchType) {
			out = append(out, raw)
		}
	}
	return out
}
func exportRestrictions(path string, values []json.RawMessage) error {
	list := make([]any, 0, len(values))
	for _, raw := range values {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			list = append(list, sanitizeExport(v))
		}
	}
	var data []byte
	var err error
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		data, err = yaml.Marshal(list)
	} else {
		data, err = json.MarshalIndent(list, "", "  ")
		data = append(data, '\n')
	}
	if err != nil {
		return fmt.Errorf("encode export: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write export: %w", err)
	}
	return nil
}
func sanitizeExport(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			lower := strings.ToLower(k)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || lower == "authorization" {
				continue
			}
			out[k] = sanitizeExport(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = sanitizeExport(x)
		}
		return out
	default:
		return value
	}
}
