package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
	"github.com/thaodangspace/bitbucket-cli/selector"

	"github.com/spf13/cobra"
)

// commentPath returns the comments collection for a pull request.
func commentPath(base string, prID int) string {
	return fmt.Sprintf("%s/pullrequests/%d/comments", base, prID)
}

func renderJSON(v any) error { return output.RenderJSON(os.Stdout, v) }

func commentID(raw json.RawMessage) any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	if id, ok := m["id"]; ok {
		return jsonNumber(id)
	}
	return nil
}

func postComment(cmd *cobra.Command, selectedID int, selectedRepo *repoContext, body string, parentID int, inline map[string]any, pending bool) (json.RawMessage, error) {
	payload := map[string]any{"content": map[string]any{"raw": body}}
	if parentID > 0 {
		payload["parent"] = map[string]any{"id": parentID}
	}
	if inline != nil {
		payload["inline"] = inline
	}
	if pending {
		payload["pending"] = true
	}
	var raw json.RawMessage
	if err := selectedRepo.client.Request(ctx(cmd), commentPath(selectedRepo.base, selectedID), bitbucket.RequestOptions{Method: http.MethodPost, Body: payload}, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// repoContext keeps the client and resolved API base together for mutation
// commands, avoiding accidental requests against a different repository.
type repoContext struct {
	client *bitbucket.Client
	base   string
}

func newPRContext(selectedRepo *selector.PullRequestSelector) (repoContext, error) {
	cfg, client, err := newClient()
	if err != nil {
		return repoContext{}, err
	}
	_, base, err := resolveRepoFor(cfg, selectedRepo.Repository)
	if err != nil {
		return repoContext{}, err
	}
	return repoContext{client: client, base: base}, nil
}

// selectPRContext supports the optional selector form used by review and
// comment commands by resolving the current git branch when omitted.
func selectPRContext(cmd *cobra.Command, args []string) (selector.PullRequestSelector, repoContext, error) {
	if len(args) == 1 {
		selected, err := parsePullRequestSelector(args[0])
		if err != nil {
			return selector.PullRequestSelector{}, repoContext{}, err
		}
		prctx, err := newPRContext(&selected)
		return selected, prctx, err
	}
	cfg, client, err := newClient()
	if err != nil {
		return selector.PullRequestSelector{}, repoContext{}, err
	}
	ref, base, err := resolveRepo(cfg)
	if err != nil {
		return selector.PullRequestSelector{}, repoContext{}, err
	}
	branch, err := currentGitBranch()
	if err != nil {
		return selector.PullRequestSelector{}, repoContext{}, err
	}
	q := url.Values{"q": {fmt.Sprintf("source.branch.name=%q", branch)}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
	values, err := client.Paginate(ctx(cmd), fmt.Sprintf("%s/pullrequests?%s", base, q.Encode()), 1, bitbucket.DefaultMaxPages)
	if err != nil {
		return selector.PullRequestSelector{}, repoContext{}, err
	}
	if len(values) == 0 {
		return selector.PullRequestSelector{}, repoContext{}, fmt.Errorf("no pull request found for current branch %q", branch)
	}
	var item map[string]any
	if err := json.Unmarshal(values[0], &item); err != nil {
		return selector.PullRequestSelector{}, repoContext{}, err
	}
	id, ok := item["id"].(float64)
	if !ok || id <= 0 {
		return selector.PullRequestSelector{}, repoContext{}, fmt.Errorf("current branch pull request has no valid ID")
	}
	repo := selector.Repository{Workspace: ref.Workspace, Repo: ref.RepoSlug}
	return selector.PullRequestSelector{Repository: &repo, ID: int(id)}, repoContext{client: client, base: base}, nil
}

func init() {
	var (
		body       string
		bodyFile   string
		replyTo    string
		inlinePath string
		inlineFrom string
		inlineTo   string
		pending    bool
	)
	commentCmd := &cobra.Command{
		Use:   "comment [<id>]",
		Short: "Create or manage pull request comments",
		Long:  "Create, edit, or delete comments on a Bitbucket Cloud pull request. These are write operations; use only when the user has asked to change the pull request.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bodyValue, provided, err := readBody(body, bodyFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			if !provided || strings.TrimSpace(bodyValue) == "" {
				return fail(fmt.Errorf("provide a non-empty --body or --body-file"))
			}

			var parentID int
			if cmd.Flags().Changed("reply-to") {
				parentID, err = parsePositiveID("reply-to comment id", replyTo)
				if err != nil {
					return fail(err)
				}
			}
			inline, err := parseInline(inlinePath, inlineFrom, inlineTo, cmd.Flags().Changed("path"), cmd.Flags().Changed("from"), cmd.Flags().Changed("to"))
			if err != nil {
				return fail(err)
			}
			if inline != nil && parentID > 0 {
				return fail(fmt.Errorf("--reply-to cannot be combined with inline comment flags"))
			}
			selected, prctx, err := selectPRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			raw, err := postComment(cmd, selected.ID, &prctx, bodyValue, parentID, inline, pending)
			if err != nil {
				return fail(err)
			}
			if flagPretty {
				_, err := fmt.Fprintf(os.Stdout, "Posted comment #%v on pull request #%d.\n", commentID(raw), selected.ID)
				return err
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return fail(err)
			}
			return renderJSON(value)
		},
	}
	commentCmd.Flags().StringVar(&body, "body", "", "Markdown comment body")
	commentCmd.Flags().StringVar(&bodyFile, "body-file", "", "Read the comment body from a file (- for stdin)")
	commentCmd.Flags().StringVar(&replyTo, "reply-to", "", "Parent comment ID to reply to")
	commentCmd.Flags().StringVar(&inlinePath, "path", "", "File path for an inline comment")
	commentCmd.Flags().StringVar(&inlineFrom, "from", "", "Positive old-side file line number for an inline comment")
	commentCmd.Flags().StringVar(&inlineTo, "to", "", "Positive new-side file line number for an inline comment")
	commentCmd.Flags().BoolVar(&pending, "pending", false, "Send Bitbucket's pending=true comment field; this creates no local draft and may be rejected by unsupported workflows")

	var editBody, editFile string
	editCmd := &cobra.Command{
		Use:   "edit <pr> <comment-id>",
		Short: "Edit a pull request comment",
		Long:  "Edit a pull request comment. This is a write operation; use only when explicitly requested.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			cid, err := parsePositiveID("comment id", args[1])
			if err != nil {
				return fail(err)
			}
			value, provided, err := readBody(editBody, editFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			if !provided || strings.TrimSpace(value) == "" {
				return fail(fmt.Errorf("provide a non-empty --body or --body-file"))
			}
			prctx, err := newPRContext(&selected)
			if err != nil {
				return fail(err)
			}
			if err := ensureCommentOwner(cmd, prctx, selected.ID, cid); err != nil {
				return fail(err)
			}
			var raw json.RawMessage
			path := fmt.Sprintf("%s/pullrequests/%d/comments/%d", prctx.base, selected.ID, cid)
			if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPut, Body: map[string]any{"content": map[string]any{"raw": value}}}, &raw); err != nil {
				return fail(err)
			}
			var out any
			if err := json.Unmarshal(raw, &out); err != nil {
				return fail(err)
			}
			return renderJSON(out)
		},
	}
	editCmd.Flags().StringVar(&editBody, "body", "", "Replacement markdown body")
	editCmd.Flags().StringVar(&editFile, "body-file", "", "Read the replacement body from a file (- for stdin)")

	var deleteYes bool
	deleteCmd := &cobra.Command{
		Use:   "delete <pr> <comment-id>",
		Short: "Delete a pull request comment",
		Long:  "Delete a pull request comment. This is a write operation and requires --yes in non-interactive mode.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			cid, err := parsePositiveID("comment id", args[1])
			if err != nil {
				return fail(err)
			}
			if !deleteYes {
				return fail(fmt.Errorf("refusing to delete comment without --yes"))
			}
			prctx, err := newPRContext(&selected)
			if err != nil {
				return fail(err)
			}
			if err := ensureCommentOwner(cmd, prctx, selected.ID, cid); err != nil {
				return fail(err)
			}
			path := fmt.Sprintf("%s/pullrequests/%d/comments/%d", prctx.base, selected.ID, cid)
			if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
				return fail(err)
			}
			return renderJSON(map[string]any{"deleted": true, "id": cid})
		},
	}
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")

	commentCmd.AddCommand(editCmd, deleteCmd)
	prCmd.AddCommand(commentCmd)
}

