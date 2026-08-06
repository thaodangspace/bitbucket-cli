package cmd

import (
	"fmt"
	"net/url"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

func init() {
	branchCmd := &cobra.Command{
		Use:   "branch",
		Short: "Branch commands",
	}

	var (
		query string
		limit int
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List branches in a repository",
		Args:  cobra.NoArgs,
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
			path := fmt.Sprintf("%s/refs/branches?%s", base, q.Encode())

			values, err := client.Paginate(ctx(cmd), path, limit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			if err := emitListFields(values, output.BranchFields, output.BranchSummary, "No branches found."); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&query, "query", "", `Bitbucket q expression, e.g. name ~ "feature/"`)
	listCmd.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum branches to return")

	branchCmd.AddCommand(listCmd)
	rootCmd.AddCommand(branchCmd)
}
