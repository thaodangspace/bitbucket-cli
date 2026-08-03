package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"

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
func parseID(arg string) (int, error) {
	return parsePositiveID("pull request id", arg)
}

func parsePositiveID(label, arg string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive integer", label, arg)
	}
	return id, nil
}

// emitObject renders a single decoded JSON object as JSON, or its pretty
// summary when --pretty is set.
func emitObject(raw json.RawMessage, summary func(map[string]any) string) error {
	if flagPretty {
		m, err := toMap(raw)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, summary(m))
		return err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	return output.RenderJSON(os.Stdout, v)
}

// emitList renders a list of raw JSON objects as a JSON array, or one summary
// line per item when --pretty is set.
func emitList(values []json.RawMessage, summary func(map[string]any) string, emptyMsg string) error {
	if flagPretty {
		lines := make([]string, 0, len(values))
		for _, raw := range values {
			m, err := toMap(raw)
			if err != nil {
				return err
			}
			lines = append(lines, summary(m))
		}
		return output.RenderLines(os.Stdout, lines, emptyMsg)
	}
	items := make([]any, 0, len(values))
	for _, raw := range values {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		items = append(items, v)
	}
	return output.RenderJSON(os.Stdout, items)
}

func toMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
