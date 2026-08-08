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
	"github.com/thaodangspace/bitbucket-cli/output"
)

func projectSelector(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" || strings.ContainsAny(raw, "/?#") {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i := range parts {
				if parts[i] == "projects" && i+1 < len(parts) {
					raw = parts[i+1]
					break
				}
			}
		}
	}
	if raw == "" || strings.ContainsAny(raw, "/?#") {
		return "", fmt.Errorf("invalid project selector %q", value)
	}
	return raw, nil
}

func projectPath(workspace, project string) string {
	return workspacePath(workspace) + "/projects/" + bitbucket.EncodePathSegment(project)
}

func projectRaw(value map[string]any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return sanitizePrivateJSON(raw)
}

// fetchProject preserves the server's project-key casing. A case-insensitive
// fallback is used because project keys are case-insensitive for lookup while
// API URLs and output should retain the server spelling.
func fetchProject(ctx context.Context, client *bitbucket.Client, workspace, selector string) (map[string]any, string, error) {
	path := projectPath(workspace, selector)
	var value map[string]any
	if err := client.Request(ctx, path, bitbucket.RequestOptions{}, &value); err == nil {
		key := selector
		if serverKey, ok := value["key"].(string); ok && serverKey != "" {
			key = serverKey
		}
		return value, key, nil
	} else {
		var he *bitbucket.HTTPError
		if !errors.As(err, &he) || he.Status != http.StatusNotFound {
			return nil, "", err
		}
	}
	values, err := client.Paginate(ctx, workspacePath(workspace)+"/projects?pagelen="+fmt.Sprint(bitbucket.DefaultPageLen), 0, bitbucket.DefaultMaxPages)
	if err != nil {
		return nil, "", err
	}
	for _, raw := range values {
		var candidate map[string]any
		if json.Unmarshal(raw, &candidate) != nil {
			continue
		}
		for _, field := range []string{"key", "uuid"} {
			if candidateValue, ok := candidate[field].(string); ok && strings.EqualFold(candidateValue, selector) {
				canonical := candidateValue
				if key, ok := candidate["key"].(string); ok && key != "" {
					canonical = key
				}
				if err := client.Request(ctx, projectPath(workspace, canonical), bitbucket.RequestOptions{}, &candidate); err != nil {
					return nil, "", err
				}
				return candidate, canonical, nil
			}
		}
	}
	return nil, "", fmt.Errorf("project %q not found in workspace %q", selector, workspace)
}

func projectRepositories(ctx context.Context, client *bitbucket.Client, workspace, project string) ([]json.RawMessage, error) {
	q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
	q.Set("q", fmt.Sprintf(`project.key="%s"`, strings.ReplaceAll(project, `"`, `\"`)))
	return client.Paginate(ctx, "/repositories/"+bitbucket.EncodePathSegment(workspace)+"?"+q.Encode(), 0, bitbucket.DefaultMaxPages)
}

func projectRepoMetadataList(workspace, project string, values []json.RawMessage) map[string]any {
	items := make([]any, 0, len(values))
	for _, raw := range sanitizedValues(values) {
		var item any
		_ = json.Unmarshal(raw, &item)
		items = append(items, item)
	}
	return map[string]any{
		"metadata": map[string]any{"workspace": workspace, "project": project, "filter": "project"},
		"values":   items,
	}
}

