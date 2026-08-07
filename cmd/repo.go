package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/thaodangspace/bitbucket-cli/selector"
)

// repoObject is intentionally small: Bitbucket adds fields to repository
// responses frequently, while these fields are the ones needed by local
// workflows and by the mutation commands.
type repoObject struct {
	Workspace     string `json:"-"`
	Slug          string `json:"slug,omitempty"`
	Name          string `json:"name,omitempty"`
	FullName      string `json:"full_name,omitempty"`
	WorkspaceInfo struct {
		Slug string `json:"slug"`
	} `json:"workspace,omitempty"`
	Main struct {
		Name string `json:"name"`
	} `json:"mainbranch,omitempty"`
	Links struct {
		Clone []struct {
			Name string `json:"name"`
			Href string `json:"href"`
		} `json:"clone"`
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links,omitempty"`
}

func repoRefFromArgs(cfg config.Config, args []string) (config.ResolvedRepoRef, string, error) {
	if len(args) > 1 {
		return config.ResolvedRepoRef{}, "", fmt.Errorf("accepts at most one repository selector")
	}
	if len(args) == 1 {
		selected, err := parseRepositorySelector(args[0])
		if err != nil {
			return config.ResolvedRepoRef{}, "", err
		}
		return resolveRepoFor(cfg, &selector.Repository{Workspace: selected.Workspace, Repo: selected.RepoSlug})
	}
	return resolveRepo(cfg)
}

func repoPath(ref config.ResolvedRepoRef) string {
	return fmt.Sprintf("/repositories/%s/%s", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.RepoSlug))
}

func workspaceFor(cfg config.Config, value string) (string, error) {
	workspace := strings.TrimSpace(value)
	if workspace == "" {
		if remote, ok := config.GitRepoRefFrom(""); ok {
			workspace = remote.Workspace
		}
	}
	if workspace == "" {
		workspace = strings.TrimSpace(cfg.DefaultWorkspace)
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace is required; pass --workspace or set a default workspace")
	}
	if strings.ContainsAny(workspace, "/?#") {
		return "", fmt.Errorf("invalid workspace %q", workspace)
	}
	return workspace, nil
}

func renderRepo(raw json.RawMessage) error {
	return emitObjectFields(raw, output.RepoFields, output.RepoSummary)
}

func requestRepo(ctx context.Context, client *bitbucket.Client, ref config.ResolvedRepoRef) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := client.Request(ctx, repoPath(ref), bitbucket.RequestOptions{}, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func canonicalRepositoryRef(raw json.RawMessage) (config.ResolvedRepoRef, error) {
	var repo repoObject
	if err := json.Unmarshal(raw, &repo); err != nil {
		return config.ResolvedRepoRef{}, err
	}
	workspace, slug := strings.TrimSpace(repo.WorkspaceInfo.Slug), strings.TrimSpace(repo.Slug)
	if workspace == "" || slug == "" {
		parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(repo.FullName), ".git"), "/")
		if len(parts) == 2 {
			if workspace == "" {
				workspace = parts[0]
			}
			if slug == "" {
				slug = parts[1]
			}
		}
	}
	if (workspace == "" || slug == "") && repo.Links.Self.Href != "" {
		if parsed, err := selector.RepositorySelector(repo.Links.Self.Href); err == nil {
			workspace, slug = parsed.Workspace, parsed.Repo
		}
	}
	if workspace == "" || slug == "" {
		return config.ResolvedRepoRef{}, fmt.Errorf("Bitbucket fork response did not include a canonical workspace and repository slug")
	}
	return config.ResolvedRepoRef{Workspace: workspace, RepoSlug: slug}, nil
}

func repositorySourcePath(ref config.ResolvedRepoRef, branch, filePath string) string {
	parts := []string{repoPath(ref), "src", bitbucket.EncodePathSegment(branch)}
	for _, part := range strings.Split(strings.Trim(filePath, "/"), "/") {
		if part != "" {
			parts = append(parts, bitbucket.EncodePathSegment(part))
		}
	}
	return strings.Join(parts, "/")
}

