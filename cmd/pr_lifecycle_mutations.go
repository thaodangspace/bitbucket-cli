package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
)

func responseJSON(resp *bitbucket.Response) (any, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return value, nil
}

func lifecycleState(pr map[string]any) string {
	state, _ := pr["state"].(string)
	return strings.ToUpper(state)
}

func mergeTaskID(value any) string {
	if item, ok := value.(map[string]any); ok {
		for _, key := range []string{"task_id", "id", "uuid"} {
			if id, ok := item[key]; ok && fmt.Sprint(id) != "" {
				return fmt.Sprint(id)
			}
		}
		if nested, ok := item["task_status"]; ok {
			return mergeTaskID(nested)
		}
	}
	return ""
}

func addLifecycleCommands() {
	var strategy, mergeMessage, mergeMessageFile string
	var closeSource, mergeAsync bool
	var mergeInterval, mergeTimeout time.Duration
	mergeCmd := &cobra.Command{
		Use: "merge [<selector>]", Short: "Merge a pull request",
		Long: "Merge a Bitbucket Cloud pull request. This is a write operation; use only when explicitly requested.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strategy != "merge_commit" && strategy != "squash" && strategy != "fast_forward" {
				return fail(fmt.Errorf("invalid --strategy %q (use merge_commit, squash, or fast_forward)", strategy))
			}
			message, provided, err := readBody(mergeMessage, mergeMessageFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			selected, prctx, err := resolvePRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			pr, err := prObject(cmd, prctx, selected.ID)
			if err != nil {
				return fail(err)
			}
			state := lifecycleState(pr)
			if state != "OPEN" {
				return fail(fmt.Errorf("cannot merge pull request #%d: current state is %s", selected.ID, state))
			}
			payload := map[string]any{"merge_strategy": strategy, "close_source_branch": closeSource}
			if provided {
				payload["message"] = message
			}
			resp, err := prctx.client.Do(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/merge", prctx.base, selected.ID), bitbucket.RequestOptions{Method: http.MethodPost, Body: payload})
			if err != nil {
				return fail(err)
			}
			value, err := responseJSON(resp)
			if err != nil {
				return fail(err)
			}
			if resp.StatusCode == http.StatusAccepted {
				taskID := mergeTaskID(value)
				if location := resp.Header.Get("Location"); location != "" {
					locatedID, locationErr := mergeTaskIDFromLocation(location)
					if locationErr != nil {
						return fail(locationErr)
					}
					taskID = locatedID
				}
				if taskID == "" {
					return fail(fmt.Errorf("Bitbucket accepted merge but returned no task identifier or valid Location header"))
				}
				if mergeAsync {
					return renderJSON(map[string]any{"accepted": true, "task_id": taskID})
				}
				return waitForMergeTask(cmd, prctx, selected.ID, taskID, mergeInterval, mergeTimeout)
			}
			return renderJSON(value)
		},
	}
	mergeCmd.Flags().StringVar(&strategy, "strategy", "merge_commit", "Merge strategy: merge_commit, squash, or fast_forward")
	mergeCmd.Flags().StringVar(&mergeMessage, "message", "", "Merge commit message")
	mergeCmd.Flags().StringVar(&mergeMessageFile, "message-file", "", "Read merge message from a file (- for stdin)")
	mergeCmd.Flags().BoolVar(&closeSource, "close-source-branch", false, "Close the source branch after merging")
	mergeCmd.Flags().BoolVar(&mergeAsync, "async", false, "Return after Bitbucket accepts an asynchronous merge")
	mergeCmd.Flags().DurationVar(&mergeInterval, "interval", 2*time.Second, "Merge task polling interval")
	mergeCmd.Flags().DurationVar(&mergeTimeout, "timeout", 10*time.Minute, "Maximum time to wait for an asynchronous merge")
	prCmd.AddCommand(mergeCmd)

	var declineMessage, declineMessageFile string
	var declineYes bool
	declineCmd := &cobra.Command{
		Use: "decline [<selector>]", Short: "Decline a pull request",
		Long: "Decline a Bitbucket Cloud pull request. This is a destructive write operation; use only when explicitly requested. Interactive terminals prompt unless --yes is supplied.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			message, provided, err := readBody(declineMessage, declineMessageFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			if !declineYes && !stdinIsTTY() {
				return fail(fmt.Errorf("refusing to decline pull request without --yes in non-interactive mode"))
			}
			if !declineYes {
				fmt.Fprint(os.Stderr, "Decline pull request? [y/N] ")
				var answer string
				fmt.Fscanln(os.Stdin, &answer)
				if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
					return fail(fmt.Errorf("decline cancelled"))
				}
			}
			selected, prctx, err := resolvePRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			payload := map[string]any{}
			if provided {
				payload["message"] = message
			}
			var raw json.RawMessage
			if err := prctx.client.Request(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/decline", prctx.base, selected.ID), bitbucket.RequestOptions{Method: http.MethodPost, Body: payload}, &raw); err != nil {
				return fail(err)
			}
			return renderJSONValue(raw)
		},
	}
	declineCmd.Flags().StringVar(&declineMessage, "message", "", "Decline message")
	declineCmd.Flags().StringVar(&declineMessageFile, "message-file", "", "Read decline message from a file (- for stdin)")
	declineCmd.Flags().BoolVar(&declineYes, "yes", false, "Confirm the destructive action")
	prCmd.AddCommand(declineCmd)

	reopenCmd := &cobra.Command{
		Use: "reopen [<selector>]", Short: "Reopen a declined pull request",
		Long: "Reopen a Bitbucket Cloud pull request. This is a write operation; use only when explicitly requested.", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, prctx, err := resolvePRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			var raw json.RawMessage
			if err := prctx.client.Request(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/reopen", prctx.base, selected.ID), bitbucket.RequestOptions{Method: http.MethodPost}, &raw); err != nil {
				return fail(err)
			}
			return renderJSONValue(raw)
		},
	}
	prCmd.AddCommand(reopenCmd)

	var workspace string
	var mine, reviewRequested bool
	statusCmd := &cobra.Command{
		Use: "status", Short: "Show pull requests for review or the current branch", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !mine && !reviewRequested {
				selected, prctx, err := resolvePRContext(cmd, nil)
				if err != nil {
					return fail(err)
				}
				raw, err := prRaw(cmd, prctx, selected.ID, "")
				if err != nil {
					return fail(err)
				}
				return renderPR(raw)
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ws := workspace
			if ws == "" {
				ws = flagWorkspace
			}
			if ws == "" {
				ws = cfg.DefaultWorkspace
			}
			if ws == "" {
				return fail(fmt.Errorf("--workspace is required when no default workspace is configured"))
			}
			account, err := currentAccountID(ctx(cmd), client)
			if err != nil {
				return fail(err)
			}
			var values []json.RawMessage
			if mine {
				mineValues, err := client.PaginateAll(ctx(cmd), fmt.Sprintf("/workspaces/%s/pullrequests/%s?pagelen=%d", bitbucket.EncodePathSegment(ws), url.PathEscape(account), bitbucket.DefaultPageLen), bitbucket.DefaultMaxPages)
				if err != nil {
					return fail(err)
				}
				values = append(values, mineValues...)
			}
			if reviewRequested {
				ref, _, err := resolveRepo(cfg)
				if err != nil {
					return fail(err)
				}
				ref.Workspace = ws
				q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}, "q": {fmt.Sprintf("state=%q AND reviewers.account_id=%q", "OPEN", account)}}
				reviewValues, err := client.PaginateAll(ctx(cmd), fmt.Sprintf("/repositories/%s/%s/pullrequests?%s", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.RepoSlug), q.Encode()), bitbucket.DefaultMaxPages)
				if err != nil {
					return fail(err)
				}
				values = append(values, reviewValues...)
			}
			return emitListFields(dedupePullRequests(values), nil, output.PullRequestSummary, "No pull requests found.")
		},
	}
	statusCmd.Flags().StringVar(&workspace, "workspace", "", "Workspace slug for status views")
	statusCmd.Flags().BoolVar(&mine, "mine", false, "Show pull requests authored by the current account")
	statusCmd.Flags().BoolVar(&reviewRequested, "review-requested", false, "Show pull requests where the current account is a reviewer")
	prCmd.AddCommand(statusCmd)
}

