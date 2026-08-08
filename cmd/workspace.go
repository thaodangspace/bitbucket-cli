package cmd

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
)

func workspaceSelector(cfg config.Config, value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return workspaceFor(cfg, "")
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || (!strings.EqualFold(u.Hostname(), "bitbucket.org") && !strings.EqualFold(u.Hostname(), "api.bitbucket.org")) {
			return "", fmt.Errorf("invalid workspace selector %q", value)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "2.0" && parts[1] == "workspaces" {
			raw = parts[2]
		} else if len(parts) == 1 && parts[0] != "" {
			raw = parts[0]
		} else {
			return "", fmt.Errorf("invalid workspace selector %q", value)
		}
	}
	if strings.ContainsAny(raw, "/?#") {
		return "", fmt.Errorf("invalid workspace selector %q", value)
	}
	return raw, nil
}

func workspacePath(workspace string) string {
	return "/workspaces/" + bitbucket.EncodePathSegment(workspace)
}

func workspaceURL(workspace string) string {
	return "https://bitbucket.org/" + url.PathEscape(workspace)
}

// sanitizePrivateFields prevents a default API response from accidentally
// exposing private email fields. An explicit --json email selection opts in to
// that field, subject to the server having authorized the caller to see it.
func sanitizePrivateJSON(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	sanitizePrivateValue(value, explicitlySelectedEmail())
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func explicitlySelectedEmail() bool {
	for _, field := range strings.Split(flagJSON, ",") {
		field = strings.TrimSpace(field)
		if field == "email" || strings.HasSuffix(field, ".email") {
			return true
		}
	}
	return false
}

func sanitizePrivateValue(value any, allowEmail bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			lower := strings.ToLower(key)
			if !allowEmail && (lower == "email" || lower == "email_address") {
				delete(v, key)
				continue
			}
			sanitizePrivateValue(child, allowEmail)
		}
	case []any:
		for _, child := range v {
			sanitizePrivateValue(child, allowEmail)
		}
	}
}

func sanitizedValues(values []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(values))
	for i, raw := range values {
		out[i] = sanitizePrivateJSON(raw)
	}
	return out
}