func findReadmePath(values []json.RawMessage) (string, error) {
	var candidates []string
	for _, raw := range values {
		var entry struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &entry) != nil || entry.Path == "" || strings.Contains(strings.Trim(entry.Path, "/"), "/") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Path))
		if name == "readme" || name == "readme.md" || name == "readme.rst" || name == "readme.txt" || strings.HasPrefix(name, "readme.") {
			candidates = append(candidates, entry.Path)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("repository does not contain a README file at its root")
	}
	preference := map[string]int{"readme.md": 0, "readme.rst": 1, "readme.txt": 2, "readme": 3}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := strings.ToLower(candidates[i]), strings.ToLower(candidates[j])
		li, lok := preference[left]
		lj, jok := preference[right]
		if lok && jok && li != lj {
			return li < lj
		}
		if lok != jok {
			return lok
		}
		return left < right
	})
	return candidates[0], nil
}

func cloneURL(raw json.RawMessage, ref config.ResolvedRepoRef, protocol string) (string, error) {
	var repo repoObject
	if err := json.Unmarshal(raw, &repo); err != nil {
		return "", err
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol != "https" && protocol != "ssh" {
		return "", fmt.Errorf("invalid --protocol %q (use https or ssh)", protocol)
	}
	for _, link := range repo.Links.Clone {
		if strings.EqualFold(link.Name, protocol) && strings.TrimSpace(link.Href) != "" {
			return link.Href, nil
		}
	}
	if protocol == "ssh" {
		return fmt.Sprintf("git@bitbucket.org:%s/%s.git", ref.Workspace, ref.RepoSlug), nil
	}
	return fmt.Sprintf("https://bitbucket.org/%s/%s.git", url.PathEscape(ref.Workspace), url.PathEscape(ref.RepoSlug)), nil
}

var localDefaultReader = func() (string, bool) {
	out, err := exec.Command("git", "config", "--local", "--get", "bitbucket-cli.repository").Output()
	if err != nil {
		return "", false
	}
	value := strings.TrimSpace(string(out))
	return value, value != ""
}

func currentLocalDefault() (string, bool) { return localDefaultReader() }

func setLocalDefault(value string) error {
	cmd := exec.Command("git", "config", "--local", "bitbucket-cli.repository", value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set local repository default: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func init() {
	var listRole, listQuery, listSort, listProject string
	var listLimit int
	var listPrivate, listPublic, listFork, listSource bool

	repoCmd := &cobra.Command{Use: "repo", Short: "Repository commands"}

	listCmd := &cobra.Command{
		Use: "list [<workspace>]", Short: "List repositories in a workspace",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listRole != "" && !contains([]string{"owner", "member", "contributor"}, strings.ToLower(listRole)) {
				return fail(fmt.Errorf("invalid --role %q (use owner, member, or contributor)", listRole))
			}
			if listPrivate && listPublic {
				return fail(fmt.Errorf("--private and --public are mutually exclusive"))
			}
			if listFork && listSource {
				return fail(fmt.Errorf("--fork and --source are mutually exclusive"))
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			workspace := ""
			if len(args) == 1 {
				workspace = args[0]
			}
			workspace, err = workspaceFor(cfg, workspace)
			if err != nil {
				return fail(err)
			}
			q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
			if listRole != "" {
				q.Set("role", listRole)
			}
			if listQuery != "" {
				q.Set("q", listQuery)
			}
			if listSort != "" {
				q.Set("sort", listSort)
			}
			if listProject != "" {
				q.Set("q", combineRepoQuery(q.Get("q"), fmt.Sprintf("project.key=\"%s\"", listProject)))
			}
			if listPrivate {
				q.Set("q", combineRepoQuery(q.Get("q"), "is_private=true"))
			}
			if listPublic {
				q.Set("q", combineRepoQuery(q.Get("q"), "is_private=false"))
			}
			if listFork {
				q.Set("q", combineRepoQuery(q.Get("q"), "fork=true"))
			}
			if listSource {
				q.Set("q", combineRepoQuery(q.Get("q"), "fork=false"))
			}
			values, err := client.Paginate(ctx(cmd), fmt.Sprintf("/repositories/%s?%s", bitbucket.EncodePathSegment(workspace), q.Encode()), listLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			if err := emitListFields(values, output.RepoFields, output.RepoSummary, "No repositories found."); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&listRole, "role", "", "Repository role: owner, member, or contributor")
	listCmd.Flags().StringVar(&listQuery, "query", "", "Bitbucket q expression")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort field, optionally prefixed with -")
	listCmd.Flags().StringVar(&listProject, "project", "", "Filter by project key")
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum repositories to return")
	listCmd.Flags().BoolVar(&listPrivate, "private", false, "Only private repositories")
	listCmd.Flags().BoolVar(&listPublic, "public", false, "Only public repositories")
	listCmd.Flags().BoolVar(&listFork, "fork", false, "Only fork repositories")
	listCmd.Flags().BoolVar(&listSource, "source", false, "Only non-fork repositories")

	var viewReadme, viewWeb bool
	var viewBranch string
	viewCmd := &cobra.Command{
		Use: "view [<workspace/repo>]", Aliases: []string{"get"}, Short: "View a repository",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, _, err := repoRefFromArgs(cfg, args)
			if err != nil {
				return fail(err)
			}
			if viewWeb {
				return openWeb(buildRepositoryURL(ref.Workspace, ref.RepoSlug))
			}
			var raw json.RawMessage
			branch := viewBranch
			if !viewReadme || branch == "" {
				raw, err = requestRepo(ctx(cmd), client, ref)
				if err != nil {
					return fail(err)
				}
			}
			if !viewReadme {
				return renderRepo(raw)
			}
			if branch == "" {
				var repo repoObject
				if err := json.Unmarshal(raw, &repo); err != nil {
					return fail(err)
				}
				branch = repo.Main.Name
			}
			if branch == "" {
				branch = "main"
			}
			rootPath := fmt.Sprintf("%s/?pagelen=%d", repositorySourcePath(ref, branch, ""), bitbucket.DefaultPageLen)
			values, err := client.PaginateAll(ctx(cmd), rootPath, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			readmePath, err := findReadmePath(values)
			if err != nil {
				return fail(err)
			}
			resp, err := client.Do(ctx(cmd), repositorySourcePath(ref, branch, readmePath), bitbucket.RequestOptions{Headers: http.Header{"Accept": {"text/plain"}}})
			if err != nil {
				return fail(err)
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return fail(err)
			}
			return writeOutput(data)
		},
	}
	viewCmd.Flags().BoolVar(&viewReadme, "readme", false, "Print the repository README")
	viewCmd.Flags().StringVar(&viewBranch, "branch", "", "Branch or ref used for --readme")
	viewCmd.Flags().BoolVar(&viewWeb, "web", false, "Open the repository in a browser")

	var createDescription, createProject, createMainBranch, createSource, createProtocol, createDirectory, createRemoteName string
	var createPrivate, createClone bool
	createCmd := &cobra.Command{
		Use: "create <name>", Short: "Create a repository",
		Long: "Create a repository. This is a write operation; run it only when explicitly requested.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" || strings.ContainsAny(name, "/?#") {
				return fail(fmt.Errorf("invalid repository name %q", args[0]))
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			workspace, err := workspaceFor(cfg, flagWorkspace)
			if strings.TrimSpace(flagWorkspace) == "" {
				return fail(fmt.Errorf("--workspace is required when creating a repository"))
			}
			if err != nil {
				return fail(err)
			}
			body := map[string]any{"name": name, "is_private": createPrivate}
			if cmd.Flags().Changed("description") {
				body["description"] = createDescription
			}
			if createProject != "" {
				body["project"] = map[string]string{"key": createProject}
			}
			if createMainBranch != "" {
				body["mainbranch"] = map[string]string{"name": createMainBranch}
			}
			var raw json.RawMessage
			createPath := fmt.Sprintf("/repositories/%s/%s", bitbucket.EncodePathSegment(workspace), bitbucket.EncodePathSegment(name))
			if err := client.Request(ctx(cmd), createPath, bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
				return fail(err)
			}
			ref := config.ResolvedRepoRef{Workspace: workspace, RepoSlug: name}
			if raw == nil {
				raw = json.RawMessage(fmt.Sprintf(`{"workspace":{"slug":%q},"name":%q,"full_name":%q}`, workspace, name, workspace+"/"+name))
			}
			protocol := protocolFor(cfg, createProtocol)
			if createSource != "" {
				if err := pushLocalSource(ctx(cmd), createSource, ref, createRemoteName, protocol); err != nil {
					_ = emitObject(raw, output.RepoSummary)
					return fail(fmt.Errorf("repository created at %s/%s, but local source push failed: %w; remote was not deleted", workspace, name, err))
				}
			}
			if createClone {
				if err := cloneRepository(ctx(cmd), client, raw, ref, protocol, createDirectory, nil); err != nil {
					_ = emitObject(raw, output.RepoSummary)
					return fail(fmt.Errorf("repository created at %s/%s, but clone failed: %w", workspace, name, err))
				}
			}
			return emitObject(raw, output.RepoSummary)
		},
	}
	createCmd.Flags().BoolVar(&createPrivate, "private", false, "Make the repository private")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Repository description")
	createCmd.Flags().StringVar(&createProject, "project", "", "Project key")
	createCmd.Flags().StringVar(&createMainBranch, "main-branch", "", "Main branch name")
	createCmd.Flags().BoolVar(&createClone, "clone", false, "Clone after creation")
	createCmd.Flags().StringVar(&createSource, "source", "", "Existing local git repository to push after creation")
	createCmd.Flags().StringVar(&createProtocol, "protocol", "", "Clone protocol: https or ssh (defaults to config)")
	createCmd.Flags().StringVar(&createDirectory, "directory", "", "Directory for --clone")
	createCmd.Flags().StringVar(&createRemoteName, "remote-name", "", "Remote name used by --source")

	var editName, editDescription, editForkPolicy, editMainBranch string
	var editPrivate, editIssues, editWiki bool
	editCmd := &cobra.Command{
		Use: "edit [<workspace/repo>]", Short: "Edit a repository",
		Long: "Edit a repository. This is a write operation; only explicitly supplied fields are changed.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, _, err := repoRefFromArgs(cfg, args)
			if err != nil {
				return fail(err)
			}
			body := map[string]any{}
			if cmd.Flags().Changed("name") {
				if strings.TrimSpace(editName) == "" {
					return fail(fmt.Errorf("--name must not be empty"))
				}
				body["name"] = editName
			}
			if cmd.Flags().Changed("description") {
				body["description"] = editDescription
			}
			if cmd.Flags().Changed("private") {
				body["is_private"] = editPrivate
			}
			if cmd.Flags().Changed("has-issues") {
				body["has_issues"] = editIssues
			}
			if cmd.Flags().Changed("has-wiki") {
				body["has_wiki"] = editWiki
			}
			if cmd.Flags().Changed("fork-policy") {
				body["fork_policy"] = editForkPolicy
			}
			if cmd.Flags().Changed("main-branch") {
				body["mainbranch"] = map[string]string{"name": editMainBranch}
			}
			if len(body) == 0 {
				return fail(fmt.Errorf("at least one repository field must be supplied"))
			}
			// Bitbucket treats repository PUT as a full update. Read the current
			// mutable state first so an edit such as --description does not reset
			// visibility, issue/wiki settings, or the main branch.
			current, err := requestRepo(ctx(cmd), client, ref)
			if err != nil {
				return fail(err)
			}
			body = mergeRepositoryUpdate(current, body, ref)
			var raw json.RawMessage
			if err := client.Request(ctx(cmd), repoPath(ref), bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
				return fail(err)
			}
			return renderRepo(raw)
		},
	}
	editCmd.Flags().StringVar(&editName, "name", "", "New repository name")
	editCmd.Flags().StringVar(&editDescription, "description", "", "New repository description")
	editCmd.Flags().BoolVar(&editPrivate, "private", false, "Set private visibility")
	editCmd.Flags().BoolVar(&editIssues, "has-issues", false, "Enable or disable issue tracking")
	editCmd.Flags().BoolVar(&editWiki, "has-wiki", false, "Enable or disable wiki")
	editCmd.Flags().StringVar(&editForkPolicy, "fork-policy", "", "Fork policy: allow_forks, no_public_forks, or no_forks")
	editCmd.Flags().StringVar(&editMainBranch, "main-branch", "", "New main branch name")

	var deleteYes bool
	deleteCmd := &cobra.Command{
		Use: "delete [<workspace/repo>]", Short: "Delete a repository",
		Long: "Delete a repository. This is a destructive write operation; the exact repository must be selected and --yes is required in non-interactive use.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, _, err := repoRefFromArgs(cfg, args)
			if err != nil {
				return fail(err)
			}
			name := ref.Workspace + "/" + ref.RepoSlug
			if !deleteYes {
				return fail(fmt.Errorf("refusing to delete %s without --yes", name))
			}
			if _, err := fmt.Fprintf(os.Stderr, "Deleting %s\n", name); err != nil {
				return fail(err)
			}
			if err := client.Request(ctx(cmd), repoPath(ref), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
				return fail(err)
			}
			return emitObject(json.RawMessage(fmt.Sprintf(`{"deleted":%q}`, name)), output.RepoSummary)
		},
	}
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")

	var forkWorkspace, forkName, forkProtocol, forkDirectory, forkRemoteName string
	var forkClone, forkRemote, forkYes bool
	forkCmd := &cobra.Command{
		Use: "fork [<workspace/repo>]", Short: "Fork a repository",
		Long: "Fork a repository. This is a write operation; run it only when explicitly requested.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, _, err := repoRefFromArgs(cfg, args)
			if err != nil {
				return fail(err)
			}
			body := map[string]any{}
			if forkWorkspace != "" {
				body["workspace"] = map[string]string{"slug": forkWorkspace}
			}
			if forkName != "" {
				body["name"] = forkName
			}
			var raw json.RawMessage
			if err := client.Request(ctx(cmd), repoPath(ref)+"/forks", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
				return fail(err)
			}
			// The repository response is authoritative. Never derive a slug from
			// the display name: Bitbucket may slugify it differently and the
			// fork may live in a different workspace.
			forkRef, err := canonicalRepositoryRef(raw)
			if err != nil {
				return fail(err)
			}
			if forkClone {
				protocol := protocolFor(cfg, forkProtocol)
				if !hasCloneLink(raw, protocol) {
					// Fork creation can return before the repository object is
					// clone-ready. Poll briefly, but retain the URL fallback if
					// Bitbucket never exposes clone links.
					for attempt := 0; attempt < 5 && !hasCloneLink(raw, protocol); attempt++ {
						updated, pollErr := requestRepo(ctx(cmd), client, forkRef)
						if pollErr == nil {
							raw = updated
						}
						if hasCloneLink(raw, protocol) {
							break
						}
						select {
						case <-ctx(cmd).Done():
							return fail(ctx(cmd).Err())
						case <-time.After(100 * time.Millisecond):
						}
					}
				}
				if err := cloneRepository(ctx(cmd), client, raw, forkRef, protocol, forkDirectory, nil); err != nil {
					return fail(err)
				}
			}
			if forkRemote {
				if err := configureForkRemote(ctx(cmd), forkRef, forkRemoteName, forkYes); err != nil {
					return fail(err)
				}
			}
			return emitObject(raw, output.RepoSummary)
		},
	}
	forkCmd.Flags().StringVar(&forkWorkspace, "workspace", "", "Target workspace")
	forkCmd.Flags().StringVar(&forkName, "name", "", "Fork name")
	forkCmd.Flags().BoolVar(&forkClone, "clone", false, "Clone the fork after creation")
	forkCmd.Flags().BoolVar(&forkRemote, "remote", false, "Configure the fork as a git remote")
	forkCmd.Flags().StringVar(&forkRemoteName, "remote-name", "origin", "Remote name for the fork")
	forkCmd.Flags().BoolVar(&forkYes, "yes", false, "Confirm remote changes")
	forkCmd.Flags().StringVar(&forkProtocol, "protocol", "", "Clone protocol: https or ssh (defaults to config)")
	forkCmd.Flags().StringVar(&forkDirectory, "directory", "", "Directory for --clone")

	var cloneProtocol string
	cloneCmd := &cobra.Command{
		Use: "clone <workspace/repo|url> [<directory>] [-- <git-flags>...]", Short: "Clone a repository",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, err := parseRepositorySelector(args[0])
			if err != nil {
				return fail(err)
			}
			raw, err := requestRepo(ctx(cmd), client, config.ResolvedRepoRef{Workspace: ref.Workspace, RepoSlug: ref.RepoSlug})
			if err != nil {
				return fail(err)
			}
			gitArgs := []string{}
			directory := ""
			if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
				directory = args[1]
				gitArgs = args[2:]
			} else if len(args) > 1 {
				gitArgs = args[1:]
			}
			return cloneRepository(ctx(cmd), client, raw, config.ResolvedRepoRef{Workspace: ref.Workspace, RepoSlug: ref.RepoSlug}, protocolFor(cfg, cloneProtocol), directory, gitArgs)
		},
	}
	cloneCmd.Flags().StringVar(&cloneProtocol, "protocol", "", "Clone protocol: https or ssh (defaults to config)")

	var browseBranch string
	browseCmd := &cobra.Command{
		Use: "browse [<workspace/repo>] [<path>]", Short: "Open a repository path in a browser",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fail(err)
			}
			ref, path, err := browseRepoArgs(cfg, args)
			if err != nil {
				return fail(err)
			}
			target, err := buildBrowseURL(ref.Workspace, ref.RepoSlug, path, browseBranch)
			if err != nil {
				return fail(err)
			}
			return openWeb(target)
		},
	}
	browseCmd.Flags().StringVar(&browseBranch, "branch", "", "Branch or ref to browse")

	var defaultSelector string
	setDefaultCmd := &cobra.Command{
		Use: "set-default [<workspace/repo>]", Short: "Set the local repository default",
		Long: "Store a repository preference in the current git repository only; this is a local configuration write.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fail(err)
			}
			value := defaultSelector
			if len(args) == 1 {
				value = args[0]
			}
			var ref config.ResolvedRepoRef
			if value != "" {
				ref, err = parseRepositorySelector(value)
			} else {
				ref, _, err = resolveRepo(cfg)
			}
			if err != nil {
				return fail(err)
			}
			selectorValue := ref.Workspace + "/" + ref.RepoSlug
			if err := setLocalDefault(selectorValue); err != nil {
				return fail(err)
			}
			return emitObject(json.RawMessage(fmt.Sprintf(`{"repository":%q,"scope":"local"}`, selectorValue)), func(_ map[string]any) string { return "Default repository: " + selectorValue })
		},
	}
	setDefaultCmd.Flags().StringVar(&defaultSelector, "value", "", "Repository selector (normally use the positional argument)")

	repoCmd.AddCommand(listCmd, viewCmd, createCmd, editCmd, deleteCmd, forkCmd, cloneCmd, browseCmd, setDefaultCmd)
	rootCmd.AddCommand(repoCmd)
}

