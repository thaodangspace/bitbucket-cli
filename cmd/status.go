package cmd

import (
	"fmt"
	"os"

	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Check bitbucket-cli configuration",
		Long:  "Report whether credentials resolve from the environment and which default repository is in effect.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fail(err)
			}

			defaultRepo := ""
			if cfg.DefaultWorkspace != "" && cfg.DefaultRepo != "" {
				defaultRepo = cfg.DefaultWorkspace + "/" + cfg.DefaultRepo
			}

			if flagPretty {
				repo := defaultRepo
				if repo == "" {
					repo = "not configured"
				}
				_, err := fmt.Fprintf(os.Stdout, "Bitbucket configured. Default repo: %s\n", repo)
				return err
			}

			return output.RenderJSON(os.Stdout, map[string]any{
				"configured":  true,
				"email":       cfg.Email,
				"defaultRepo": nullableString(defaultRepo),
			})
		},
	})
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
