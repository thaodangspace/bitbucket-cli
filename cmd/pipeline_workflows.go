package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/selector"
)

func pipelineRepoClient() (config.Config, *bitbucket.Client, string, error) {
	cfg, client, err := newClient()
	if err != nil {
		return cfg, nil, "", err
	}
	_, base, err := resolveRepo(cfg)
	return cfg, client, base, err
}

func pipelineSelected(cmd *cobra.Command, value string) (*bitbucket.Client, string, string, error) {
	sel, err := selector.Pipeline(value)
	if err != nil {
		return nil, "", "", err
	}
	cfg, client, err := newClient()
	if err != nil {
		return nil, "", "", err
	}
	_, base, err := resolveRepoFor(cfg, sel.Repository)
	if err != nil {
		return nil, "", "", err
	}
	id, err := resolvePipelineID(ctx(cmd), client, base, sel)
	return client, base, id, err
}

func pipelineGenericSummary(m map[string]any) string {
	for _, key := range []string{"name", "key", "uuid", "id", "status", "enabled"} {
		if v, ok := m[key]; ok {
			return fmt.Sprintf("%s: %v", key, v)
		}
	}
	return fmt.Sprint(m)
}

func emitPipelineMap(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return emitObject(raw, pipelineGenericSummary)
}

func requirePipelineYes(ok bool, operation string) error {
	if !ok {
		return fmt.Errorf("%s is destructive; pass --yes to confirm", operation)
	}
	return nil
}

func pipelineTarget(branch, tag, commit string) (map[string]any, error) {
	count := 0
	for _, v := range []string{branch, tag, commit} {
		if strings.TrimSpace(v) != "" {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("exactly one of --branch, --tag, or --commit is required")
	}
	if branch != "" {
		return map[string]any{"type": "pipeline_ref_target", "ref_type": "branch", "ref_name": branch}, nil
	}
	if tag != "" {
		return map[string]any{"type": "pipeline_ref_target", "ref_type": "tag", "ref_name": tag}, nil
	}
	return map[string]any{"type": "pipeline_commit_target", "commit": map[string]any{"hash": commit}}, nil
}

func pipelineSelector(value string) map[string]any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return map[string]any{"type": "custom", "pattern": value}
}

func pipelineVariables(values, secured []string) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(values)+len(secured))
	seen := map[string]bool{}
	for _, item := range values {
		// pflag resets a StringSlice flag to its textual [] default when
		// cobra reuses the command tree between executions.
		if item == "[]" {
			continue
		}
		key, val, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --variable %q: expected KEY=VALUE", item)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate pipeline variable key %q", key)
		}
		seen[key] = true
		result = append(result, map[string]any{"key": key, "value": val, "secured": false})
	}
	cleanedSecured := secured[:0]
	for _, key := range secured {
		if key != "[]" {
			cleanedSecured = append(cleanedSecured, key)
		}
	}
	secured = cleanedSecured
	if len(secured) == 0 {
		return result, nil
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read secured variable from stdin: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(input), "\n"), "\n")
	if len(secured) == 1 {
		lines = []string{strings.TrimSuffix(string(input), "\n")}
	}
	if len(lines) != len(secured) {
		return nil, fmt.Errorf("expected one secret value on stdin for each --secured-variable")
	}
	for i, key := range secured {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("secured variable key cannot be empty")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate pipeline variable key %q", key)
		}
		seen[key] = true
		result = append(result, map[string]any{"key": key, "value": lines[i], "secured": true})
	}
	return result, nil
}

func sanitizePipeline(value any) any {
	switch v := value.(type) {
	case []any:
		for i := range v {
			v[i] = sanitizePipeline(v[i])
		}
	case map[string]any:
		secured, _ := v["secured"].(bool)
		if secured {
			delete(v, "value")
		}
		for key, child := range v {
			if !(secured && key == "value") {
				v[key] = sanitizePipeline(child)
			}
		}
	}
	return value
}

func pipelineSteps(c context.Context, client *bitbucket.Client, base, id string) ([]json.RawMessage, error) {
	return client.Paginate(c, fmt.Sprintf("%s/pipelines/%s/steps/?pagelen=%d", base, url.PathEscape(id), bitbucket.DefaultPageLen), 0, bitbucket.DefaultMaxPages)
}

