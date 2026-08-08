package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
	"github.com/thaodangspace/bitbucket-cli/selector"

	"github.com/spf13/cobra"
)

// pipelineCmd is the parent for pipeline subcommands.
var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Pipeline commands",
}

func init() {
	var (
		listState      string
		listLimit      int
		pipelineGetWeb bool
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List pipeline runs for a repository",
		Long:  "List recent pipeline runs for the resolved repository, newest first.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			_, base, err := resolveRepo(cfg)
			if err != nil {
				return fail(err)
			}

			q := url.Values{
				"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)},
				"sort":    {"-created_on"},
			}
			if listState != "" {
				// Normalize to uppercase — Bitbucket API expects uppercase state values.
				q.Set("state", strings.ToUpper(listState))
			}
			path := fmt.Sprintf("%s/pipelines/?%s", base, q.Encode())

			values, err := client.Paginate(ctx(cmd), path, listLimit, bitbucket.DefaultMaxPages)
			if err != nil {
				return fail(err)
			}
			if err := emitListFields(values, output.PipelineFields, output.PipelineSummary, "No pipelines found."); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&listState, "state", "", "Filter by state: PENDING, IN_PROGRESS, COMPLETED, PAUSED, HALTED, ERROR")
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum pipelines to return")

	getCmd := &cobra.Command{
		Use:   "get <uuid>",
		Short: "Get a single pipeline run by UUID with its steps",
		Long:  "Fetch a pipeline run by its UUID (e.g. {abc123-...}) and include its steps in the output.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, selectorErr := selector.Pipeline(args[0])
			if selectorErr != nil {
				return fail(selectorErr)
			}

			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, base, err := resolveRepoFor(cfg, selected.Repository)
			if err != nil {
				return fail(err)
			}
			if pipelineGetWeb && selected.BuildNumber != nil {
				return fail(fmt.Errorf("--web requires a pipeline UUID or URL, not a build number"))
			}
			if pipelineGetWeb {
				return openWeb(buildPipelineURL(ref.Workspace, ref.RepoSlug, selected.UUID))
			}
			uuid, err := resolvePipelineID(ctx(cmd), client, base, selected)
			if err != nil {
				return fail(err)
			}

			// 1. Fetch pipeline.
			pipelinePath := fmt.Sprintf("%s/pipelines/%s", base, url.PathEscape(uuid))
			var pipelineRaw json.RawMessage
			if err := client.Request(ctx(cmd), pipelinePath, bitbucket.RequestOptions{}, &pipelineRaw); err != nil {
				return fail(err)
			}

			// 2. Fetch steps.
			stepsPath := fmt.Sprintf("%s/pipelines/%s/steps/?pagelen=%d", base, url.PathEscape(uuid), bitbucket.DefaultPageLen)
			stepsRaw, err := client.Paginate(ctx(cmd), stepsPath, 200, bitbucket.DefaultMaxPages)
			if err != nil {
				// Keep machine-readable output valid: warnings are part of the
				// result rather than an unstructured stderr line.
				return emitPipelineGetWithWarnings(pipelineRaw, nil, []map[string]any{{
					"operation": "fetch_steps",
					"error":     warningError(err),
				}})
			}

			return emitPipelineGet(pipelineRaw, stepsRaw)
		},
	}
	getCmd.Flags().BoolVar(&pipelineGetWeb, "web", false, "Open the pipeline in a browser")

	pipelineCmd.AddCommand(listCmd, getCmd)
	rootCmd.AddCommand(pipelineCmd)
}

// emitPipelineGet renders a pipeline with its steps. In JSON mode (default)
// it merges steps into the pipeline object; in --pretty mode it renders a
// multi-section text summary.
func resolvePipelineID(c context.Context, client *bitbucket.Client, base string, selected selector.PipelineSelector) (string, error) {
	if selected.UUID != "" {
		return selected.UUID, nil
	}
	if selected.BuildNumber == nil {
		return "", fmt.Errorf("pipeline selector has no UUID or build number")
	}
	q := url.Values{"q": {fmt.Sprintf("build_number=%d", *selected.BuildNumber)}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
	values, err := client.Paginate(c, fmt.Sprintf("%s/pipelines/?%s", base, q.Encode()), 1, bitbucket.DefaultMaxPages)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", fmt.Errorf("pipeline build number %d was not found", *selected.BuildNumber)
	}
	var pipeline map[string]any
	if err := json.Unmarshal(values[0], &pipeline); err != nil {
		return "", err
	}
	uuid, _ := pipeline["uuid"].(string)
	if uuid == "" {
		return "", fmt.Errorf("pipeline build number %d has no UUID", *selected.BuildNumber)
	}
	return uuid, nil
}

func warningError(err error) map[string]any {
	if httpErr, ok := err.(*bitbucket.HTTPError); ok {
		return map[string]any{
			"method": httpErr.Method, "url": httpErr.URL, "status": httpErr.Status,
			"statusText": httpErr.StatusText, "excerpt": httpErr.Excerpt,
		}
	}
	return map[string]any{"message": err.Error()}
}

func emitPipelineGet(pipelineRaw json.RawMessage, stepsRaw []json.RawMessage) error {
	return emitPipelineGetWithWarnings(pipelineRaw, stepsRaw, nil)
}

func emitPipelineGetWithWarnings(pipelineRaw json.RawMessage, stepsRaw []json.RawMessage, warnings []map[string]any) error {
	// Decode pipeline for both modes.
	var pipeline map[string]any
	if err := json.Unmarshal(pipelineRaw, &pipeline); err != nil {
		return err
	}

	// Decode steps.
	steps := make([]map[string]any, 0)
	for _, s := range stepsRaw {
		var step map[string]any
		if err := json.Unmarshal(s, &step); err != nil {
			return err
		}
		steps = append(steps, step)
	}

	// Include steps in the structured value so every output mode and transform
	// sees the same response.
	pipeline["steps"] = steps
	if len(warnings) > 0 {
		pipeline["warnings"] = warnings
	}
	return renderValue(pipeline, output.PipelineFields,
		func(m map[string]any) string {
			projectedSteps := steps
			if raw, ok := m["steps"].([]any); ok {
				projectedSteps = make([]map[string]any, 0, len(raw))
				for _, item := range raw {
					if step, ok := item.(map[string]any); ok {
						projectedSteps = append(projectedSteps, step)
					}
				}
			}
			return output.PipelineGetSummary(m, projectedSteps)
		}, false, "")
}