func hasCloneLink(raw json.RawMessage, protocol string) bool {
	var repo repoObject
	if json.Unmarshal(raw, &repo) != nil {
		return false
	}
	for _, link := range repo.Links.Clone {
		if strings.EqualFold(link.Name, protocol) && strings.TrimSpace(link.Href) != "" {
			return true
		}
	}
	return false
}

func mergeRepositoryUpdate(raw json.RawMessage, changed map[string]any, ref config.ResolvedRepoRef) map[string]any {
	payload := map[string]any{}
	var current map[string]any
	if json.Unmarshal(raw, &current) == nil {
		for _, key := range []string{"name", "description", "is_private", "has_issues", "has_wiki", "fork_policy", "mainbranch", "project", "scm"} {
			if value, ok := current[key]; ok {
				payload[key] = value
			}
		}
	}
	if _, ok := payload["name"]; !ok {
		payload["name"] = ref.RepoSlug
	}
	for key, value := range changed {
		payload[key] = value
	}
	return payload
}

func protocolFor(cfg config.Config, override string) string {
	protocol := strings.ToLower(strings.TrimSpace(override))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(cfg.CloneProtocol))
	}
	if protocol == "" {
		protocol = "https"
	}
	return protocol
}

func combineRepoQuery(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return "(" + existing + ") AND " + addition
}

