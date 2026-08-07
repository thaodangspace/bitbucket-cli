package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
)

func init() {
	branchCmd := &cobra.Command{Use: "branch", Short: "Branch commands"}
	var query, sortBy string
	var limit int
	listCmd := &cobra.Command{
		Use: "list", Short: "List branches in a repository", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			_, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}
			q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
			if query != "" {
				q.Set("q", query)
			}
			if sortBy != "" {
				q.Set("sort", sortBy)
			}
			values, err := client.Paginate(ctx(cmd), base+"/refs/branches?"+q.Encode(), limit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			return emitListFields(values, output.BranchFields, output.BranchSummary, "No branches found.")
		},
	}
	listCmd.Flags().StringVar(&query, "query", "", `Bitbucket q expression, e.g. name ~ "feature/"`)
	listCmd.Flags().StringVar(&sortBy, "sort", "", "Sort field, optionally prefixed with -")
	listCmd.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum branches to return")

	viewCmd := refViewCommand("branch", "branches")
	var createTarget string
	createCmd := &cobra.Command{
		Use: "create <name> --target <commit|branch|tag>", Short: "Create a branch",
		Long: "Create a branch. This is a write operation; run it only when explicitly requested.", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateRefName("branch", name); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(createTarget) == "" {
				return fail(fmt.Errorf("--target is required"))
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			_, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}
			path := refPath(base, "branches", name)
			if err := ensureRefAbsent(ctx(cmd), client, path, "branch", name); err != nil {
				return fail(err)
			}
			resolved, err := resolveRefTarget(ctx(cmd), client, base, createTarget)
			if err != nil {
				return fail(err)
			}
			var raw json.RawMessage
			body := map[string]any{"name": name, "target": map[string]string{"hash": resolved}}
			if err := client.Request(ctx(cmd), base+"/refs/branches", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
				return fail(err)
			}
			return emitObjectFields(addTargetMetadata(raw, createTarget, resolved), output.BranchFields, output.BranchSummary)
		},
	}
	createCmd.Flags().StringVar(&createTarget, "target", "", "Commit hash, branch, or tag to point at")

	var deleteYes bool
	deleteCmd := &cobra.Command{
		Use: "delete <name> --yes", Short: "Delete a branch",
		Long: "Delete a branch. This is a destructive write operation and requires --yes.", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateRefName("branch", name); err != nil {
				return fail(err)
			}
			if !deleteYes {
				return fail(fmt.Errorf("refusing to delete branch %q without --yes", name))
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}
			if name == "main" {
				return fail(fmt.Errorf("refusing to delete the main branch %q", name))
			}
			var repo struct {
				MainBranch struct {
					Name string `json:"name"`
				} `json:"mainbranch"`
			}
			if err := client.Request(ctx(cmd), base, bitbucket.RequestOptions{}, &repo); err != nil && !isNotFound(err) {
				return fail(err)
			}
			if repo.MainBranch.Name != "" && repo.MainBranch.Name == name {
				return fail(fmt.Errorf("refusing to delete repository main branch %q", name))
			}
			if err := client.Request(ctx(cmd), refPath(base, "branches", name), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
				return fail(err)
			}
			return emitObject(json.RawMessage(fmt.Sprintf(`{"deleted":%q,"repository":%q}`, name, ref.Workspace+"/"+ref.RepoSlug)), nil)
		},
	}
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")

	branchCmd.AddCommand(listCmd, viewCmd, createCmd, deleteCmd)
	rootCmd.AddCommand(branchCmd)
}

func refViewCommand(kind, apiKind string) *cobra.Command {
	return &cobra.Command{
		Use: "view <name>", Aliases: []string{"get"}, Short: "View a " + kind,
		Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRefName(kind, args[0]); err != nil {
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
			if err := client.Request(ctx(cmd), refPath(base, apiKind, args[0]), bitbucket.RequestOptions{}, &raw); err != nil {
				return fail(err)
			}
			if kind == "tag" {
				return emitObjectFields(raw, output.TagFields, output.TagSummary)
			}
			return emitObjectFields(raw, output.BranchFields, output.BranchSummary)
		},
	}
}
