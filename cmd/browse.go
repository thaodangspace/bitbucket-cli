package cmd

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
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

func buildPRURL(workspace, repo string, id int) string {
	return fmt.Sprintf("https://bitbucket.org/%s/%s/pull-requests/%d", url.PathEscape(workspace), url.PathEscape(repo), id)
}

func buildPipelineURL(workspace, repo, uuid string) string {
	return fmt.Sprintf("https://bitbucket.org/%s/%s/addon/pipelines/home#!/results/%s", url.PathEscape(workspace), url.PathEscape(repo), url.PathEscape(uuid))
}

func buildRepositoryURL(workspace, repo string) string {
	u, _ := buildBrowseURL(workspace, repo, "", "")
	return u
}

func openWeb(target string) error {
	if err := openBrowser(target); err != nil {
		return fail(fmt.Errorf("open browser: %w", err))
	}
	return nil
}

func buildBrowseURL(workspace, repo, path, branch string) (string, error) {
	segments := []string{workspace, repo}
	if strings.TrimSpace(branch) != "" {
		segments = append(segments, "src", branch)
	}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return "", fmt.Errorf("browse path must not contain %q", part)
		}
		segments = append(segments, part)
	}
	u := url.URL{Scheme: "https", Host: "bitbucket.org", Path: "/" + strings.Join(segments, "/")}
	return u.String(), nil
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
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			target, err := buildBrowseURL(ref.Workspace, ref.RepoSlug, path, branch)
			if err != nil {
				return fail(err)
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
