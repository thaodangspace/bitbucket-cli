package cmd

import (
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/output"
)

// version is the bitbucket-cli release version. Release builds set it via
// -ldflags "-X github.com/thaodangspace/bitbucket-cli/cmd.version=...". For
// `go install`-ed builds it falls back to the module version from build info.
var version = "dev"

// resolveVersion prefers the ldflag-injected version and otherwise reports the
// module version recorded by `go install` (e.g. v0.1.0).
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

// Persistent flags shared by all subcommands.
var (
	flagWorkspace  string
	flagRepo       string
	flagRepository string
	flagPretty     bool
	flagJSON       string
	flagJQ         string
	flagTemplate   string
	flagFormat     string
	flagColor      string
	flagPager      string
	flagNoPager    bool
)

var rootCmd = &cobra.Command{
	Use:           "bitbucket-cli",
	Short:         "Bitbucket Cloud CLI for agents and humans",
	Long:          "bitbucket-cli exposes Bitbucket Cloud pull requests, branches, and repo\ninfo as scriptable commands. Output is JSON by default; pass --pretty for\nhuman-readable text. Credentials come from environment variables.",
	Version:       resolveVersion(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	args, err := expandAliasArgs(os.Args[1:])
	if err != nil {
		output.WriteError(os.Stderr, err)
		os.Exit(exitCode(err))
	}
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagWorkspace, "workspace", "", "Bitbucket workspace slug (defaults to BITBUCKET_DEFAULT_WORKSPACE or git remote)")
	pf.StringVar(&flagRepo, "repo", "", "Bitbucket repository slug (defaults to BITBUCKET_DEFAULT_REPO or git remote)")
	pf.StringVarP(&flagRepository, "repository", "R", "", "Repository selector (workspace/repo, Bitbucket URL, or current git remote)")
	pf.BoolVar(&flagPretty, "pretty", false, "Render human-readable table output (alias for --format table)")
	pf.StringVar(&flagJSON, "json", "", "Select documented output fields (comma-separated)")
	pf.StringVar(&flagJQ, "jq", "", "Transform JSON output with a jq expression")
	pf.StringVar(&flagTemplate, "template", "", "Format JSON output with a Go template")
	pf.StringVar(&flagFormat, "format", "json", "Output format: json, table, yaml, or raw")
	pf.StringVar(&flagColor, "color", "auto", "Colorize output: auto, always, or never")
	pf.StringVar(&flagPager, "pager", "auto", "Pager behavior: auto, always, or never")
	pf.BoolVar(&flagNoPager, "no-pager", false, "Disable paging (alias for --pager never)")
	rootCmd.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFlags(); err != nil {
			return fail(err)
		}
		return nil
	}
}
