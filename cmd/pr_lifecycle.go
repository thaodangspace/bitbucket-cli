package cmd

import (
	"context"
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
	"github.com/thaodangspace/bitbucket-cli/selector"
)

// parseLifecycleSelector accepts the normal numeric/URL forms and a source
// branch name. Branch names are resolved by resolvePRContext.
func parseLifecycleSelector(value string) (selector.PullRequestSelector, error) {
	selected, err := parsePullRequestSelector(value)
	if err == nil {
		return selected, nil
	}
	branch := strings.TrimSpace(value)
	if branch != "" && !strings.Contains(branch, "://") && !strings.ContainsAny(branch, "?#") && !strings.HasPrefix(branch, "/") {
		return selector.PullRequestSelector{Branch: branch}, nil
	}
	return selector.PullRequestSelector{}, err
}

// resolvePRContext resolves numeric IDs, Bitbucket URLs, source branches, and
// the current branch. Branch selectors are deliberately rejected when they
// match more than one candidate unless exactly one open PR targets main.
func resolvePRContext(cmd *cobra.Command, args []string) (selector.PullRequestSelector, repoContext, error) {
	var selected selector.PullRequestSelector
	var err error
	if len(args) == 1 {
		selected, err = parseLifecycleSelector(args[0])
		if err != nil {
			return selected, repoContext{}, err
		}
	}

	cfg, client, err := newClient()
	if err != nil {
		return selected, repoContext{}, err
	}
	ref, base, err := resolveRepoFor(cfg, selected.Repository)
	if err != nil {
		return selected, repoContext{}, err
	}
	if selected.ID > 0 {
		return selected, repoContext{client: client, base: base}, nil
	}

	branch := selected.Branch
	omitted := branch == ""
	if omitted {
		branch, err = currentGitBranch()
		if err != nil {
			return selected, repoContext{}, err
		}
	}
	parts := []string{fmt.Sprintf("source.branch.name=%q", branch)}
	if omitted {
		parts = append(parts, `state="OPEN"`)
	}
	if remotes, remoteErr := gitRunner.Remotes(ctx(cmd)); remoteErr == nil {
		if sourceRepo, ok := trackedSourceRepository(ctx(cmd), remotes); ok {
			parts = append(parts, fmt.Sprintf("source.repository.full_name=%q", sourceRepo.Workspace+"/"+sourceRepo.Repo))
		}
	}
	q := url.Values{
		"q":       {strings.Join(parts, " AND ")},
		"pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)},
	}
	values, err := client.PaginateAll(ctx(cmd), fmt.Sprintf("%s/pullrequests?%s", base, q.Encode()), bitbucket.DefaultMaxPages)
	if err != nil {
		return selected, repoContext{}, err
	}
	if len(values) == 0 && len(parts) > 1 {
		fallback := []string{fmt.Sprintf("source.branch.name=%q", branch)}
		if omitted {
			fallback = append(fallback, `state="OPEN"`)
		}
		fallbackQuery := url.Values{"q": {strings.Join(fallback, " AND ")}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
		values, err = client.PaginateAll(ctx(cmd), fmt.Sprintf("%s/pullrequests?%s", base, fallbackQuery.Encode()), bitbucket.DefaultMaxPages)
		if err != nil {
			return selected, repoContext{}, err
		}
	}
	if len(values) == 0 && omitted {
		if commits, ok := gitRunner.(gitCommit); ok {
			if commit, commitErr := commits.CurrentCommit(ctx(cmd)); commitErr == nil {
				values, err = pullRequestsForCommit(ctx(cmd), client, base, commit)
				if err != nil {
					return selected, repoContext{}, err
				}
			}
		}
	}
	if len(values) == 0 {
		return selected, repoContext{}, fmt.Errorf("no pull request found for source branch %q", branch)
	}

	if len(values) > 1 {
		candidates := make([]string, 0, len(values))
		for _, raw := range values {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				return selected, repoContext{}, err
			}
			title, _ := item["title"].(string)
			candidates = append(candidates, fmt.Sprintf("#%s %q", fmt.Sprint(numberValue(item["id"])), title))
		}
		return selected, repoContext{}, fmt.Errorf("ambiguous pull request selector for branch %q; candidates: %s", branch, strings.Join(candidates, ", "))
	}
	var candidate map[string]any
	if err := json.Unmarshal(values[0], &candidate); err != nil {
		return selected, repoContext{}, err
	}
	id, ok := candidate["id"]
	if !ok {
		return selected, repoContext{}, fmt.Errorf("selected pull request has no ID")
	}
	prID, err := positiveJSONID(id)
	if err != nil {
		return selected, repoContext{}, err
	}
	repo := selector.Repository{Workspace: ref.Workspace, Repo: ref.RepoSlug}
	selected = selector.PullRequestSelector{Repository: &repo, ID: prID, Branch: branch}
	return selected, repoContext{client: client, base: base}, nil
}

