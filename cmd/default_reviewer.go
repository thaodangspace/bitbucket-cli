package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
)

var uuidPattern = regexp.MustCompile(`^\{?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}\}?$`)
var accountIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{20,32}$`)

func init() {
	cmd := &cobra.Command{Use: "default-reviewer", Short: "Default reviewer commands"}
	listCmd := &cobra.Command{Use: "list", Short: "List default reviewers", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		values, err := client.Paginate(ctx(c), base+"/default-reviewers?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), 0, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(values, output.AccountFields, output.AccountSummary, "No default reviewers found.")
	}}
	addCmd := &cobra.Command{Use: "add <user-selector>", Short: "Add a default reviewer", Long: "Add a default reviewer. This is a write operation; run it only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		ref, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		account, err := resolveAccountSelector(ctx(c), client, ref.Workspace, args[0])
		if err != nil {
			return fail(err)
		}
		id, err := accountSelector(account)
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(c), base+"/default-reviewers/"+bitbucket.EncodePathSegment(id), bitbucket.RequestOptions{Method: http.MethodPut}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(raw, output.AccountFields, output.AccountSummary)
	}}
	var deleteYes bool
	removeCmd := &cobra.Command{Use: "remove <user-selector> --yes", Short: "Remove a default reviewer", Long: "Remove a default reviewer. This is a destructive write operation and requires --yes.", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		if !deleteYes {
			return fail(fmt.Errorf("refusing to remove default reviewer without --yes"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		ref, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		account, err := resolveAccountSelector(ctx(c), client, ref.Workspace, args[0])
		if err != nil {
			return fail(err)
		}
		id, err := accountSelector(account)
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(c), base+"/default-reviewers/"+bitbucket.EncodePathSegment(id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return emitObject(json.RawMessage(fmt.Sprintf(`{"removed":%q}`, args[0])), nil)
	}}
	removeCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm removal")
	cmd.AddCommand(listCmd, addCmd, removeCmd)
	rootCmd.AddCommand(cmd)
}

func resolveAccountSelector(ctx context.Context, client *bitbucket.Client, workspace, selector string) (map[string]any, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, fmt.Errorf("user selector must not be empty")
	}
	if uuidPattern.MatchString(selector) {
		return map[string]any{"uuid": selector}, nil
	}
	if accountIDPattern.MatchString(selector) || strings.Contains(selector, ":") {
		return map[string]any{"account_id": selector}, nil
	}
	// A nickname is resolved through the users endpoint. This avoids treating
	// a display name as a nickname and silently selecting the wrong account.
	var account map[string]any
	err := client.Request(ctx, "/users/"+bitbucket.EncodePathSegment(selector), bitbucket.RequestOptions{}, &account)
	if err == nil {
		return account, nil
	}
	q := url.Values{"q": {fmt.Sprintf(`user.display_name="%s"`, strings.ReplaceAll(selector, `"`, `\"`))}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
	values, queryErr := client.Paginate(ctx, "/workspaces/"+bitbucket.EncodePathSegment(workspace)+"/members?"+q.Encode(), 0, bitbucket.DefaultMaxPages)
	if queryErr != nil {
		return nil, fmt.Errorf("resolve user %q: %w", selector, queryErr)
	}
	matches := []map[string]any{}
	for _, raw := range values {
		var membership map[string]any
		if json.Unmarshal(raw, &membership) != nil {
			continue
		}
		account := membership
		if nested, ok := membership["user"].(map[string]any); ok {
			account = nested
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(account["display_name"])), selector) {
			matches = append(matches, account)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no user uniquely matches %q", selector)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("user display name %q is ambiguous (%d matches); use an account UUID, account ID, or nickname", selector, len(matches))
	}
	return matches[0], nil
}

func accountSelector(account map[string]any) (string, error) {
	for _, key := range []string{"uuid", "account_id", "nickname", "username"} {
		if value := strings.TrimSpace(fmt.Sprint(account[key])); value != "" && value != "<nil>" {
			return value, nil
		}
	}
	return "", fmt.Errorf("resolved user did not contain an account UUID, account ID, or nickname")
}

func resolveRestrictionUsers(ctx context.Context, cfg config.Config, client *bitbucket.Client, selectors []string) ([]any, error) {
	out := make([]any, 0, len(selectors))
	ref, _, err := resolveRepo(cfg)
	if err != nil {
		return nil, err
	}
	for _, selector := range selectors {
		account, err := resolveAccountSelector(ctx, client, ref.Workspace, selector)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, nil
}
func resolveRestrictionGroups(ctx context.Context, cfg config.Config, client *bitbucket.Client, selectors []string) ([]any, error) {
	ref, _, err := resolveRepo(cfg)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(selectors))
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			return nil, fmt.Errorf("group selector must not be empty")
		}
		q := url.Values{"q": {fmt.Sprintf(`name="%s"`, strings.ReplaceAll(selector, `"`, `\"`))}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
		values, queryErr := client.Paginate(ctx, "/workspaces/"+bitbucket.EncodePathSegment(ref.Workspace)+"/permissions-config/groups?"+q.Encode(), 0, bitbucket.DefaultMaxPages)
		if queryErr != nil {
			return nil, fmt.Errorf("resolve group %q: %w", selector, queryErr)
		}
		matches := []map[string]any{}
		for _, raw := range values {
			var value map[string]any
			if json.Unmarshal(raw, &value) == nil && strings.EqualFold(strings.TrimSpace(fmt.Sprint(value["name"])), selector) {
				matches = append(matches, value)
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no group uniquely matches %q", selector)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("group name %q is ambiguous; use an exact unique group selector", selector)
		}
		out = append(out, matches[0])
	}
	return out, nil
}