func mergeTaskIDFromLocation(location string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(location))
	if err != nil || u.Path == "" {
		return "", fmt.Errorf("invalid merge task Location %q", location)
	}
	if u.IsAbs() && (u.Scheme != "https" || strings.ToLower(u.Hostname()) != "api.bitbucket.org" || u.Port() != "" || u.User != nil) {
		return "", fmt.Errorf("merge task Location must point to api.bitbucket.org")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return "", fmt.Errorf("merge task Location has no task identifier")
	}
	id, err := url.PathUnescape(parts[len(parts)-1])
	if err != nil || id == "" {
		return "", fmt.Errorf("merge task Location has invalid task identifier")
	}
	return id, nil
}

func dedupePullRequests(values []json.RawMessage) []json.RawMessage {
	seen := map[string]bool{}
	out := make([]json.RawMessage, 0, len(values))
	for _, raw := range values {
		var item map[string]any
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		key := fmt.Sprint(item["id"])
		if key == "<nil>" {
			key = string(raw)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, raw)
		}
	}
	return out
}

func waitForMergeTask(cmd *cobra.Command, prctx repoContext, prID int, taskID string, interval, timeout time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		var value map[string]any
		err := prctx.client.Request(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/merge/task-status/%s", prctx.base, prID, url.PathEscape(taskID)), bitbucket.RequestOptions{}, &value)
		if err != nil {
			return fail(err)
		}
		state := strings.ToUpper(firstString(value, "task_status", "status", "state", "result"))
		switch state {
		case "SUCCESS", "SUCCESSFUL", "COMPLETED", "MERGED":
			return renderJSON(value)
		case "FAILED", "ERROR", "STOPPED", "CANCELLED":
			payload, _ := json.Marshal(value)
			return fail(fmt.Errorf("pull request merge task %s failed: %s", taskID, payload))
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx(cmd).Done():
			timer.Stop()
			return fail(ctx(cmd).Err())
		case <-deadline.C:
			timer.Stop()
			return fail(fmt.Errorf("timed out waiting for merge task %s", taskID))
		case <-timer.C:
		}
	}
}