func browseRepoArgs(cfg config.Config, args []string) (config.ResolvedRepoRef, string, error) {
	if len(args) == 0 {
		ref, _, err := resolveRepo(cfg)
		return ref, "", err
	}
	if len(args) == 1 {
		if ref, err := parseRepositorySelector(args[0]); err == nil {
			return ref, "", nil
		}
		ref, _, err := resolveRepo(cfg)
		return ref, args[0], err
	}
	ref, err := parseRepositorySelector(args[0])
	if err != nil {
		return config.ResolvedRepoRef{}, "", err
	}
	return ref, args[1], nil
}

var gitClone = func(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"clone"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

func cloneRepository(ctx context.Context, client *bitbucket.Client, raw json.RawMessage, ref config.ResolvedRepoRef, protocol, directory string, flags []string) error {
	_ = client
	remote, err := cloneURL(raw, ref, protocol)
	if err != nil {
		return err
	}
	args := []string{remote}
	if directory != "" {
		args = append(args, directory)
	}
	args = append(args, flags...)
	return gitClone(ctx, args...)
}

func pushLocalSource(ctx context.Context, source string, ref config.ResolvedRepoRef, remoteName, protocol string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("source path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path %q is not a directory", source)
	}
	if remoteName == "" {
		remoteName = "origin"
	}
	remoteList, remoteErr := exec.CommandContext(ctx, "git", "-C", source, "remote").Output()
	if remoteErr != nil {
		return fmt.Errorf("source is not a git repository: %w", remoteErr)
	}
	remotes := strings.Fields(string(remoteList))
	for _, name := range remotes {
		if name == remoteName {
			return fmt.Errorf("source already has a %q remote; pass a different --remote-name", remoteName)
		}
	}
	clone := fmt.Sprintf("https://bitbucket.org/%s/%s.git", url.PathEscape(ref.Workspace), url.PathEscape(ref.RepoSlug))
	if strings.EqualFold(protocol, "ssh") {
		clone = fmt.Sprintf("git@bitbucket.org:%s/%s.git", ref.Workspace, ref.RepoSlug)
	}
	if err := runGitIn(ctx, source, "remote", "add", remoteName, clone); err != nil {
		return err
	}
	branch := "main"
	if out, e := runGitInOutput(ctx, source, "symbolic-ref", "--short", "HEAD"); e == nil && strings.TrimSpace(out) != "" {
		branch = strings.TrimSpace(out)
	}
	if err := runGitIn(ctx, source, "push", remoteName, "HEAD:"+branch); err != nil {
		return err
	}
	return nil
}

func runGitInOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
func runGitIn(ctx context.Context, dir string, args ...string) error {
	_, err := runGitInOutput(ctx, dir, args...)
	return err
}

func configureForkRemote(ctx context.Context, ref config.ResolvedRepoRef, remoteName string, yes bool) error {
	if remoteName == "" {
		remoteName = "origin"
	}
	remotes, err := gitRunner.Remotes(ctx)
	if err != nil {
		return err
	}
	var hasOrigin, hasUpstream, hasTarget bool
	for _, remote := range remotes {
		hasOrigin = hasOrigin || remote.Name == "origin"
		hasUpstream = hasUpstream || remote.Name == "upstream"
		hasTarget = hasTarget || remote.Name == remoteName
	}
	if hasTarget && remoteName != "origin" {
		return fmt.Errorf("git remote %q already exists; refusing to overwrite it", remoteName)
	}
	if hasOrigin && hasUpstream {
		return fmt.Errorf("git remote upstream already exists; refusing to rename origin")
	}
	if !yes {
		return fmt.Errorf("refusing to change git remotes without --yes")
	}
	manager, ok := gitRunner.(gitRemoteManager)
	if !ok {
		return fmt.Errorf("git remote management is unavailable")
	}
	if hasOrigin {
		if renamer, ok := gitRunner.(interface {
			RenameRemote(context.Context, string, string) error
		}); ok {
			if err := renamer.RenameRemote(ctx, "origin", "upstream"); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("cannot rename origin to upstream")
		}
	}
	clone := fmt.Sprintf("https://bitbucket.org/%s/%s.git", url.PathEscape(ref.Workspace), url.PathEscape(ref.RepoSlug))
	return manager.AddRemote(ctx, remoteName, clone)
}
