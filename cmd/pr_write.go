package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

// readBody resolves a text body from a flag value, a file path, or stdin. The
// flag and file sources are mutually exclusive. A file source of "-" reads
// stdin. The returned `provided` is true whenever the caller supplied a value:
// a file always counts as provided (even when empty, enabling an explicit
// clear), while a bare flag counts only when non-empty.
func readBody(text, file string, stdin io.Reader) (value string, provided bool, err error) {
	if text != "" && file != "" {
		return "", false, fmt.Errorf("use only one of --description / --description-file")
	}
	if file != "" {
		if file == "-" {
			b, rerr := io.ReadAll(stdin)
			if rerr != nil {
				return "", false, fmt.Errorf("read body from stdin: %w", rerr)
			}
			return string(b), true, nil
		}
		b, rerr := os.ReadFile(file)
		if rerr != nil {
			return "", false, fmt.Errorf("read body file %q: %w", file, rerr)
		}
		return string(b), true, nil
	}
	return text, text != "", nil
}

func init() {
	// pr update <id> — edit title and/or description of an existing PR.
	var (
		updTitle string
		updDesc  string
		updDescF string
	)
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a pull request's title and/or description",
		Long: "Update the title and/or description of an existing Bitbucket Cloud pull " +
			"request. This is a write operation; use only when the user has asked to " +
			"update the pull request. Existing reviewers are preserved.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return fail(err)
			}
			desc, descProvided, err := readBody(updDesc, updDescF, os.Stdin)
			if err != nil {
				return fail(err)
			}
			titleProvided := strings.TrimSpace(updTitle) != ""
			if !titleProvided && !descProvided {
				return fail(fmt.Errorf("Provide --title and/or --description."))
			}

			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			_, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}

			// Read-modify-write: fetch the current PR so we can preserve the
			// title (required by Bitbucket on PUT) and the reviewers, which a
			// bare PUT would otherwise clear.
			path := fmt.Sprintf("%s/pullrequests/%d", base, id)
			var current map[string]any
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &current); err != nil {
				return fail(err)
			}

			body := map[string]any{}
			if titleProvided {
				body["title"] = updTitle
			} else if t, ok := current["title"].(string); ok {
				body["title"] = t
			}
			if descProvided {
				body["description"] = desc
			}
			if reviewers, ok := current["reviewers"]; ok && reviewers != nil {
				body["reviewers"] = reviewers
			}

			var raw json.RawMessage
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
				return fail(err)
			}
			if err := emitObjectFields(raw, output.PullRequestFields, output.PullRequestSummary); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updTitle, "title", "", "New pull request title")
	updateCmd.Flags().StringVar(&updDesc, "description", "", "New description (markdown)")
	updateCmd.Flags().StringVar(&updDescF, "description-file", "", "Read description from file (- for stdin)")

	// pr create — open a new pull request.
	var (
		crSource string
		crDest   string
		crTitle  string
		crDesc   string
		crDescF  string
		crClose  bool
	)
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new pull request",
		Long: "Create a new Bitbucket Cloud pull request from a source branch. This is a " +
			"write operation; use only when the user has asked to create the pull request.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(crSource) == "" {
				return fail(fmt.Errorf("Provide --source (the source branch name)."))
			}
			if strings.TrimSpace(crTitle) == "" {
				return fail(fmt.Errorf("Provide --title."))
			}
			desc, descProvided, err := readBody(crDesc, crDescF, os.Stdin)
			if err != nil {
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

			body := map[string]any{
				"title":               crTitle,
				"source":              map[string]any{"branch": map[string]any{"name": crSource}},
				"close_source_branch": crClose,
			}
			if strings.TrimSpace(crDest) != "" {
				body["destination"] = map[string]any{"branch": map[string]any{"name": crDest}}
			}
			if descProvided {
				body["description"] = desc
			}

			var raw json.RawMessage
			path := fmt.Sprintf("%s/pullrequests", base)
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &raw); err != nil {
				return fail(err)
			}
			if err := emitObjectFields(raw, output.PullRequestFields, output.PullRequestSummary); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	createCmd.Flags().StringVar(&crSource, "source", "", "Source branch name (required)")
	createCmd.Flags().StringVar(&crDest, "destination", "", "Destination branch name (defaults to the repo main branch)")
	createCmd.Flags().StringVar(&crTitle, "title", "", "Pull request title (required)")
	createCmd.Flags().StringVar(&crDesc, "description", "", "Description (markdown)")
	createCmd.Flags().StringVar(&crDescF, "description-file", "", "Read description from file (- for stdin)")
	createCmd.Flags().BoolVar(&crClose, "close-source-branch", false, "Close the source branch when the PR is merged")
	_ = createCmd.MarkFlagRequired("source")
	_ = createCmd.MarkFlagRequired("title")

	prCmd.AddCommand(updateCmd, createCmd)
}
