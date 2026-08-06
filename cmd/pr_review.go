package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

// partialReviewError is returned when the requested review comment was
// created but the participant action failed. Review actions are not atomic in
// Bitbucket, so callers must be told exactly what succeeded.
type partialReviewError struct {
	action    string
	commentID any
	err       error
}

func (e *partialReviewError) Error() string {
	return fmt.Sprintf("review %s failed after posting comment #%v: %v", e.action, e.commentID, e.err)
}
func (e *partialReviewError) Unwrap() error { return e.err }
func (e *partialReviewError) Details() map[string]any {
	return map[string]any{"partial_success": true, "action": e.action, "comment_id": e.commentID}
}

func participantAction(cmd *cobra.Command, selectedID int, prctx repoContext, action string) (json.RawMessage, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/%s", prctx.base, selectedID, action)
	method := http.MethodPost
	if strings.HasPrefix(action, "remove-") || action == "unapprove" {
		method = http.MethodDelete
		path = strings.TrimSuffix(path, "unapprove") + "approve"
		if action == "remove-change-request" {
			path = strings.TrimSuffix(path, "remove-change-request") + "request-changes"
		}
	}
	var raw json.RawMessage
	if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: method}, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func reviewResult(prID int, action string, comment json.RawMessage, participant json.RawMessage) (map[string]any, error) {
	result := map[string]any{"pull_request_id": prID, "action": action}
	if comment != nil {
		var v any
		if err := json.Unmarshal(comment, &v); err != nil {
			return nil, err
		}
		result["comment"] = v
	}
	if participant != nil {
		var v any
		if err := json.Unmarshal(participant, &v); err == nil && v != nil {
			result["participant"] = v
		}
	}
	return result, nil
}

func init() {
	var approve, requestChanges, comment bool
	var body, bodyFile string
	reviewCmd := &cobra.Command{
		Use:   "review [<id>]",
		Short: "Approve, request changes, or comment on a pull request",
		Long:  "Apply one Bitbucket Cloud pull request review action. This is a write operation. A review body is posted as a separate PR comment before approve/request-changes, so a failed action may leave a successfully posted comment behind.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			count := 0
			for _, set := range []bool{approve, requestChanges, comment} {
				if set {
					count++
				}
			}
			if count != 1 {
				return fail(fmt.Errorf("exactly one of --approve, --request-changes, or --comment is required"))
			}
			bodyValue, provided, err := readBody(body, bodyFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			action := "comment"
			if approve {
				action = "approve"
			}
			if requestChanges {
				action = "request-changes"
			}
			if action == "comment" && (!provided || strings.TrimSpace(bodyValue) == "") {
				return fail(fmt.Errorf("--comment requires a non-empty --body or --body-file"))
			}
			if action == "request-changes" && (!provided || strings.TrimSpace(bodyValue) == "") {
				return fail(fmt.Errorf("--request-changes requires a non-empty review body"))
			}
			selected, prctx, err := selectPRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			var posted json.RawMessage
			if provided && strings.TrimSpace(bodyValue) != "" {
				posted, err = postComment(cmd, selected.ID, &prctx, bodyValue, 0, nil, false)
				if err != nil {
					return fail(err)
				}
			}
			if action == "comment" {
				var value any
				if err := json.Unmarshal(posted, &value); err != nil {
					return fail(err)
				}
				return renderJSON(value)
			}
			participant, err := participantAction(cmd, selected.ID, prctx, action)
			if err != nil {
				if posted != nil {
					return fail(&partialReviewError{action: action, commentID: commentID(posted), err: err})
				}
				return fail(err)
			}
			result, err := reviewResult(selected.ID, action, posted, participant)
			if err != nil {
				return fail(err)
			}
			return renderJSON(result)
		},
	}
	reviewCmd.Flags().BoolVar(&approve, "approve", false, "Approve the pull request")
	reviewCmd.Flags().BoolVar(&requestChanges, "request-changes", false, "Request changes (requires a non-empty review body)")
	reviewCmd.Flags().BoolVar(&comment, "comment", false, "Post a review comment without approving or requesting changes")
	reviewCmd.Flags().StringVar(&body, "body", "", "Review/comment body")
	reviewCmd.Flags().StringVar(&bodyFile, "body-file", "", "Read the review/comment body from a file (- for stdin)")

	prCmd.AddCommand(reviewCmd)

	for _, spec := range []struct {
		name, use, short, action string
	}{
		{"unapprove", "unapprove [<id>]", "Remove your approval from a pull request", "unapprove"},
		{"remove-change-request", "remove-change-request [<id>]", "Remove your change request from a pull request", "remove-change-request"},
	} {
		s := spec
		cmd := &cobra.Command{
			Use: s.use, Short: s.short,
			Long: s.short + ". This is a write operation; use only when explicitly requested.", Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				selected, prctx, err := selectPRContext(cmd, args)
				if err != nil {
					return fail(err)
				}
				raw, err := participantAction(cmd, selected.ID, prctx, s.action)
				if err != nil {
					return fail(err)
				}
				result, err := reviewResult(selected.ID, s.action, nil, raw)
				if err != nil {
					return fail(err)
				}
				return renderJSON(result)
			},
		}
		prCmd.AddCommand(cmd)
	}
}
