package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
	"github.com/thaodangspace/bitbucket-cli/selector"

	"github.com/spf13/cobra"
)

// testTransport, when non-nil, is injected into every client. Tests set this
// to a stub RoundTripper; it is always nil in production.
var testTransport http.RoundTripper

// envMap snapshots the process environment into a map for config loading.
func envMap() map[string]string {
	env := os.Environ()
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// loadConfig resolves config from the environment, the YAML config file, and
// the local git remote.
func loadConfig() (config.Config, error) {
	env := envMap()
	return config.LoadConfig(env, "", config.DefaultConfigPath(env))
}

// newClient loads config and builds a Bitbucket client.
func newClient() (config.Config, *bitbucket.Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return config.Config{}, nil, err
	}
	var opts []bitbucket.Option
	if testTransport != nil {
		opts = append(opts, bitbucket.WithHTTPClient(&http.Client{Transport: testTransport}))
	}
	return cfg, bitbucket.NewClient(cfg.Auth, opts...), nil
}

// resolveRepo turns the persistent --workspace/--repo flags plus config
// defaults into a concrete repo reference and its API base path.
func resolveRepo(cfg config.Config) (config.ResolvedRepoRef, string, error) {
	return resolveRepoFor(cfg, nil)
}

func resolveRepoFor(cfg config.Config, selected *selector.Repository) (config.ResolvedRepoRef, string, error) {
	if selected != nil {
		if strings.TrimSpace(flagRepository) != "" || strings.TrimSpace(flagWorkspace) != "" || strings.TrimSpace(flagRepo) != "" {
			ref, _, err := resolveRepo(cfg)
			if err != nil {
				return config.ResolvedRepoRef{}, "", err
			}
			if ref.Workspace != selected.Workspace || ref.RepoSlug != selected.Repo {
				return config.ResolvedRepoRef{}, "", fmt.Errorf("resource URL targets %s/%s, which conflicts with the selected repository %s/%s", selected.Workspace, selected.Repo, ref.Workspace, ref.RepoSlug)
			}
		}
		return config.ResolvedRepoRef{Workspace: selected.Workspace, RepoSlug: selected.Repo}, fmt.Sprintf("/repositories/%s/%s", bitbucket.EncodePathSegment(selected.Workspace), bitbucket.EncodePathSegment(selected.Repo)), nil
	}
	if strings.TrimSpace(flagRepository) != "" {
		if strings.TrimSpace(flagWorkspace) != "" || strings.TrimSpace(flagRepo) != "" {
			return config.ResolvedRepoRef{}, "", fmt.Errorf("--repository cannot be combined with --workspace or --repo")
		}
		ref, err := parseRepositorySelector(flagRepository)
		if err != nil {
			return config.ResolvedRepoRef{}, "", err
		}
		return ref, fmt.Sprintf("/repositories/%s/%s", bitbucket.EncodePathSegment(ref.Workspace), bitbucket.EncodePathSegment(ref.RepoSlug)), nil
	}
	ref, err := config.ResolveRepoRef(config.RepoRef{Workspace: flagWorkspace, RepoSlug: flagRepo}, cfg)
	if err != nil {
		return config.ResolvedRepoRef{}, "", err
	}
	base := fmt.Sprintf("/repositories/%s/%s",
		bitbucket.EncodePathSegment(ref.Workspace),
		bitbucket.EncodePathSegment(ref.RepoSlug))
	return ref, base, nil
}

// fail renders a structured error to stderr and returns it so Execute exits 1.
func fail(err error) error {
	output.WriteError(os.Stderr, err)
	return err
}

// ctx returns the command's context (carries cancellation on Ctrl-C).
func ctx(cmd *cobra.Command) context.Context {
	return cmd.Context()
}

// parseID parses a positional pull request id argument.
func parseRepositorySelector(value string) (config.ResolvedRepoRef, error) {
	ref, err := selector.RepositorySelector(value)
	if err != nil {
		return config.ResolvedRepoRef{}, err
	}
	return config.ResolvedRepoRef{Workspace: ref.Workspace, RepoSlug: ref.Repo}, nil
}

func parseID(arg string) (int, error) {
	selected, err := selector.PullRequest(arg)
	if err == nil {
		return selected.ID, nil
	}
	return parsePositiveID("pull request id", arg)
}

func parsePullRequestSelector(arg string) (selector.PullRequestSelector, error) {
	selected, err := selector.PullRequest(arg)
	if err != nil {
		return selector.PullRequestSelector{}, err
	}
	return selected, nil
}

func currentGitBranch() (string, error) {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return "", fmt.Errorf("resolve current git branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", fmt.Errorf("could not determine current git branch")
	}
	return branch, nil
}

func parsePositiveID(label, arg string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive integer", label, arg)
	}
	return id, nil
}

// emitObject preserves the original API for command code that does not need
// field metadata. Wrapped commands should use emitObjectFields.
func emitObject(raw json.RawMessage, summary func(map[string]any) string) error {
	return emitObjectFields(raw, nil, summary)
}

func emitObjectFields(raw json.RawMessage, fields output.FieldSet, summary func(map[string]any) string) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return renderValue(value, fields, summary, false, "")
}

// emitList renders a list of raw JSON objects using the shared output
// contract. --json is a documented projection; --jq and --template transform
// that projection (or the complete response when no projection is requested).
func emitList(values []json.RawMessage, summary func(map[string]any) string, emptyMsg string) error {
	return emitListFields(values, nil, summary, emptyMsg)
}