// parseInline validates Bitbucket's file-line model without attempting to
// translate unified diff positions.
func parseInline(path, from, to string, pathSet, fromSet, toSet bool) (map[string]any, error) {
	if !pathSet && !fromSet && !toSet {
		return nil, nil
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("--path is required for an inline comment")
	}
	if !fromSet && !toSet {
		return nil, fmt.Errorf("inline comments require --from and/or --to")
	}
	inline := map[string]any{"path": path}
	for name, value := range map[string]string{"from": from, "to": to} {
		if value == "" {
			continue
		}
		n, err := parsePositiveID("inline "+name+" line", value)
		if err != nil {
			return nil, err
		}
		inline[name] = n
	}
	return inline, nil
}

// ensureCommentOwner performs the required read before mutation. If the API
// exposes an owner account ID, compare it with the authenticated /user result
// (or BITBUCKET_ACCOUNT_ID) before issuing PUT/DELETE. Display names are not
// identity evidence, so missing owner data is left to Bitbucket to enforce.
func ensureCommentOwner(cmd *cobra.Command, prctx repoContext, prID, commentID int) error {
	var comment map[string]any
	path := fmt.Sprintf("%s/pullrequests/%d/comments/%d", prctx.base, prID, commentID)
	if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &comment); err != nil {
		return err
	}
	user, _ := comment["user"].(map[string]any)
	owner, ownerOK := user["account_id"].(string)
	if !ownerOK || strings.TrimSpace(owner) == "" {
		return nil
	}
	known := strings.TrimSpace(os.Getenv("BITBUCKET_ACCOUNT_ID"))
	if known == "" {
		var me map[string]any
		if err := prctx.client.Request(ctx(cmd), "/user", bitbucket.RequestOptions{}, &me); err == nil {
			known, _ = me["account_id"].(string)
		}
	}
	if known != "" && owner != known {
		return fmt.Errorf("comment #%d is owned by another Bitbucket account", commentID)
	}
	return nil
}

// jsonNumber renders a whole JSON number as an integer for display.
func jsonNumber(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}
