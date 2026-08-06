package cmd

import (
	"encoding/json"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

func init() {
	var web bool
	repoCmd := &cobra.Command{
		Use:   "repo",
		Short: "Repository commands",
	}

	repoCmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get details for a Bitbucket Cloud repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}
			if web {
				return openWeb(buildRepositoryURL(ref.Workspace, ref.RepoSlug))
			}

			var raw json.RawMessage
			if err := client.Request(ctx(cmd), base, bitbucket.RequestOptions{}, &raw); err != nil {
				return fail(err)
			}
			if err := emitObjectFields(raw, output.RepoFields, output.RepoSummary); err != nil {
				return fail(err)
			}
			return nil
		},
	})
	repoCmd.Commands()[0].Flags().BoolVar(&web, "web", false, "Open the repository in a browser")

	rootCmd.AddCommand(repoCmd)
}