func emitListFields(values []json.RawMessage, fields output.FieldSet, summary func(map[string]any) string, emptyMsg string) error {
	items := make([]any, 0, len(values))
	for _, raw := range values {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		items = append(items, value)
	}
	return renderValue(items, fields, summary, true, emptyMsg)
}

func renderValue(value any, fields output.FieldSet, summary func(map[string]any) string, list bool, emptyMsg string) error {
	if flagJSON != "" && fields == nil {
		return fmt.Errorf("--json is not supported for this command")
	}
	if flagJSON != "" {
		projected, err := fields.Project(value, splitFields(flagJSON))
		if err != nil {
			return err
		}
		value = projected
	}
	mode, err := outputMode()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if flagJQ != "" {
		if err := emitJQValueTo(&buf, value, flagJQ); err != nil {
			return err
		}
	} else if flagTemplate != "" {
		if err := emitTemplateValueTo(&buf, value, flagTemplate); err != nil {
			return err
		}
	} else {
		switch mode {
		case "table":
			if list {
				items, _ := value.([]any)
				lines := make([]string, 0, len(items))
				for _, item := range items {
					m, ok := item.(map[string]any)
					if !ok {
						return fmt.Errorf("table output requires object values")
					}
					lines = append(lines, summary(m))
				}
				if err := output.RenderLines(&buf, lines, emptyMsg); err != nil {
					return err
				}
			} else {
				m, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("table output requires an object")
				}
				if _, err := fmt.Fprintln(&buf, summary(m)); err != nil {
					return err
				}
			}
		case "json":
			if err := output.RenderJSON(&buf, value); err != nil {
				return err
			}
		case "yaml":
			if err := output.RenderYAML(&buf, value); err != nil {
				return err
			}
		case "raw":
			if err := output.RenderRaw(&buf, value); err != nil {
				return err
			}
		}
	}
	data := buf.Bytes()
	if mode == "table" && flagJQ == "" && flagTemplate == "" {
		data = colorizeTable(data)
	}
	return writeOutput(data)
}

func writeOutput(data []byte) error {
	pager := strings.ToLower(flagPager)
	if flagNoPager {
		pager = "never"
	}
	if pager == "always" || (pager == "auto" && stdoutIsTTY()) {
		command := strings.TrimSpace(os.Getenv("PAGER"))
		parts := strings.Fields(command)
		if len(parts) == 0 {
			parts = []string{"less", "-FRX"}
		}
		p := exec.Command(parts[0], parts[1:]...)
		p.Stdin = bytes.NewReader(data)
		p.Stdout, p.Stderr = os.Stdout, os.Stderr
		return p.Run()
	}
	_, err := os.Stdout.Write(data)
	return err
}

func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func colorizeTable(data []byte) []byte {
	color := strings.ToLower(flagColor)
	if color == "never" || (color == "auto" && !stdoutIsTTY()) {
		return data
	}
	return append(append([]byte("\033[36m"), data...), []byte("\033[0m")...)
}

func splitFields(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validateOutputFlags() error {
	mode := strings.ToLower(strings.TrimSpace(flagFormat))
	if mode == "" {
		mode = "json"
	}
	if !contains([]string{"json", "table", "yaml", "raw"}, mode) {
		return fmt.Errorf("unsupported output format %q (use json, table, yaml, or raw)", mode)
	}
	if !contains([]string{"auto", "always", "never"}, strings.ToLower(flagColor)) {
		return fmt.Errorf("invalid --color %q (use auto, always, or never)", flagColor)
	}
	pager := strings.ToLower(flagPager)
	if flagNoPager {
		pager = "never"
	}
	if !contains([]string{"auto", "always", "never"}, pager) {
		return fmt.Errorf("invalid --pager %q (use auto, always, or never)", flagPager)
	}
	if flagJQ != "" && flagTemplate != "" {
		return fmt.Errorf("--jq and --template are mutually exclusive")
	}
	return nil
}

func outputMode() (string, error) {
	if err := validateOutputFlags(); err != nil {
		return "", err
	}
	mode := strings.ToLower(strings.TrimSpace(flagFormat))
	if mode == "" {
		mode = "json"
	}
	if flagPretty && !rootCmd.PersistentFlags().Lookup("format").Changed {
		mode = "table"
	}
	return mode, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func emitJQValue(value any, expr string) error { return emitJQValueTo(os.Stdout, value, expr) }

func emitJQValueTo(w io.Writer, value any, expr string) error {
	results, err := jqEvaluate(expr, value)
	if err != nil {
		return fmt.Errorf("jq: %w", err)
	}
	enc := json.NewEncoder(w)
	for _, result := range results {
		if err := enc.Encode(result); err != nil {
			return err
		}
	}
	return nil
}

func emitTemplateValue(value any, expr string) error {
	return emitTemplateValueTo(os.Stdout, value, expr)
}

func emitTemplateValueTo(w io.Writer, value any, expr string) error {
	tpl, err := template.New("output").Funcs(template.FuncMap{
		"json":   func(v any) (string, error) { b, e := json.Marshal(v); return string(b), e },
		"pretty": func(v any) (string, error) { b, e := json.MarshalIndent(v, "", "  "); return string(b), e },
	}).Parse(expr)
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, value); err != nil {
		return fmt.Errorf("template: %w", err)
	}
	_, err = io.Copy(w, &buf)
	return err
}

func toMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