func numberValue(value any) any {
	if f, ok := value.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return value
}

func positiveJSONID(value any) (int, error) {
	f, ok := value.(float64)
	if !ok || f <= 0 || f != float64(int(f)) {
		return 0, fmt.Errorf("pull request has invalid ID %v", value)
	}
	return int(f), nil
}

func prRaw(cmd *cobra.Command, prctx repoContext, id int, suffix string) (json.RawMessage, error) {
	var raw json.RawMessage
	path := fmt.Sprintf("%s/pullrequests/%d%s", prctx.base, id, suffix)
	if err := prctx.client.Request(ctx(cmd), path, bitbucket.RequestOptions{}, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func prObject(cmd *cobra.Command, prctx repoContext, id int) (map[string]any, error) {
	raw, err := prRaw(cmd, prctx, id, "")
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func renderPR(raw json.RawMessage) error {
	return emitObjectFields(raw, output.PullRequestFields, output.PullRequestSummary)
}

func runPRView(cmd *cobra.Command, args []string, viewComments, viewActivity, viewWeb bool) error {
	selected, prctx, err := resolvePRContext(cmd, args)
	if err != nil {
		return fail(err)
	}
	if viewWeb {
		ref, err := repositoryRefForPR(selected, prctx)
		if err != nil {
			return fail(err)
		}
		return openWeb(buildPRURL(ref.Workspace, ref.Repo, selected.ID))
	}
	if !viewComments && !viewActivity {
		raw, err := prRaw(cmd, prctx, selected.ID, "")
		if err != nil {
			return fail(err)
		}
		return renderPR(raw)
	}
	result := map[string]any{}
	pr, err := prObject(cmd, prctx, selected.ID)
	if err != nil {
		return fail(err)
	}
	result["pull_request"] = pr
	if viewComments {
		values, err := prctx.client.PaginateAll(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/comments?pagelen=%d", prctx.base, selected.ID, bitbucket.DefaultPageLen), bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		result["comments"] = rawValues(values)
	}
	if viewActivity {
		values, err := prctx.client.PaginateAll(ctx(cmd), fmt.Sprintf("%s/pullrequests/%d/activity?pagelen=%d", prctx.base, selected.ID, bitbucket.DefaultPageLen), bitbucket.DefaultMaxPages)
		if err != nil {
			return fail(err)
		}
		result["activity"] = rawValues(values)
	}
	return renderJSON(result)
}

func init() {
	var diffPatch, diffStat, diffNames bool
	diffCmd := &cobra.Command{
		Use: "diff [<selector>]", Short: "Show pull request changes",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			variants := 0
			if diffPatch {
				variants++
			}
			if diffStat {
				variants++
			}
			if diffNames {
				variants++
			}
			if variants > 1 {
				return fail(fmt.Errorf("--patch, --stat, and --name-only are mutually exclusive"))
			}
			selected, prctx, err := resolvePRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			suffix := "/diff"
			if diffPatch {
				suffix = "/patch"
			} else if diffStat || diffNames {
				suffix = "/diffstat"
			}
			path := fmt.Sprintf("%s/pullrequests/%d%s", prctx.base, selected.ID, suffix)
			if diffNames {
				values, err := prctx.client.PaginateAll(ctx(cmd), path, bitbucket.DefaultMaxPages)
				if err != nil {
					return fail(err)
				}
				return writeDiffNameValues(values)
			}
			resp, err := prctx.client.Do(ctx(cmd), path, bitbucket.RequestOptions{Headers: http.Header{"Accept": {"text/plain"}}})
			if err != nil {
				return fail(err)
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return fail(err)
			}
			return writeOutput(data)
		},
	}
	diffCmd.Flags().BoolVar(&diffPatch, "patch", false, "Show a patch")
	diffCmd.Flags().BoolVar(&diffStat, "stat", false, "Show diffstat")
	diffCmd.Flags().BoolVar(&diffNames, "name-only", false, "Show changed filenames only")
	prCmd.AddCommand(diffCmd)

	var checkWatch bool
	var checkInterval time.Duration
	checksCmd := &cobra.Command{
		Use: "checks [<selector>]", Short: "Show pull request commit checks",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, prctx, err := resolvePRContext(cmd, args)
			if err != nil {
				return fail(err)
			}
			for {
				values, err := clientPaginateChecks(ctx(cmd), prctx, selected.ID)
				if err != nil {
					return fail(err)
				}
				failed, pending := checkStates(values)
				if !checkWatch || (!pending) {
					if err := emitList(values, checkSummary, "No checks found."); err != nil {
						return fail(err)
					}
					if failed {
						return fail(fmt.Errorf("one or more pull request checks failed"))
					}
					if pending {
						return fail(fmt.Errorf("pull request checks are pending"))
					}
					return nil
				}
				select {
				case <-ctx(cmd).Done():
					return fail(ctx(cmd).Err())
				case <-time.After(checkInterval):
				}
			}
		},
	}
	checksCmd.Flags().BoolVar(&checkWatch, "watch", false, "Wait for checks to finish")
	checksCmd.Flags().DurationVar(&checkInterval, "interval", 10*time.Second, "Polling interval")
	prCmd.AddCommand(checksCmd)

	conflictsCmd := &cobra.Command{Use: "conflicts [<selector>]", Short: "Show pull request conflicts", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		selected, prctx, err := resolvePRContext(cmd, args)
		if err != nil {
			return fail(err)
		}
		raw, err := prRaw(cmd, prctx, selected.ID, "/conflicts")
		if err != nil {
			return fail(err)
		}
		return renderJSONValue(raw)
	}}
	prCmd.AddCommand(conflictsCmd)

	addLifecycleCommands()
}

func repositoryRefForPR(selected selector.PullRequestSelector, prctx repoContext) (selector.Repository, error) {
	if selected.Repository != nil {
		return *selected.Repository, nil
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(prctx.base, "/repositories/"), "/"), "/")
	if len(parts) != 2 {
		return selector.Repository{}, fmt.Errorf("could not resolve repository for pull request")
	}
	return selector.Repository{Workspace: parts[0], Repo: parts[1]}, nil
}