func choosePipelineStep(values []json.RawMessage, selectorValue string) (map[string]any, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("pipeline has no steps")
	}
	if selectorValue == "" {
		var m map[string]any
		err := json.Unmarshal(values[0], &m)
		return m, err
	}
	for i, raw := range values {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		if m["uuid"] == selectorValue {
			return m, nil
		}
		if m["name"] == selectorValue {
			for _, otherRaw := range values[i+1:] {
				var other map[string]any
				if json.Unmarshal(otherRaw, &other) == nil && other["name"] == selectorValue {
					return nil, fmt.Errorf("step name %q is ambiguous; use its UUID or index", selectorValue)
				}
			}
			return m, nil
		}
	}
	if n, err := strconv.Atoi(selectorValue); err == nil && n >= 1 && n <= len(values) {
		var m map[string]any
		err = json.Unmarshal(values[n-1], &m)
		return m, err
	}
	return nil, fmt.Errorf("pipeline step %q was not found", selectorValue)
}

func pipelineFinished(p map[string]any) bool {
	state, _ := p["state"].(map[string]any)
	name, _ := state["name"].(string)
	switch strings.ToUpper(name) {
	case "COMPLETED", "HALTED", "ERROR", "STOPPED":
		return true
	}
	return false
}
func pipelineResult(p map[string]any) string {
	state, _ := p["state"].(map[string]any)
	result, _ := state["result"].(map[string]any)
	name, _ := result["name"].(string)
	if name == "" {
		name, _ = state["name"].(string)
	}
	return strings.ToUpper(name)
}

func watchPipeline(cmd *cobra.Command, client *bitbucket.Client, base, id string, interval time.Duration, exitStatus bool) error {
	for {
		var p map[string]any
		if err := client.Request(ctx(cmd), fmt.Sprintf("%s/pipelines/%s", base, url.PathEscape(id)), bitbucket.RequestOptions{}, &p); err != nil {
			return fail(err)
		}
		if pipelineFinished(p) {
			if err := emitPipelineMap(sanitizePipeline(p)); err != nil {
				return err
			}
			result := pipelineResult(p)
			if exitStatus && result != "SUCCESSFUL" && result != "" {
				return fmt.Errorf("pipeline completed with result %s", result)
			}
			return nil
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx(cmd).Done():
			t.Stop()
			return ctx(cmd).Err()
		case <-t.C:
		}
	}
}

func writePipelineAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".bitbucket-cli-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pipelineRaw(cmd *cobra.Command, client *bitbucket.Client, path, outputFile string, follow bool, interval time.Duration) error {
	var previous string
	var complete []byte
	for {
		// Follow polls complete snapshots; each individual request remains
		// bounded even though the outer loop may run indefinitely.
		response, err := client.Do(ctx(cmd), path, bitbucket.RequestOptions{})
		if err != nil {
			return fail(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return fail(err)
		}
		text := string(data)
		if follow && strings.HasPrefix(text, previous) {
			data = []byte(text[len(previous):])
		}
		previous = text
		if outputFile != "" {
			if follow {
				complete = append(complete, data...)
				data = complete
			}
			if err := writePipelineAtomic(outputFile, data); err != nil {
				return fail(err)
			}
		} else if _, err := os.Stdout.Write(data); err != nil {
			return fail(err)
		}
		if !follow {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx(cmd).Done():
			timer.Stop()
			return ctx(cmd).Err()
		case <-timer.C:
		}
	}
}

func pipelineReportCases(cmd *cobra.Command, client *bitbucket.Client, path string) ([]json.RawMessage, error) {
	// Bitbucket exposes test cases directly below the step's test_reports
	// resource; there is no report UUID path component for this endpoint.
	return client.Paginate(ctx(cmd), path+"/test_cases/", 0, bitbucket.DefaultMaxPages)
}

