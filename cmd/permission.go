package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
)

var validPermissionLevels = []string{"read", "write", "admin"}

func validatePermission(value string) error {
	if !contains(validPermissionLevels, strings.ToLower(strings.TrimSpace(value))) {
		return fmt.Errorf("invalid permission %q (use read, write, or admin)", value)
	}
	return nil
}

func permissionPath(ref config.ResolvedRepoRef, kind, selector string) string {
	return repoPath(ref) + "/permissions-config/" + kind + "/" + bitbucket.EncodePathSegment(selector)
}

func permissionSelectorQuery(account map[string]any) (string, error) {
	for _, pair := range [][2]string{{"uuid", "user.uuid"}, {"account_id", "user.account_id"}, {"nickname", "user.nickname"}} {
		if value, ok := account[pair[0]].(string); ok && strings.TrimSpace(value) != "" {
			return pair[1] + `="` + strings.ReplaceAll(value, `"`, `\"`) + `"`, nil
		}
	}
	return "", fmt.Errorf("resolved user did not contain a supported selector")
}

func permissionValue(value map[string]any) (normalized, raw string) {
	raw, _ = value["permission"].(string)
	normalized = strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		normalized = "none"
	}
	return normalized, raw
}

func permissionNotFound(err error) bool {
	var he *bitbucket.HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

func currentPermission(ctx context.Context, client *bitbucket.Client, path string) (map[string]any, bool, error) {
	var value map[string]any
	if err := client.Request(ctx, path, bitbucket.RequestOptions{}, &value); err != nil {
		if permissionNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return value, true, nil
}

// auditPermission creates the stable before/after result used by permission
// mutations. before_raw and after_raw retain server values such as "none".
func auditPermission(operation string, ref config.ResolvedRepoRef, principal any, before, beforeRaw, after, afterRaw string, changed bool) error {
	value := map[string]any{
		"operation":  operation,
		"target":     ref.Workspace + "/" + ref.RepoSlug,
		"principal":  principal,
		"before":     before,
		"after":      after,
		"before_raw": beforeRaw,
		"after_raw":  afterRaw,
		"changed":    changed,
	}
	raw, _ := json.Marshal(value)
	return emitObjectFields(sanitizePrivateJSON(raw), output.PermissionFields, output.PermissionSummary)
}

func init() {
	permissionCmd := &cobra.Command{Use: "permission", Short: "Repository permission commands"}

	var reposWorkspace, reposQuery string
	var reposLimit int
	reposCmd := &cobra.Command{Use: "repos --user <user-selector>", Short: "List repositories accessible to a user", Long: capabilityHelp("permission.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(reposUser) == "" {
			return fail(fmt.Errorf("--user is required"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "permission.read"); err != nil {
			return fail(err)
		}
		workspace, err := workspaceSelector(cfg, firstNonEmptyCLI(reposWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		account, err := resolveAccountSelector(ctx(cmd), client, workspace, reposUser)
		if err != nil {
			return fail(err)
		}
		query, err := permissionSelectorQuery(account)
		if err != nil {
			return fail(err)
		}
		q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}, "q": {query}}
		if reposQuery != "" {
			q.Set("q", "("+query+") AND ("+reposQuery+")")
		}
		values, err := client.Paginate(ctx(cmd), workspacePath(workspace)+"/permissions/repositories?"+q.Encode(), reposLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(sanitizedValues(values), output.PermissionFields, output.PermissionSummary, "No repository permissions found.")
	}}
	reposCmd.Flags().StringVar(&reposUser, "user", "", "User selector (required)")
	reposCmd.Flags().StringVar(&reposWorkspace, "workspace", "", "Workspace slug or UUID")
	reposCmd.Flags().StringVar(&reposQuery, "query", "", "Additional Bitbucket q expression")
	reposCmd.Flags().IntVar(&reposLimit, "limit", bitbucket.DefaultLimit, "Maximum permissions to return")

	var usersLimit int
	usersCmd := &cobra.Command{Use: "users", Short: "List explicit repository user permissions", Long: capabilityHelp("permission.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "permission.read"); err != nil {
			return fail(err)
		}
		ref, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		_ = ref
		values, err := client.Paginate(ctx(cmd), base+"/permissions-config/users?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), usersLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(sanitizedValues(values), output.PermissionFields, output.PermissionSummary, "No explicit user permissions found.")
	}}
	usersCmd.Flags().IntVar(&usersLimit, "limit", bitbucket.DefaultLimit, "Maximum permissions to return")

	var groupsLimit int
	groupsCmd := &cobra.Command{Use: "groups", Short: "List explicit repository group permissions", Long: capabilityHelp("permission.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "permission.read"); err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		values, err := client.Paginate(ctx(cmd), base+"/permissions-config/groups?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), groupsLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(sanitizedValues(values), output.PermissionFields, output.PermissionSummary, "No explicit group permissions found.")
	}}
	groupsCmd.Flags().IntVar(&groupsLimit, "limit", bitbucket.DefaultLimit, "Maximum permissions to return")

	grantCmd := permissionMutationCommand("grant", "permission.grant")
	revokeCmd := permissionMutationCommand("revoke", "permission.revoke")
	permissionCmd.AddCommand(reposCmd, usersCmd, groupsCmd, grantCmd, revokeCmd)
	rootCmd.AddCommand(permissionCmd)
}

var reposUser string

func permissionMutationCommand(operation, capability string) *cobra.Command {
	var user, group, level string
	var yes bool
	cmd := &cobra.Command{Use: operation + " --repository <workspace/repo> (--user <selector>|--group <slug>)", Short: strings.Title(operation) + " an explicit repository permission", Long: strings.Title(operation) + " an explicit repository permission. This is a write operation; run only when explicitly requested. " + capabilityHelp(capability), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if (strings.TrimSpace(user) == "") == (strings.TrimSpace(group) == "") {
			return fail(fmt.Errorf("provide exactly one of --user or --group"))
		}
		if operation == "revoke" && !yes {
			return fail(fmt.Errorf("refusing to revoke a permission without --yes"))
		}
		if operation == "grant" {
			if err := validatePermission(level); err != nil {
				return fail(err)
			}
			level = strings.ToLower(strings.TrimSpace(level))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, capability); err != nil {
			return fail(err)
		}
		ref, _, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		kind, selector := "users", user
		var principal any
		if group != "" {
			kind, selector = "groups", group
			principal = map[string]any{"type": "group", "slug": group}
		} else {
			account, e := resolveAccountSelector(ctx(cmd), client, ref.Workspace, user)
			if e != nil {
				return fail(e)
			}
			resolved, e := accountSelector(account)
			if e != nil {
				return fail(e)
			}
			selector = resolved
			principal = account
		}
		path := permissionPath(ref, kind, selector)
		current, exists, err := currentPermission(ctx(cmd), client, path)
		if err != nil {
			return fail(err)
		}
		before, beforeRaw := "none", "none"
		if exists {
			before, beforeRaw = permissionValue(current)
		}
		if operation == "grant" && before == level {
			return auditPermission("permission.grant", ref, principal, before, beforeRaw, before, beforeRaw, false)
		}
		if operation == "revoke" && (!exists || before == "none") {
			return auditPermission("permission.revoke", ref, principal, before, beforeRaw, "none", beforeRaw, false)
		}
		if operation == "grant" {
			var response map[string]any
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPut, Body: map[string]any{"permission": level}}, &response); err != nil {
				return fail(err)
			}
			after, afterRaw := permissionValue(response)
			if after == "none" {
				after, afterRaw = level, level
			}
			return auditPermission("permission.grant", ref, principal, before, beforeRaw, after, afterRaw, true)
		}
		if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return auditPermission("permission.revoke", ref, principal, before, beforeRaw, "none", "none", true)
	}}
	cmd.Flags().StringVar(&user, "user", "", "User selector")
	cmd.Flags().StringVar(&group, "group", "", "Group slug")
	if operation == "grant" {
		cmd.Flags().StringVar(&level, "permission", "", "Permission level: read, write, or admin")
	} else {
		cmd.Flags().BoolVar(&yes, "yes", false, "Confirm revocation")
	}
	return cmd
}