func renderJSONValue(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	return renderJSON(v)
}

func rawValues(values []json.RawMessage) []any {
	items := make([]any, 0, len(values))
	for _, raw := range values {
		var value any
		if json.Unmarshal(raw, &value) == nil {
			items = append(items, value)
		}
	}
	return items
}

func writeDiffNameValues(values []json.RawMessage) error {
	for _, raw := range values {
		var value struct {
			New struct {
				Path string `json:"path"`
			} `json:"new"`
			Old struct {
				Path string `json:"path"`
			} `json:"old"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		name := value.New.Path
		if name == "" {
			name = value.Old.Path
		}
		if name != "" {
			if _, err := fmt.Fprintln(os.Stdout, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func clientPaginateChecks(ctx context.Context, prctx repoContext, id int) ([]json.RawMessage, error) {
	return prctx.client.PaginateAll(ctx, fmt.Sprintf("%s/pullrequests/%d/statuses?pagelen=%d", prctx.base, id, bitbucket.DefaultPageLen), bitbucket.DefaultMaxPages)
}

func checkStates(values []json.RawMessage) (failed, pending bool) {
	for _, raw := range values {
		var item map[string]any
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		state := strings.ToUpper(firstString(item, "state", "status"))
		switch state {
		case "FAILED", "ERROR", "STOPPED", "EXPIRED":
			failed = true
		case "INPROGRESS", "IN_PROGRESS", "PENDING", "RUNNING":
			pending = true
		}
	}
	return
}

func checkSummary(item map[string]any) string {
	name := firstString(item, "name", "key")
	state := firstString(item, "state", "status")
	if name == "" {
		name = "check"
	}
	if state == "" {
		state = "UNKNOWN"
	}
	return fmt.Sprintf("%s [%s]", name, state)
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key].(string); ok {
			return value
		}
		if value, ok := item[key].(map[string]any); ok {
			if name, ok := value["name"].(string); ok {
				return name
			}
		}
	}
	return ""
}
