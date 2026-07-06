package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/dtonair/bitbucket-cli/bitbucket"
	"github.com/dtonair/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

func init() {
	var body string
	var replyTo string
	commentCmd := &cobra.Command{
		Use:   "comment <id>",
		Short: "Post a markdown comment on a pull request",
		Long:  "Create a markdown comment on a Bitbucket Cloud pull request. This is a write operation; use only when the user has asked to post the comment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return fail(err)
			}
			if strings.TrimSpace(body) == "" {
				return fail(fmt.Errorf("Provide a non-empty body."))
			}

			var parentID int
			if cmd.Flags().Changed("reply-to") {
				parentID, err = parsePositiveID("reply-to comment id", replyTo)
				if err != nil {
					return fail(err)
				}
			}

			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			_, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}

			payload := map[string]any{
				"content": map[string]any{"raw": body},
			}
			if parentID > 0 {
				payload["parent"] = map[string]any{"id": parentID}
			}
			var raw json.RawMessage
			path := fmt.Sprintf("%s/pullrequests/%d/comments", base, id)
			if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPost, Body: payload}, &raw); err != nil {
				return fail(err)
			}

			if flagPretty {
				m, err := toMap(raw)
				if err != nil {
					return fail(err)
				}
				commentID := "unknown"
				if v, ok := m["id"]; ok {
					commentID = fmt.Sprintf("%v", jsonNumber(v))
				}
				_, err = fmt.Fprintf(os.Stdout, "Posted comment #%s on pull request #%d.\n", commentID, id)
				return err
			}

			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				return fail(err)
			}
			return output.RenderJSON(os.Stdout, v)
		},
	}
	commentCmd.Flags().StringVar(&body, "body", "", "Markdown comment body to post (required)")
	commentCmd.Flags().StringVar(&replyTo, "reply-to", "", "Parent comment ID to reply to")
	_ = commentCmd.MarkFlagRequired("body")

	prCmd.AddCommand(commentCmd)
}

// jsonNumber renders a whole JSON number as an integer for display.
func jsonNumber(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}
