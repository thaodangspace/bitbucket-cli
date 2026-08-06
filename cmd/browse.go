package cmd

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

// openBrowser is injectable so browse can be tested without launching a GUI.
var openBrowser = func(target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}
	return exec.Command(name, args...).Run()
}

func init() {
	var branch string
	var noBrowser bool
	browseCmd := &cobra.Command{
		Use:   "browse [path]",
		Short: "Open the repository in a browser",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fail(err)
			}
			ref, _, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}
			target := "https://bitbucket.org/" + bitbucket.EncodePathSegment(ref.Workspace) + "/" + bitbucket.EncodePathSegment(ref.RepoSlug)
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				path := strings.Trim(args[0], "/")
				if strings.Contains(path, "..") {
					return fail(fmt.Errorf("browse path must not contain .."))
				}
				target += "/" + path
			}
			if strings.TrimSpace(branch) != "" {
				target += "/branch/" + url.PathEscape(branch)
			}
			if noBrowser {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), target)
				return err
			}
			if err := openBrowser(target); err != nil {
				return fail(fmt.Errorf("open browser: %w", err))
			}
			return nil
		},
	}
	browseCmd.Flags().StringVar(&branch, "branch", "", "Branch to view")
	browseCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the URL without opening a browser")
	rootCmd.AddCommand(browseCmd)
}