func init() {
	workspaceCmd := &cobra.Command{Use: "workspace", Short: "Workspace and membership commands"}

	var listRole, listQuery string
	var listLimit int
	listCmd := &cobra.Command{
		Use: "list", Short: "List workspaces", Long: capabilityHelp("workspace.read"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listRole != "" && !contains([]string{"member", "collaborator", "owner"}, strings.ToLower(listRole)) {
				return fail(fmt.Errorf("invalid --role %q (use member, collaborator, or owner)", listRole))
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			if err := requireCapability(cfg, "workspace.read"); err != nil {
				return fail(err)
			}
			q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
			if listRole != "" {
				q.Set("role", strings.ToLower(listRole))
			}
			if listQuery != "" {
				q.Set("q", listQuery)
			}
			values, err := client.Paginate(ctx(cmd), "/workspaces?"+q.Encode(), listLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			return emitListFields(sanitizedValues(values), output.WorkspaceFields, output.WorkspaceSummary, "No workspaces found.")
		},
	}
	listCmd.Flags().StringVar(&listRole, "role", "", "Workspace role: member, collaborator, or owner")
	listCmd.Flags().StringVar(&listQuery, "query", "", "Bitbucket q expression")
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum workspaces to return")

	var viewMembers, viewProjects, viewWeb bool
	viewCmd := &cobra.Command{
		Use: "view [<workspace>]", Short: "View a workspace", Long: capabilityHelp("workspace.read"), Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			if err := requireCapability(cfg, "workspace.read"); err != nil {
				return fail(err)
			}
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			workspace, err := workspaceSelector(cfg, selector)
			if err != nil {
				return fail(err)
			}
			path := workspacePath(workspace)
			var value map[string]any
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &value); err != nil {
				return fail(err)
			}
			if viewMembers {
				members, e := client.Paginate(ctx(cmd), path+"/members?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), 0, bitbucket.DefaultMaxPages)
				if e != nil {
					return fail(e)
				}
				items := make([]any, 0, len(members))
				for _, raw := range sanitizedValues(members) {
					var item any
					_ = json.Unmarshal(raw, &item)
					items = append(items, item)
				}
				value["members"] = items
			}
			if viewProjects {
				projects, e := client.Paginate(ctx(cmd), path+"/projects?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), 0, bitbucket.DefaultMaxPages)
				if e != nil {
					return fail(e)
				}
				items := make([]any, 0, len(projects))
				for _, raw := range sanitizedValues(projects) {
					var item any
					_ = json.Unmarshal(raw, &item)
					items = append(items, item)
				}
				value["projects"] = items
			}
			if viewWeb {
				if err := openWeb(workspaceURL(workspace)); err != nil {
					return err
				}
			}
			raw, _ := json.Marshal(value)
			return emitObjectFields(sanitizePrivateJSON(raw), output.WorkspaceFields, output.WorkspaceSummary)
		},
	}
	viewCmd.Flags().BoolVar(&viewMembers, "members", false, "Include workspace members")
	viewCmd.Flags().BoolVar(&viewProjects, "projects", false, "Include workspace projects")
	viewCmd.Flags().BoolVar(&viewWeb, "web", false, "Open the workspace in a browser")

	var membersQuery string
	var membersLimit int
	membersCmd := &cobra.Command{
		Use: "members [<workspace>]", Short: "List workspace members", Long: capabilityHelp("workspace.read"), Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			if err := requireCapability(cfg, "workspace.read"); err != nil {
				return fail(err)
			}
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			workspace, err := workspaceSelector(cfg, selector)
			if err != nil {
				return fail(err)
			}
			q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
			if membersQuery != "" {
				q.Set("q", membersQuery)
			}
			values, err := client.Paginate(ctx(cmd), workspacePath(workspace)+"/members?"+q.Encode(), membersLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			return emitListFields(sanitizedValues(values), output.WorkspaceMemberFields, output.WorkspaceMemberSummary, "No workspace members found.")
		},
	}
	membersCmd.Flags().StringVar(&membersQuery, "query", "", "Bitbucket q expression")
	membersCmd.Flags().IntVar(&membersLimit, "limit", bitbucket.DefaultLimit, "Maximum members to return")

	memberViewCmd := &cobra.Command{
		Use: "view <user-selector>", Short: "View a workspace member", Long: capabilityHelp("workspace.read"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			if err := requireCapability(cfg, "workspace.read"); err != nil {
				return fail(err)
			}
			workspace, err := workspaceSelector(cfg, flagWorkspace)
			if err != nil {
				return fail(err)
			}
			account, err := resolveAccountSelector(ctx(cmd), client, workspace, args[0])
			if err != nil {
				return fail(err)
			}
			id, err := accountSelector(account)
			if err != nil {
				return fail(err)
			}
			var value map[string]any
			if err := client.Request(ctx(cmd), workspacePath(workspace)+"/members/"+bitbucket.EncodePathSegment(id), bitbucket.RequestOptions{}, &value); err != nil {
				return fail(err)
			}
			raw, _ := json.Marshal(value)
			return emitObjectFields(sanitizePrivateJSON(raw), output.WorkspaceMemberFields, output.WorkspaceMemberSummary)
		},
	}

	var inviteGroup, invitePermission string
	inviteCmd := &cobra.Command{Use: "invite <email>", Short: "Invite a workspace member", Long: capabilityHelp("workspace.invite"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		parsed, err := mail.ParseAddress(strings.TrimSpace(args[0]))
		if err != nil || parsed.Address != strings.TrimSpace(args[0]) {
			return fail(fmt.Errorf("invalid invitation email %q", args[0]))
		}
		if invitePermission != "" {
			if err := validatePermission(invitePermission); err != nil {
				return fail(err)
			}
		}
		cfg, err := loadConfig()
		if err != nil {
			return fail(err)
		}
		return fail(requireCapability(cfg, "workspace.invite"))
	}}
	inviteCmd.Flags().StringVar(&inviteGroup, "group", "", "Workspace group slug (unsupported until invitations are exposed by the API)")
	inviteCmd.Flags().StringVar(&invitePermission, "permission", "", "Workspace permission: read, write, or admin")
	removeCmd := &cobra.Command{Use: "remove-member <user-selector> --yes", Short: "Remove a workspace member", Long: capabilityHelp("workspace.remove-member"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fail(err)
		}
		return fail(requireCapability(cfg, "workspace.remove-member"))
	}}
	var yes bool
	removeCmd.Flags().BoolVar(&yes, "yes", false, "Confirm member removal")

	memberCmd := &cobra.Command{Use: "member", Short: "Workspace member commands"}
	memberCmd.AddCommand(memberViewCmd)
	workspaceCmd.AddCommand(listCmd, viewCmd, membersCmd, memberCmd, inviteCmd, removeCmd)
	rootCmd.AddCommand(workspaceCmd)
}
