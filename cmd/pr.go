package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/dtonair/bitbucket-cli/bitbucket"
	"github.com/dtonair/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

// prCmd is the parent for pull request subcommands. The write subcommand
// (comment) registers itself onto this from pr_comment.go.
var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Pull request commands",
}

func init() {
	var (
		listState  string
		listLimit  int
		listAuthor string
		listMine   bool
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests for a repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listMine && listAuthor != "" {
				return fail(fmt.Errorf("--author and --mine are mutually exclusive"))
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			_, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}

			author := listAuthor
			if listMine {
				author, err = currentAccountID(ctx(cmd), client)
				if err != nil {
					return fail(err)
				}
			}

			q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
			if listState != "" {
				q.Set("state", listState)
			}
			if author != "" {
				q.Set("q", fmt.Sprintf("author.account_id=%q", author))
			}
			path := fmt.Sprintf("%s/pullrequests?%s", base, q.Encode())

			values, err := client.Paginate(ctx(cmd), path, listLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			if err := emitList(values, output.PullRequestSummary, "No pull requests found."); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&listState, "state", "", "Filter by state: OPEN, MERGED, DECLINED, or SUPERSEDED")
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum pull requests to return")
	listCmd.Flags().StringVar(&listAuthor, "author", "", "Filter by author account ID (e.g. from the Bitbucket profile URL)")
	listCmd.Flags().BoolVar(&listMine, "mine", false, "Filter to pull requests authored by the authenticated user")

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single pull request by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
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

			var raw json.RawMessage
			path := fmt.Sprintf("%s/pullrequests/%d", base, id)
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &raw); err != nil {
				return fail(err)
			}
			if err := emitObject(raw, output.PullRequestSummary); err != nil {
				return fail(err)
			}
			return nil
		},
	}

	var commentsLimit int
	commentsCmd := &cobra.Command{
		Use:   "comments <id>",
		Short: "List comments on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
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

			path := fmt.Sprintf("%s/pullrequests/%d/comments?pagelen=%d", base, id, bitbucket.DefaultPageLen)
			values, err := client.Paginate(ctx(cmd), path, commentsLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			if err := emitList(values, output.CommentSummary, "No comments found."); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	commentsCmd.Flags().IntVar(&commentsLimit, "limit", bitbucket.DefaultLimit, "Maximum comments to return")

	var commitsLimit int
	commitsCmd := &cobra.Command{
		Use:   "commits <id>",
		Short: "List commits on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
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

			path := fmt.Sprintf("%s/pullrequests/%d/commits?pagelen=%d", base, id, bitbucket.DefaultPageLen)
			values, err := client.Paginate(ctx(cmd), path, commitsLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			if err := emitList(values, output.CommitSummary, "No commits found."); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	commitsCmd.Flags().IntVar(&commitsLimit, "limit", bitbucket.DefaultLimit, "Maximum commits to return")

	prCmd.AddCommand(listCmd, getCmd, commentsCmd, commitsCmd)
	rootCmd.AddCommand(prCmd)
}

// currentAccountID resolves the authenticated user's account ID via GET /user.
func currentAccountID(c context.Context, client *bitbucket.Client) (string, error) {
	var user struct {
		AccountID string `json:"account_id"`
	}
	if err := client.Request(c, "/user", bitbucket.RequestOptions{}, &user); err != nil {
		return "", err
	}
	if user.AccountID == "" {
		return "", fmt.Errorf("could not resolve authenticated user's account ID from /user")
	}
	return user.AccountID, nil
}