func init() {
	var branch, tag, commit, custom string
	var variables, secured []string
	var wait bool
	runCmd := &cobra.Command{Use: "run", Short: "Start a pipeline", Long: "Start a Bitbucket pipeline. This is a write operation; run only when explicitly requested. Secured variable values are read from stdin and never accepted as command-line arguments.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		target, err := pipelineTarget(branch, tag, commit)
		if err != nil {
			return fail(err)
		}
		vars, err := pipelineVariables(variables, secured)
		if err != nil {
			return fail(err)
		}
		_, client, base, err := pipelineRepoClient()
		if err != nil {
			return fail(err)
		}
		body := map[string]any{"target": target}
		if s := pipelineSelector(custom); s != nil {
			body["selector"] = s
		}
		if len(vars) > 0 {
			body["variables"] = vars
		}
		var p map[string]any
		if err := client.Request(ctx(cmd), base+"/pipelines/", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &p); err != nil {
			return fail(err)
		}
		if wait {
			id, _ := p["uuid"].(string)
			if id == "" {
				return fail(fmt.Errorf("pipeline response has no UUID"))
			}
			return watchPipeline(cmd, client, base, id, 2*time.Second, false)
		}
		return emitPipelineMap(sanitizePipeline(p))
	}}
	runCmd.Flags().StringVar(&branch, "branch", "", "Run against a branch")
	runCmd.Flags().StringVar(&tag, "tag", "", "Run against a tag")
	runCmd.Flags().StringVar(&commit, "commit", "", "Run against a commit hash")
	runCmd.Flags().StringVar(&custom, "custom", "", "Custom pipeline selector")
	runCmd.Flags().StringSliceVar(&variables, "variable", nil, "Pipeline variable KEY=VALUE (repeatable)")
	runCmd.Flags().StringSliceVar(&secured, "secured-variable", nil, "Secured variable key; read its value from stdin (repeatable)")
	runCmd.Flags().BoolVar(&wait, "wait", false, "Wait for the pipeline to finish")
	pipelineCmd.AddCommand(runCmd)

	var stopYes bool
	stopCmd := &cobra.Command{Use: "stop <pipeline>", Short: "Stop a pipeline", Long: "Stop a running pipeline. This is a write operation; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePipelineYes(stopYes, "stopping a pipeline"); err != nil {
			return fail(err)
		}
		client, base, id, err := pipelineSelected(cmd, args[0])
		if err != nil {
			return fail(err)
		}
		if err := client.Request(ctx(cmd), fmt.Sprintf("%s/pipelines/%s/stopPipeline", base, url.PathEscape(id)), bitbucket.RequestOptions{Method: http.MethodPost}, nil); err != nil {
			return fail(err)
		}
		return emitPipelineMap(map[string]any{"uuid": id, "stopped": true})
	}}
	stopCmd.Flags().BoolVar(&stopYes, "yes", false, "Confirm stopping the pipeline")
	pipelineCmd.AddCommand(stopCmd)

	var watchInterval time.Duration
	var exitStatus bool
	watchCmd := &cobra.Command{Use: "watch <pipeline>", Short: "Watch a pipeline until it finishes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if watchInterval <= 0 {
			return fail(fmt.Errorf("--interval must be positive"))
		}
		client, base, id, err := pipelineSelected(cmd, args[0])
		if err != nil {
			return fail(err)
		}
		return watchPipeline(cmd, client, base, id, watchInterval, exitStatus)
	}}
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 2*time.Second, "Polling interval")
	watchCmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero for an unsuccessful result")
	pipelineCmd.AddCommand(watchCmd)

	var stepsLimit int
	stepsCmd := &cobra.Command{Use: "steps <pipeline>", Short: "List pipeline steps", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, id, err := pipelineSelected(cmd, args[0])
		if err != nil {
			return fail(err)
		}
		v, err := client.Paginate(ctx(cmd), fmt.Sprintf("%s/pipelines/%s/steps/?pagelen=%d", base, url.PathEscape(id), bitbucket.DefaultPageLen), stepsLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitList(v, pipelineGenericSummary, "No pipeline steps found.")
	}}
	stepsCmd.Flags().IntVar(&stepsLimit, "limit", bitbucket.DefaultLimit, "Maximum steps to return")
	pipelineCmd.AddCommand(stepsCmd)

	var follow bool
	var outputFile string
	var interval time.Duration
	logCmd := &cobra.Command{Use: "log <pipeline> [step]", Short: "Read a pipeline step log", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, id, err := pipelineSelected(cmd, args[0])
		if err != nil {
			return fail(err)
		}
		v, err := pipelineSteps(ctx(cmd), client, base, id)
		if err != nil {
			return fail(err)
		}
		s, err := choosePipelineStep(v, strings.Join(args[1:], " "))
		if err != nil {
			return fail(err)
		}
		sid, _ := s["uuid"].(string)
		return pipelineRaw(cmd, client, fmt.Sprintf("%s/pipelines/%s/steps/%s/log", base, url.PathEscape(id), url.PathEscape(sid)), outputFile, follow, interval)
	}}
	logCmd.Flags().BoolVar(&follow, "follow", false, "Poll for new log content")
	logCmd.Flags().StringVar(&outputFile, "output", "", "Write atomically to this file")
	logCmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "Polling interval")
	pipelineCmd.AddCommand(logCmd)

	var cases bool
	var reportOutput string
	reportCmd := &cobra.Command{Use: "test-report <pipeline> [step]", Short: "Read a pipeline test report", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		client, base, id, err := pipelineSelected(cmd, args[0])
		if err != nil {
			return fail(err)
		}
		v, err := pipelineSteps(ctx(cmd), client, base, id)
		if err != nil {
			return fail(err)
		}
		s, err := choosePipelineStep(v, strings.Join(args[1:], " "))
		if err != nil {
			return fail(err)
		}
		sid, _ := s["uuid"].(string)
		path := fmt.Sprintf("%s/pipelines/%s/steps/%s/test_reports", base, url.PathEscape(id), url.PathEscape(sid))
		if cases {
			values, err := pipelineReportCases(cmd, client, path)
			if err != nil {
				return fail(err)
			}
			items := make([]any, 0, len(values))
			for _, value := range values {
				var item any
				if err := json.Unmarshal(value, &item); err != nil {
					return fail(err)
				}
				items = append(items, item)
			}
			data, err := json.MarshalIndent(items, "", "  ")
			if err != nil {
				return fail(err)
			}
			data = append(data, '\n')
			if reportOutput != "" {
				if err := writePipelineAtomic(reportOutput, data); err != nil {
					return fail(err)
				}
				return nil
			}
			return writeOutput(data)
		}
		if reportOutput != "" {
			return pipelineRaw(cmd, client, path, reportOutput, false, 0)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &raw); err != nil {
			return fail(err)
		}
		return emitObject(raw, pipelineGenericSummary)
	}}
	reportCmd.Flags().BoolVar(&cases, "cases", false, "Include test cases")
	reportCmd.Flags().StringVar(&reportOutput, "output", "", "Write raw report output atomically to this file")
	pipelineCmd.AddCommand(reportCmd)
}

