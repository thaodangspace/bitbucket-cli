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
	tagCmd := &cobra.Command{Use: "tag", Short: "Tag commands"}
	var query, sortBy string
	var limit int
	listCmd := &cobra.Command{Use: "list", Short: "List tags in a repository", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		values, err := client.Paginate(ctx(cmd), base+"/refs/tags?"+q.Encode(), limit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitListFields(values, output.TagFields, output.TagSummary, "No tags found.")
	}}
	listCmd.Flags().StringVar(&query, "query", "", "Bitbucket q expression")
	listCmd.Flags().StringVar(&sortBy, "sort", "", "Sort field, optionally prefixed with -")
	listCmd.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum tags to return")

	viewCmd := refViewCommand("tag", "tags")
	var target, message string
	createCmd := &cobra.Command{Use: "create <name> --target <commit|branch>", Short: "Create a tag", Long: "Create a tag. This is a write operation; run it only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := validateRefName("tag", name); err != nil {
			return fail(err)
		}
		if strings.TrimSpace(target) == "" {
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
		path := refPath(base, "tags", name)
		if err := ensureRefAbsent(ctx(cmd), client, path, "tag", name); err != nil {
			return fail(err)
		}
		resolved, err := resolveRefTarget(ctx(cmd), client, base, target)
		if err != nil {
			return fail(err)
		}
		body := map[string]any{"name": name, "target": map[string]string{"hash": resolved}}
		if cmd.Flags().Changed("message") {
			body["message"] = message
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), base+"/refs/tags", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(addTargetMetadata(raw, target, resolved), output.TagFields, output.TagSummary)
	}}
	createCmd.Flags().StringVar(&target, "target", "", "Commit hash or branch to point at")
	createCmd.Flags().StringVar(&message, "message", "", "Annotated tag message (Bitbucket creates an annotated tag and supplies a default when omitted)")

	var deleteYes bool
	deleteCmd := &cobra.Command{Use: "delete <name> --yes", Short: "Delete a tag", Long: "Delete a tag. This is a destructive write operation and requires --yes.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := validateRefName("tag", name); err != nil {
			return fail(err)
		}
		if !deleteYes {
			return fail(fmt.Errorf("refusing to delete tag %q without --yes", name))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(cmd), refPath(base, "tags", name), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return emitObject(json.RawMessage(fmt.Sprintf(`{"deleted":%q}`, name)), nil)
	}}
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")
	tagCmd.AddCommand(listCmd, viewCmd, createCmd, deleteCmd)
	rootCmd.AddCommand(tagCmd)
}
