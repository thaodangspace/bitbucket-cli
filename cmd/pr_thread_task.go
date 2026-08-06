package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
)

func init() {
	threadCmd := &cobra.Command{Use: "thread", Short: "Resolve or reopen pull request comment threads"}
	for _, spec := range []struct {
		name, use, action string
		resolved          bool
	}{
		{"resolve", "resolve <pr> <comment-id>", "resolve", true},
		{"reopen", "reopen <pr> <comment-id>", "reopen", false},
	} {
		s := spec
		cmd := &cobra.Command{
			Use: s.use, Short: strings.Title(s.name) + " a pull request comment thread",
			Long: "Change a pull request comment thread state. This is a write operation; use only when explicitly requested.",
			Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				selected, err := parsePullRequestSelector(args[0])
				if err != nil {
					return fail(err)
				}
				cid, err := parsePositiveID("comment id", args[1])
				if err != nil {
					return fail(err)
				}
				prctx, err := newPRContext(&selected)
				if err != nil {
					return fail(err)
				}
				path := fmt.Sprintf("%s/pullrequests/%d/comments/%d/resolve", prctx.base, selected.ID, cid)
				method := http.MethodPost
				if !s.resolved {
					method = http.MethodDelete
				}
				if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: method}, nil); err != nil {
					return fail(err)
				}
				return renderJSON(map[string]any{"id": cid, "resolved": s.resolved})
			},
		}
		threadCmd.AddCommand(cmd)
	}
	prCmd.AddCommand(threadCmd)

	var taskLimit int
	taskList := &cobra.Command{
		Use: "list <pr>", Short: "List pull request tasks", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			state, _ := cmd.Flags().GetString("state")
			state = strings.ToUpper(strings.TrimSpace(state))
			if state != "" && state != "OPEN" && state != "RESOLVED" {
				return fail(fmt.Errorf("invalid task state %q (use OPEN or RESOLVED)", state))
			}
			prctx, err := newPRContext(&selected)
			if err != nil {
				return fail(err)
			}
			q := url.Values{"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
			if state != "" {
				q.Set("q", fmt.Sprintf("state=%q", state))
			}
			values, err := prctx.client.Paginate(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/tasks?%s", prctx.base, selected.ID, q.Encode()), taskLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			return emitListFields(values, output.TaskFields, output.TaskSummary, "No tasks found.")
		},
	}
	taskList.Flags().IntVar(&taskLimit, "limit", bitbucket.DefaultLimit, "Maximum tasks to return")
	taskList.Flags().String("state", "", "Filter by task state: OPEN or RESOLVED")

	var taskBody, taskBodyFile, taskComment string
	taskCreate := &cobra.Command{
		Use: "create <pr>", Short: "Create a pull request task",
		Long: "Create a task on a pull request, optionally associated with a comment. This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			body, provided, err := readBody(taskBody, taskBodyFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			if !provided || strings.TrimSpace(body) == "" {
				return fail(fmt.Errorf("provide a non-empty --body or --body-file"))
			}
			prctx, err := newPRContext(&selected)
			if err != nil {
				return fail(err)
			}
			payload := map[string]any{"content": map[string]any{"raw": body}}
			if taskComment != "" {
				cid, perr := parsePositiveID("comment id", taskComment)
				if perr != nil {
					return fail(perr)
				}
				var ignored map[string]any
				commentPath := fmt.Sprintf("%s/pullrequests/%d/comments/%d", prctx.base, selected.ID, cid)
				if err := prctx.client.Request(ctx(cmd), commentPath, bitbucket.RequestOptions{}, &ignored); err != nil {
					return fail(fmt.Errorf("validate task comment #%d belongs to pull request #%d: %w", cid, selected.ID, err))
				}
				payload["comment"] = map[string]any{"id": cid}
			}
			var raw json.RawMessage
			path := fmt.Sprintf("%s/pullrequests/%d/tasks", prctx.base, selected.ID)
			if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPost, Body: payload}, &raw); err != nil {
				return fail(err)
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return fail(err)
			}
			return renderJSON(value)
		},
	}
	taskCreate.Flags().StringVar(&taskBody, "body", "", "Task body")
	taskCreate.Flags().StringVar(&taskBodyFile, "body-file", "", "Read task body from a file (- for stdin)")
	taskCreate.Flags().StringVar(&taskComment, "comment", "", "Associate the task with a comment ID")

	var taskUpdateBody, taskUpdateFile, taskUpdateState string
	taskUpdate := &cobra.Command{
		Use: "update <pr> <task-id>", Short: "Update a pull request task",
		Long: "Update selected fields of a pull request task while preserving unspecified fields. This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			tid, err := parsePositiveID("task id", args[1])
			if err != nil {
				return fail(err)
			}
			body, bodyProvided, err := readBody(taskUpdateBody, taskUpdateFile, os.Stdin)
			if err != nil {
				return fail(err)
			}
			state := strings.ToUpper(strings.TrimSpace(taskUpdateState))
			stateProvided := cmd.Flags().Changed("state")
			if !bodyProvided && !stateProvided {
				return fail(fmt.Errorf("provide --body/--body-file and/or --state"))
			}
			if stateProvided && state != "OPEN" && state != "RESOLVED" {
				return fail(fmt.Errorf("invalid task state %q (use OPEN or RESOLVED)", taskUpdateState))
			}
			prctx, err := newPRContext(&selected)
			if err != nil {
				return fail(err)
			}
			path := fmt.Sprintf("%s/pullrequests/%d/tasks/%d", prctx.base, selected.ID, tid)
			var current map[string]any
			if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &current); err != nil {
				return fail(err)
			}
			payload := map[string]any{}
			if bodyProvided {
				payload["content"] = map[string]any{"raw": body}
			} else if content, ok := current["content"]; ok {
				payload["content"] = content
			}
			if stateProvided {
				payload["state"] = state
			} else if old, ok := current["state"]; ok {
				payload["state"] = old
			}
			var raw json.RawMessage
			if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPut, Body: payload}, &raw); err != nil {
				return fail(err)
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return fail(err)
			}
			return renderJSON(value)
		},
	}
	taskUpdate.Flags().StringVar(&taskUpdateBody, "body", "", "Replacement task body")
	taskUpdate.Flags().StringVar(&taskUpdateFile, "body-file", "", "Read replacement task body from a file (- for stdin)")
	taskUpdate.Flags().StringVar(&taskUpdateState, "state", "", "Task state: OPEN or RESOLVED")

	var taskDeleteYes bool
	taskDelete := &cobra.Command{
		Use: "delete <pr> <task-id>", Short: "Delete a pull request task", Long: "Delete a pull request task. This is a write operation and requires --yes in non-interactive mode.", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			id, err := parsePositiveID("task id", args[1])
			if err != nil {
				return fail(err)
			}
			if !taskDeleteYes {
				return fail(fmt.Errorf("refusing to delete task without --yes"))
			}
			prctx, err := newPRContext(&selected)
			if err != nil {
				return fail(err)
			}
			path := fmt.Sprintf("%s/pullrequests/%d/tasks/%d", prctx.base, selected.ID, id)
			if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
				return fail(err)
			}
			return renderJSON(map[string]any{"deleted": true, "id": id})
		},
	}
	taskDelete.Flags().BoolVar(&taskDeleteYes, "yes", false, "Confirm deletion")

	taskCmd := &cobra.Command{Use: "task", Short: "Manage pull request tasks"}
	taskCmd.AddCommand(taskList, taskCreate, taskUpdate, taskDelete)
	prCmd.AddCommand(taskCmd)
}