func schedulePath(cfg config.Config) (string, *bitbucket.Client, error) {
	_, client, base, err := pipelineRepoClient()
	return base + "/pipelines_config/schedules", client, err
}
func validPipelineCron(s string) bool { return len(strings.Fields(s)) == 5 }

func init() {
	schedule := &cobra.Command{Use: "schedule", Short: "Manage pipeline schedules"}
	pipelineCmd.AddCommand(schedule)
	list := &cobra.Command{Use: "list", Short: "List pipeline schedules", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := schedulePath(cfg)
		if err != nil {
			return fail(err)
		}
		v, err := c.Paginate(ctx(cmd), p+"/", 0, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitList(v, pipelineGenericSummary, "No pipeline schedules found.")
	}}
	schedule.AddCommand(list)
	var cron, branch, tag, commit, custom string
	var enabled bool
	create := &cobra.Command{Use: "create", Short: "Create a pipeline schedule", Long: "Create a pipeline schedule. This is a write operation; run only when explicitly requested.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !validPipelineCron(cron) {
			return fail(fmt.Errorf("--cron must contain five cron fields"))
		}
		target, err := pipelineTarget(branch, tag, commit)
		if err != nil {
			return fail(err)
		}
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := schedulePath(cfg)
		if err != nil {
			return fail(err)
		}
		body := map[string]any{"cron_pattern": cron, "enabled": enabled, "target": target}
		if s := pipelineSelector(custom); s != nil {
			body["selector"] = s
		}
		var out map[string]any
		if err := c.Request(ctx(cmd), p+"/", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &out); err != nil {
			return fail(err)
		}
		return emitPipelineMap(sanitizePipeline(out))
	}}
	create.Flags().StringVar(&cron, "cron", "", "Five-field cron expression")
	create.Flags().StringVar(&branch, "branch", "", "Schedule branch")
	create.Flags().StringVar(&tag, "tag", "", "Schedule tag")
	create.Flags().StringVar(&commit, "commit", "", "Schedule commit hash")
	create.Flags().StringVar(&custom, "custom", "", "Custom pipeline selector")
	create.Flags().BoolVar(&enabled, "enabled", true, "Enable the schedule")
	schedule.AddCommand(create)
	var editCron, editBranch, editTag, editCommit, editCustom string
	var editEnabled bool
	edit := &cobra.Command{Use: "edit <uuid>", Short: "Edit a pipeline schedule", Long: "Edit a pipeline schedule with read-modify-write. This is a write operation; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := schedulePath(cfg)
		if err != nil {
			return fail(err)
		}
		endpoint := p + "/" + url.PathEscape(args[0])
		var current map[string]any
		if err := c.Request(ctx(cmd), endpoint, bitbucket.RequestOptions{}, &current); err != nil {
			return fail(err)
		}
		body := map[string]any{}
		if cmd.Flags().Changed("cron") {
			if !validPipelineCron(editCron) {
				return fail(fmt.Errorf("--cron must contain five cron fields"))
			}
			body["cron_pattern"] = editCron
		}
		if cmd.Flags().Changed("enabled") {
			body["enabled"] = editEnabled
		}
		if editBranch != "" || editTag != "" || editCommit != "" {
			t, e := pipelineTarget(editBranch, editTag, editCommit)
			if e != nil {
				return fail(e)
			}
			body["target"] = t
		}
		if editCustom != "" {
			body["selector"] = pipelineSelector(editCustom)
		}
		if len(body) == 0 {
			return fail(fmt.Errorf("provide at least one schedule field to edit"))
		}
		for k, v := range current {
			if _, ok := body[k]; !ok && k != "uuid" && k != "links" {
				body[k] = v
			}
		}
		var out map[string]any
		if err := c.Request(ctx(cmd), endpoint, bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &out); err != nil {
			return fail(err)
		}
		return emitPipelineMap(sanitizePipeline(out))
	}}
	edit.Flags().StringVar(&editCron, "cron", "", "New cron expression")
	edit.Flags().StringVar(&editBranch, "branch", "", "New branch")
	edit.Flags().StringVar(&editTag, "tag", "", "New tag")
	edit.Flags().StringVar(&editCommit, "commit", "", "New commit")
	edit.Flags().StringVar(&editCustom, "custom", "", "New custom selector")
	edit.Flags().BoolVar(&editEnabled, "enabled", false, "Enable or disable")
	schedule.AddCommand(edit)
	var deleteYes bool
	del := &cobra.Command{Use: "delete <uuid>", Short: "Delete a pipeline schedule", Long: "Delete a pipeline schedule. This is destructive; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePipelineYes(deleteYes, "deleting a pipeline schedule"); err != nil {
			return fail(err)
		}
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := schedulePath(cfg)
		if err != nil {
			return fail(err)
		}
		if err := c.Request(ctx(cmd), p+"/"+url.PathEscape(args[0]), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return emitPipelineMap(map[string]any{"uuid": args[0], "deleted": true})
	}}
	del.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")
	schedule.AddCommand(del)
	var runLimit int
	runs := &cobra.Command{Use: "runs <uuid>", Short: "List schedule runs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := schedulePath(cfg)
		if err != nil {
			return fail(err)
		}
		v, err := c.Paginate(ctx(cmd), p+"/"+url.PathEscape(args[0])+"/executions/", runLimit, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitList(v, pipelineGenericSummary, "No schedule runs found.")
	}}
	runs.Flags().IntVar(&runLimit, "limit", bitbucket.DefaultLimit, "Maximum runs")
	schedule.AddCommand(runs)
}