func init() {
	projectCmd := &cobra.Command{Use: "project", Short: "Project commands"}

	var listWorkspace, listQuery string
	var listLimit int
	listCmd := &cobra.Command{Use: "list", Short: "List projects", Long: capabilityHelp("project.read"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "project.read"); err != nil {
			return fail(err)
		}
		workspace, err := workspaceSelector(cfg, firstNonEmptyCLI(listWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
		if listQuery != "" {
			q.Set("q", listQuery)
		}
		values, err := client.Paginate(ctx(cmd), workspacePath(workspace)+"/projects?"+q.Encode(), listLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(sanitizedValues(values), output.ProjectFields, output.ProjectSummary, "No projects found.")
	}}
	listCmd.Flags().StringVar(&listWorkspace, "workspace", "", "Workspace slug or UUID")
	listCmd.Flags().StringVar(&listQuery, "query", "", "Bitbucket q expression")
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum projects to return")

	var viewWorkspace string
	var viewRepos, viewWeb bool
	viewCmd := &cobra.Command{Use: "view <key-or-uuid>", Short: "View a project", Long: capabilityHelp("project.read"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		key, err := projectSelector(args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "project.read"); err != nil {
			return fail(err)
		}
		workspace, err := workspaceSelector(cfg, firstNonEmptyCLI(viewWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		value, serverKey, err := fetchProject(ctx(cmd), client, workspace, key)
		if err != nil {
			return fail(err)
		}
		if viewRepos {
			values, e := projectRepositories(ctx(cmd), client, workspace, serverKey)
			if e != nil {
				return fail(e)
			}
			value["repositories"] = projectRepoMetadataList(workspace, serverKey, values)
		}
		if viewWeb {
			if err := openWeb("https://bitbucket.org/" + url.PathEscape(workspace) + "/workspace/projects/" + url.PathEscape(serverKey)); err != nil {
				return err
			}
		}
		return emitObjectFields(projectRaw(value), output.ProjectFields, output.ProjectSummary)
	}}
	viewCmd.Flags().StringVar(&viewWorkspace, "workspace", "", "Workspace slug or UUID")
	viewCmd.Flags().BoolVar(&viewRepos, "repos", false, "Include repositories in the project")
	viewCmd.Flags().BoolVar(&viewWeb, "web", false, "Open the project in a browser")

	var createWorkspace, createKey, createName, createDescription string
	var createPrivate bool
	createCmd := &cobra.Command{Use: "create --key <key> --name <name>", Short: "Create a project", Long: "Create a project. This is a write operation; run only when explicitly requested. " + capabilityHelp("project.write"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(createKey) == "" || strings.TrimSpace(createName) == "" {
			return fail(fmt.Errorf("--key and --name are required"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "project.write"); err != nil {
			return fail(err)
		}
		workspace, err := workspaceSelector(cfg, firstNonEmptyCLI(createWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		body := map[string]any{"key": createKey, "name": createName}
		if createDescription != "" {
			body["description"] = createDescription
		}
		if cmd.Flags().Changed("private") {
			body["is_private"] = createPrivate
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), workspacePath(workspace)+"/projects", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(sanitizePrivateJSON(raw), output.ProjectFields, output.ProjectSummary)
	}}
	createCmd.Flags().StringVar(&createWorkspace, "workspace", "", "Workspace slug or UUID")
	createCmd.Flags().StringVar(&createKey, "key", "", "Project key (required)")
	createCmd.Flags().StringVar(&createName, "name", "", "Project name (required)")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Project description")
	createCmd.Flags().BoolVar(&createPrivate, "private", false, "Whether the project is private")

	var editWorkspace, editName, editDescription, editKey string
	var editPrivate bool
	editCmd := &cobra.Command{Use: "edit <key-or-uuid>", Short: "Edit a project", Long: "Edit a project. This is a write operation; only explicitly supplied fields are changed.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		key, err := projectSelector(args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "project.write"); err != nil {
			return fail(err)
		}
		workspace, err := workspaceSelector(cfg, firstNonEmptyCLI(editWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		current, canonicalKey, err := fetchProject(ctx(cmd), client, workspace, key)
		if err != nil {
			return fail(err)
		}
		path := projectPath(workspace, canonicalKey)
		body := map[string]any{}
		for _, field := range []string{"key", "name", "description", "is_private"} {
			if value, ok := current[field]; ok {
				body[field] = value
			}
		}
		if cmd.Flags().Changed("key") {
			body["key"] = editKey
		}
		if cmd.Flags().Changed("name") {
			body["name"] = editName
		}
		if cmd.Flags().Changed("description") {
			body["description"] = editDescription
		}
		if cmd.Flags().Changed("private") {
			body["is_private"] = editPrivate
		}
		if !cmd.Flags().Changed("key") && !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") && !cmd.Flags().Changed("private") {
			return fail(fmt.Errorf("at least one project field must be supplied"))
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(sanitizePrivateJSON(raw), output.ProjectFields, output.ProjectSummary)
	}}
	editCmd.Flags().StringVar(&editWorkspace, "workspace", "", "Workspace slug or UUID")
	editCmd.Flags().StringVar(&editKey, "key", "", "New project key")
	editCmd.Flags().StringVar(&editName, "name", "", "New project name")
	editCmd.Flags().StringVar(&editDescription, "description", "", "New project description")
	editCmd.Flags().BoolVar(&editPrivate, "private", false, "Whether the project is private")

	var deleteWorkspace string
	var deleteYes bool
	deleteCmd := &cobra.Command{Use: "delete <key-or-uuid> --yes", Short: "Delete a project", Long: "Delete a project. This is a destructive write operation and requires --yes in non-interactive use.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		key, err := projectSelector(args[0])
		if err != nil {
			return fail(err)
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		if err := requireCapability(cfg, "project.write"); err != nil {
			return fail(err)
		}
		workspace, err := workspaceSelector(cfg, firstNonEmptyCLI(deleteWorkspace, flagWorkspace))
		if err != nil {
			return fail(err)
		}
		if !deleteYes {
			return fail(fmt.Errorf("refusing to delete project %q without --yes", workspace+"/"+key))
		}
		_, canonicalKey, fetchErr := fetchProject(ctx(cmd), client, workspace, key)
		if fetchErr != nil {
			return fail(fetchErr)
		}
		path := projectPath(workspace, canonicalKey)
		repoValues, listErr := projectRepositories(ctx(cmd), client, workspace, canonicalKey)
		if listErr != nil {
			return fail(listErr)
		}
		if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		result := map[string]any{"deleted": true, "workspace": workspace, "project": canonicalKey, "repositories": len(repoValues)}
		raw, _ := json.Marshal(result)
		return emitObject(raw, nil)
	}}
	deleteCmd.Flags().StringVar(&deleteWorkspace, "workspace", "", "Workspace slug or UUID")
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")

	projectCmd.AddCommand(listCmd, viewCmd, createCmd, editCmd, deleteCmd)
	rootCmd.AddCommand(projectCmd)
}

func firstNonEmptyCLI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