func variablePath(cfg config.Config, scope, environment string) (string, *bitbucket.Client, error) {
	_, client, err := newClient()
	if err != nil {
		return "", nil, err
	}
	switch strings.ToLower(scope) {
	case "", "repository":
		_, base, e := resolveRepo(cfg)
		return base + "/pipelines-config/variables", client, e
	case "workspace":
		ref, e := config.ResolveRepoRef(config.RepoRef{}, cfg)
		if e != nil {
			return "", nil, e
		}
		return "/workspaces/" + bitbucket.EncodePathSegment(ref.Workspace) + "/pipelines-config/variables", client, nil
	case "deployment":
		if environment == "" {
			return "", nil, fmt.Errorf("deployment scope requires --environment")
		}
		_, base, e := resolveRepo(cfg)
		return base + "/deployments/" + url.PathEscape(environment) + "/variables", client, e
	}
	return "", nil, fmt.Errorf("invalid variable scope %q", scope)
}
func sanitizeRawList(values []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, raw := range values {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			v = sanitizePipeline(v)
			if b, e := json.Marshal(v); e == nil {
				out = append(out, b)
				continue
			}
		}
		out = append(out, raw)
	}
	return out
}

func init() {
	variablesCmd := &cobra.Command{Use: "variable", Short: "Manage pipeline variables"}
	pipelineCmd.AddCommand(variablesCmd)
	var scope, environment string
	list := &cobra.Command{Use: "list", Short: "List pipeline variables", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := variablePath(cfg, scope, environment)
		if err != nil {
			return fail(err)
		}
		v, err := c.Paginate(ctx(cmd), p+"/", 0, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		return emitList(sanitizeRawList(v), pipelineGenericSummary, "No pipeline variables found.")
	}}
	list.Flags().StringVar(&scope, "scope", "repository", "repository, workspace, or deployment")
	list.Flags().StringVar(&environment, "environment", "", "Deployment environment UUID")
	variablesCmd.AddCommand(list)
	var value, valueFile string
	var valueStdin, secured, create, update bool
	set := &cobra.Command{Use: "set <key>", Short: "Create or update a pipeline variable", Long: "Set a pipeline variable. This is a write operation; secured values must come from a file or stdin.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		sources := 0
		if cmd.Flags().Changed("value") {
			sources++
		}
		if valueFile != "" {
			sources++
		}
		if valueStdin {
			sources++
		}
		if sources != 1 {
			return fail(fmt.Errorf("provide exactly one of --value, --value-file, or --value-stdin"))
		}
		if secured && cmd.Flags().Changed("value") {
			return fail(fmt.Errorf("secured values cannot be passed with --value"))
		}
		var data []byte
		var err error
		if valueFile != "" {
			data, err = os.ReadFile(valueFile)
		} else if valueStdin {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data = []byte(value)
		}
		if err != nil {
			return fail(err)
		}
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := variablePath(cfg, scope, environment)
		if err != nil {
			return fail(err)
		}
		existing, err := c.Paginate(ctx(cmd), p+"/", 0, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		matches := []map[string]any{}
		for _, raw := range existing {
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil && m["key"] == args[0] {
				matches = append(matches, m)
			}
		}
		if len(matches) > 1 {
			return fail(fmt.Errorf("variable key %q is ambiguous", args[0]))
		}
		if create && len(matches) > 0 {
			return fail(fmt.Errorf("variable already exists"))
		}
		if update && len(matches) == 0 {
			return fail(fmt.Errorf("variable does not exist"))
		}
		body := map[string]any{"key": args[0], "value": string(data), "secured": secured}
		endpoint := p + "/"
		method := http.MethodPost
		if len(matches) == 1 {
			endpoint = p + "/" + url.PathEscape(fmt.Sprint(matches[0]["uuid"]))
			method = http.MethodPut
		}
		var out map[string]any
		if err := c.Request(ctx(cmd), endpoint, bitbucket.RequestOptions{Method: method, Body: body}, &out); err != nil {
			return fail(err)
		}
		return emitPipelineMap(sanitizePipeline(out))
	}}
	set.Flags().StringVar(&value, "value", "", "Variable value")
	set.Flags().StringVar(&valueFile, "value-file", "", "Read value from a file")
	set.Flags().BoolVar(&valueStdin, "value-stdin", false, "Read value from stdin")
	set.Flags().BoolVar(&secured, "secured", false, "Mark variable secured")
	set.Flags().BoolVar(&create, "create", false, "Require creation")
	set.Flags().BoolVar(&update, "update", false, "Require update")
	set.Flags().StringVar(&scope, "scope", "repository", "Variable scope")
	set.Flags().StringVar(&environment, "environment", "", "Deployment environment UUID")
	variablesCmd.AddCommand(set)
	var deleteYes bool
	del := &cobra.Command{Use: "delete <key-or-uuid>", Short: "Delete a pipeline variable", Long: "Delete a pipeline variable. This is destructive; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := requirePipelineYes(deleteYes, "deleting a pipeline variable"); err != nil {
			return fail(err)
		}
		cfg, _, err := newClient()
		if err != nil {
			return fail(err)
		}
		p, c, err := variablePath(cfg, scope, environment)
		if err != nil {
			return fail(err)
		}
		v, err := c.Paginate(ctx(cmd), p+"/", 0, bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		id := ""
		for _, raw := range v {
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil && (m["key"] == args[0] || m["uuid"] == args[0]) {
				if id != "" {
					return fail(fmt.Errorf("variable selector is ambiguous"))
				}
				id = fmt.Sprint(m["uuid"])
			}
		}
		if id == "" {
			return fail(fmt.Errorf("variable %q was not found", args[0]))
		}
		if err := c.Request(ctx(cmd), p+"/"+url.PathEscape(id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return emitPipelineMap(map[string]any{"uuid": id, "deleted": true})
	}}
	del.Flags().BoolVar(&deleteYes, "yes", false, "Confirm deletion")
	del.Flags().StringVar(&scope, "scope", "repository", "Variable scope")
	del.Flags().StringVar(&environment, "environment", "", "Deployment environment UUID")
	variablesCmd.AddCommand(del)
}

func cachePath(cfg config.Config) (string, *bitbucket.Client, error) {
	_, c, b, e := pipelineRepoClient()
	return b + "/pipelines-config/caches", c, e
}
func runnerPath(cfg config.Config, workspace bool) (string, *bitbucket.Client, error) {
	_, c, e := newClient()
	if e != nil {
		return "", nil, e
	}
	if workspace {
		r, e := config.ResolveRepoRef(config.RepoRef{}, cfg)
		if e != nil {
			return "", nil, e
		}
		return "/workspaces/" + bitbucket.EncodePathSegment(r.Workspace) + "/pipelines-config/runners", c, nil
	}
	_, b, e := resolveRepo(cfg)
	return b + "/pipelines-config/runners", c, e
}

func init() {
	cache := &cobra.Command{Use: "cache", Short: "Manage pipeline caches"}
	pipelineCmd.AddCommand(cache)
	var limit int
	list := &cobra.Command{Use: "list", Short: "List pipeline caches", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := cachePath(cfg)
		if e != nil {
			return fail(e)
		}
		v, e := c.Paginate(ctx(cmd), p+"/", limit, bitbucket.DefaultMaxPages)
		if e != nil {
			return fail(e)
		}
		return emitList(v, pipelineGenericSummary, "No pipeline caches found.")
	}}
	list.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum caches")
	cache.AddCommand(list)
	var yes, all bool
	del := &cobra.Command{Use: "delete <uuid|all>", Short: "Delete a pipeline cache", Long: "Delete a pipeline cache. This is destructive; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if e := requirePipelineYes(yes, "deleting a pipeline cache"); e != nil {
			return fail(e)
		}
		if args[0] == "all" && !all {
			return fail(fmt.Errorf("deleting all caches requires --yes --all"))
		}
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := cachePath(cfg)
		if e != nil {
			return fail(e)
		}
		if args[0] == "all" {
			v, e := c.Paginate(ctx(cmd), p+"/", 0, bitbucket.DefaultMaxPages)
			if e != nil {
				return fail(e)
			}
			for _, raw := range v {
				var m map[string]any
				if json.Unmarshal(raw, &m) == nil {
					if id := fmt.Sprint(m["uuid"]); id != "" {
						if e = c.Request(ctx(cmd), p+"/"+url.PathEscape(id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); e != nil {
							return fail(e)
						}
					}
				}
			}
			return emitPipelineMap(map[string]any{"deleted": len(v), "all": true})
		}
		if e = c.Request(ctx(cmd), p+"/"+url.PathEscape(args[0]), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); e != nil {
			return fail(e)
		}
		return emitPipelineMap(map[string]any{"uuid": args[0], "deleted": true})
	}}
	del.Flags().BoolVar(&yes, "yes", false, "Confirm deletion")
	del.Flags().BoolVar(&all, "all", false, "Confirm all-cache deletion")
	cache.AddCommand(del)

	runner := &cobra.Command{Use: "runner", Short: "Manage pipeline runners"}
	pipelineCmd.AddCommand(runner)
	var workspace bool
	var runnerLimit int
	listR := &cobra.Command{Use: "list", Short: "List pipeline runners", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := runnerPath(cfg, workspace)
		if e != nil {
			return fail(e)
		}
		v, e := c.Paginate(ctx(cmd), p+"/", runnerLimit, bitbucket.DefaultMaxPages)
		if e != nil {
			return fail(e)
		}
		return emitList(sanitizeRawList(v), pipelineGenericSummary, "No pipeline runners found.")
	}}
	listR.Flags().BoolVar(&workspace, "workspace", false, "Use workspace runners")
	listR.Flags().IntVar(&runnerLimit, "limit", bitbucket.DefaultLimit, "Maximum runners")
	runner.AddCommand(listR)
	var viewWorkspace bool
	view := &cobra.Command{Use: "view <uuid>", Short: "View a pipeline runner", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := runnerPath(cfg, viewWorkspace)
		if e != nil {
			return fail(e)
		}
		var out map[string]any
		if e = c.Request(ctx(cmd), p+"/"+url.PathEscape(args[0]), bitbucket.RequestOptions{}, &out); e != nil {
			return fail(e)
		}
		return emitPipelineMap(sanitizePipeline(out))
	}}
	view.Flags().BoolVar(&viewWorkspace, "workspace", false, "Use workspace runner")
	runner.AddCommand(view)
	var createWorkspace bool
	var name string
	var labels []string
	create := &cobra.Command{Use: "create", Short: "Create a pipeline runner", Long: "Create a pipeline runner. Registration tokens are sensitive and shown once.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := runnerPath(cfg, createWorkspace)
		if e != nil {
			return fail(e)
		}
		body := map[string]any{}
		if name != "" {
			body["name"] = name
		}
		if len(labels) > 0 {
			body["labels"] = labels
		}
		var out map[string]any
		if e = c.Request(ctx(cmd), p+"/", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &out); e != nil {
			return fail(e)
		}
		return emitPipelineMap(out)
	}}
	create.Flags().BoolVar(&createWorkspace, "workspace", false, "Create workspace runner")
	create.Flags().StringVar(&name, "name", "", "Runner name")
	create.Flags().StringSliceVar(&labels, "labels", nil, "Runner labels")
	runner.AddCommand(create)
	var editWorkspace bool
	var editName string
	var editLabels []string
	var editEnabled bool
	edit := &cobra.Command{Use: "edit <uuid>", Short: "Edit a pipeline runner", Long: "Edit a pipeline runner. This is a write operation; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := runnerPath(cfg, editWorkspace)
		if e != nil {
			return fail(e)
		}
		ep := p + "/" + url.PathEscape(args[0])
		var cur map[string]any
		if e = c.Request(ctx(cmd), ep, bitbucket.RequestOptions{}, &cur); e != nil {
			return fail(e)
		}
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["name"] = editName
		}
		if cmd.Flags().Changed("labels") {
			body["labels"] = editLabels
		}
		if cmd.Flags().Changed("enabled") {
			body["enabled"] = editEnabled
		}
		if len(body) == 0 {
			return fail(fmt.Errorf("provide a runner field to edit"))
		}
		for k, v := range cur {
			if _, ok := body[k]; !ok && k != "uuid" && k != "links" {
				body[k] = v
			}
		}
		var out map[string]any
		if e = c.Request(ctx(cmd), ep, bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &out); e != nil {
			return fail(e)
		}
		return emitPipelineMap(sanitizePipeline(out))
	}}
	edit.Flags().BoolVar(&editWorkspace, "workspace", false, "Edit workspace runner")
	edit.Flags().StringVar(&editName, "name", "", "Runner name")
	edit.Flags().StringSliceVar(&editLabels, "labels", nil, "Runner labels")
	edit.Flags().BoolVar(&editEnabled, "enabled", false, "Enable or disable runner")
	runner.AddCommand(edit)
	var deleteWorkspace, runnerYes bool
	delR := &cobra.Command{Use: "delete <uuid>", Short: "Delete a pipeline runner", Long: "Delete a pipeline runner. This is destructive; run only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if e := requirePipelineYes(runnerYes, "deleting a pipeline runner"); e != nil {
			return fail(e)
		}
		cfg, _, e := newClient()
		if e != nil {
			return fail(e)
		}
		p, c, e := runnerPath(cfg, deleteWorkspace)
		if e != nil {
			return fail(e)
		}
		if e = c.Request(ctx(cmd), p+"/"+url.PathEscape(args[0]), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); e != nil {
			return fail(e)
		}
		return emitPipelineMap(map[string]any{"uuid": args[0], "deleted": true})
	}}
	delR.Flags().BoolVar(&deleteWorkspace, "workspace", false, "Delete workspace runner")
	delR.Flags().BoolVar(&runnerYes, "yes", false, "Confirm deletion")
	runner.AddCommand(delR)

	configCmd := &cobra.Command{Use: "config", Short: "Manage pipeline configuration"}
	pipelineCmd.AddCommand(configCmd)
	viewC := &cobra.Command{Use: "view", Short: "View pipeline configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, c, b, e := pipelineRepoClient()
		if e != nil {
			return fail(e)
		}
		var out map[string]any
		if e = c.Request(ctx(cmd), b+"/pipelines-config", bitbucket.RequestOptions{}, &out); e != nil {
			return fail(e)
		}
		return emitPipelineMap(out)
	}}
	configCmd.AddCommand(viewC)
	enable := &cobra.Command{Use: "enable", Short: "Enable Pipelines", Long: "Enable Pipelines. This is a write operation; run only when explicitly requested.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return setPipelineConfig(cmd, true) }}
	var disableYes bool
	disable := &cobra.Command{Use: "disable", Short: "Disable Pipelines", Long: "Disable Pipelines. This is destructive; run only when explicitly requested.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if e := requirePipelineYes(disableYes, "disabling Pipelines"); e != nil {
			return fail(e)
		}
		return setPipelineConfig(cmd, false)
	}}
	disable.Flags().BoolVar(&disableYes, "yes", false, "Confirm disabling Pipelines")
	configCmd.AddCommand(enable, disable)
}
func setPipelineConfig(cmd *cobra.Command, enabled bool) error {
	_, c, b, e := pipelineRepoClient()
	if e != nil {
		return fail(e)
	}
	var out map[string]any
	if e = c.Request(ctx(cmd), b+"/pipelines-config", bitbucket.RequestOptions{Method: http.MethodPut, Body: map[string]any{"enabled": enabled}}, &out); e != nil {
		return fail(e)
	}
	return emitPipelineMap(out)
}
