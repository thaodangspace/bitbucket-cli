package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
	"github.com/thaodangspace/bitbucket-cli/selector"
)

type prCheckoutContext struct {
	selected selector.PullRequestSelector
	prctx    repoContext
	ref      configRef
	raw      json.RawMessage
	object   map[string]any
}

type configRef struct{ Workspace, Repo string }

type prSource struct {
	Branch      string
	Commit      string
	FullName    string
	CloneURLs   []string
	Destination string
}

func prFieldMap(value any, key string) map[string]any {
	if m, ok := value.(map[string]any); ok {
		if nested, ok := m[key].(map[string]any); ok {
			return nested
		}
	}
	return nil
}

func extractPRSource(pr map[string]any) (prSource, error) {
	source := prFieldMap(pr, "source")
	destination := prFieldMap(pr, "destination")
	if source == nil {
		return prSource{}, fmt.Errorf("pull request has no source repository or branch")
	}
	result := prSource{}
	if branch := prFieldMap(source, "branch"); branch != nil {
		result.Branch, _ = branch["name"].(string)
	}
	if commit := prFieldMap(source, "commit"); commit != nil {
		result.Commit, _ = commit["hash"].(string)
	}
	if repository := prFieldMap(source, "repository"); repository != nil {
		result.FullName, _ = repository["full_name"].(string)
		if links := prFieldMap(repository, "links"); links != nil {
			if clone, ok := links["clone"].([]any); ok {
				for _, item := range clone {
					if link, ok := item.(map[string]any); ok {
						if href, ok := link["href"].(string); ok && strings.TrimSpace(href) != "" {
							result.CloneURLs = append(result.CloneURLs, href)
						}
					}
				}
			}
		}
	}
	if branch := prFieldMap(destination, "branch"); branch != nil {
		result.Destination, _ = branch["name"].(string)
	}
	if !validGitRef(result.Branch) {
		return prSource{}, fmt.Errorf("pull request source has no valid branch name")
	}
	if result.Commit != "" && !validCommit(result.Commit) {
		return prSource{}, fmt.Errorf("pull request source has invalid commit hash")
	}
	if result.FullName == "" {
		return prSource{}, fmt.Errorf("pull request source has no repository full name")
	}
	if result.Destination == "" {
		result.Destination = "unknown"
	}
	return result, nil
}

func validCommit(value string) bool {
	return regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(strings.TrimSpace(value))
}

func validGitRef(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00 ~^:?*[\\") && !strings.Contains(value, "..") && !strings.Contains(value, "@{") && !strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/") && !strings.HasSuffix(value, ".")
}

func repositoryFromFullName(fullName string) (selector.Repository, error) {
	ref, err := selector.RepositorySelector(fullName)
	if err != nil {
		return selector.Repository{}, fmt.Errorf("invalid source repository %q: %w", fullName, err)
	}
	return ref, nil
}

func remoteRepository(raw string) (selector.Repository, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "git@") {
		if i := strings.Index(raw, ":"); i >= 0 {
			raw = "ssh://" + raw[:i] + "/" + raw[i+1:]
		}
	}
	ref, err := selector.RepositorySelector(raw)
	return ref, err == nil
}

func normalizedRemoteURL(raw string) (string, error) {
	original := raw
	if strings.HasPrefix(raw, "git@") {
		if i := strings.Index(raw, ":"); i >= 0 {
			raw = "ssh://" + raw[:i] + "/" + raw[i+1:]
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || !strings.EqualFold(u.Hostname(), "bitbucket.org") || u.User != nil && u.User.Username() != "git" {
		return "", fmt.Errorf("unsupported Bitbucket remote URL %q", original)
	}
	if u.Scheme != "https" && u.Scheme != "ssh" && u.Scheme != "git" {
		return "", fmt.Errorf("unsupported Bitbucket remote URL %q", original)
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return "", fmt.Errorf("remote URL must not contain credentials or query data")
		}
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("remote URL must not contain credentials or query data")
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("unsupported Bitbucket remote URL %q", original)
	}
	return strings.ToLower(u.Hostname()) + "/" + strings.ToLower(parts[0]) + "/" + strings.ToLower(parts[1]), nil
}

func sourceCloneURL(source prSource, repo selector.Repository) (string, error) {
	for _, candidate := range source.CloneURLs {
		if _, err := normalizedRemoteURL(candidate); err == nil {
			return candidate, nil
		}
	}
	// The API normally supplies clone links, but synthesizing this URL is safe
	// and avoids ever putting an API token in git configuration.
	return fmt.Sprintf("https://bitbucket.org/%s/%s.git", url.PathEscape(repo.Workspace), url.PathEscape(repo.Repo)), nil
}

func trackedSourceRepository(ctx context.Context, remotes []Remote) (selector.Repository, bool) {
	if tracking, ok := gitRunner.(gitTracking); ok {
		name, _, err := tracking.TrackingRemote(ctx)
		if err == nil && name != "" {
			for _, remote := range remotes {
				if remote.Name == name {
					if ref, ok := remoteRepository(remote.FetchURL); ok {
						return ref, true
					}
				}
			}
		}
	}
	for _, remote := range remotes {
		if remote.Name == "origin" {
			if ref, ok := remoteRepository(remote.FetchURL); ok {
				return ref, true
			}
		}
	}
	return selector.Repository{}, false
}

func pullRequestCandidates(ctx context.Context, client *bitbucket.Client, base, branch string, sourceRepo *selector.Repository, openOnly bool) ([]json.RawMessage, error) {
	parts := []string{fmt.Sprintf("source.branch.name=%q", branch)}
	if sourceRepo != nil {
		parts = append(parts, fmt.Sprintf("source.repository.full_name=%q", sourceRepo.Workspace+"/"+sourceRepo.Repo))
	}
	if openOnly {
		parts = append(parts, `state="OPEN"`)
	}
	q := url.Values{"q": {strings.Join(parts, " AND ")}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
	values, err := client.PaginateAll(ctx, fmt.Sprintf("%s/pullrequests?%s", base, q.Encode()), bitbucket.DefaultMaxPages)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 && sourceRepo != nil {
		// A fork may not be represented by the repository inferred from origin.
		return pullRequestCandidates(ctx, client, base, branch, nil, openOnly)
	}
	return values, nil
}

func pullRequestsForCommit(ctx context.Context, client *bitbucket.Client, base, commit string) ([]json.RawMessage, error) {
	if !validCommit(commit) {
		return nil, fmt.Errorf("invalid current commit hash")
	}
	q := url.Values{"q": {`state="OPEN"`}, "pagelen": {fmt.Sprint(bitbucket.DefaultPageLen)}}
	return client.PaginateAll(ctx, fmt.Sprintf("%s/commit/%s/pullrequests?%s", base, url.PathEscape(commit), q.Encode()), bitbucket.DefaultMaxPages)
}

func choosePullRequest(values []json.RawMessage, branch string) (int, error) {
	if len(values) == 0 {
		return 0, fmt.Errorf("no open pull request found for source branch %q", branch)
	}
	if len(values) == 1 {
		var item map[string]any
		if err := json.Unmarshal(values[0], &item); err != nil {
			return 0, err
		}
		return positiveJSONID(item["id"])
	}
	candidates := make([]string, 0, len(values))
	for _, raw := range values {
		var item map[string]any
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		id := fmt.Sprint(numberValue(item["id"]))
		title, _ := item["title"].(string)
		candidates = append(candidates, fmt.Sprintf("#%s %q", id, title))
	}
	return 0, fmt.Errorf("ambiguous pull request selector for branch %q; candidates: %s", branch, strings.Join(candidates, ", "))
}

func resolveCheckoutPR(cmd *cobra.Command, args []string) (prCheckoutContext, error) {
	if _, err := gitRunner.Root(ctx(cmd)); err != nil {
		return prCheckoutContext{}, err
	}
	var selected selector.PullRequestSelector
	var err error
	if len(args) == 1 {
		selected, err = parseLifecycleSelector(args[0])
		if err != nil {
			return prCheckoutContext{}, err
		}
	}
	cfg, client, err := newClient()
	if err != nil {
		return prCheckoutContext{}, err
	}
	ref, base, err := resolveRepoFor(cfg, selected.Repository)
	if err != nil {
		return prCheckoutContext{}, err
	}
	if selected.ID == 0 {
		branch := selected.Branch
		omittedSelector := branch == ""
		if omittedSelector {
			branch, err = gitRunner.CurrentBranch(ctx(cmd))
			if err != nil {
				return prCheckoutContext{}, err
			}
		}
		remotes, remoteErr := gitRunner.Remotes(ctx(cmd))
		if remoteErr != nil {
			return prCheckoutContext{}, remoteErr
		}
		sourceRepo, hasSourceRepo := trackedSourceRepository(ctx(cmd), remotes)
		if !hasSourceRepo {
			sourceRepo = selector.Repository{Workspace: ref.Workspace, Repo: ref.RepoSlug}
		}
		values, err := pullRequestCandidates(ctx(cmd), client, base, branch, &sourceRepo, true)
		if err != nil {
			return prCheckoutContext{}, err
		}
		if len(values) == 0 && omittedSelector {
			if commits, ok := gitRunner.(gitCommit); ok {
				if commit, commitErr := commits.CurrentCommit(ctx(cmd)); commitErr == nil {
					values, err = pullRequestsForCommit(ctx(cmd), client, base, commit)
					if err != nil {
						return prCheckoutContext{}, err
					}
				}
			}
		}
		selected.ID, err = choosePullRequest(values, branch)
		if err != nil {
			return prCheckoutContext{}, err
		}
		selected.Branch = branch
	}
	raw, err := prRaw(cmd, repoContext{client: client, base: base}, selected.ID, "")
	if err != nil {
		return prCheckoutContext{}, err
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return prCheckoutContext{}, err
	}
	return prCheckoutContext{selected: selected, prctx: repoContext{client: client, base: base}, ref: configRef{ref.Workspace, ref.RepoSlug}, raw: raw, object: object}, nil
}

func chooseRemote(ctx context.Context, source prSource, sourceRepo selector.Repository, destination selector.Repository, remotes []Remote) (Remote, bool, error) {
	wanted, err := sourceCloneURL(source, sourceRepo)
	if err != nil {
		return Remote{}, false, err
	}
	wantedNormalized, err := normalizedRemoteURL(wanted)
	if err != nil {
		return Remote{}, false, err
	}
	for _, remote := range remotes {
		for _, candidate := range []string{remote.FetchURL, remote.PushURL} {
			if candidate == "" {
				continue
			}
			if normalized, normalizeErr := normalizedRemoteURL(candidate); normalizeErr == nil && normalized == wantedNormalized {
				return remote, false, nil
			}
		}
	}
	if sourceRepo.Workspace == destination.Workspace && sourceRepo.Repo == destination.Repo {
		for _, remote := range remotes {
			if remote.Name == "origin" {
				if _, normalizeErr := normalizedRemoteURL(remote.FetchURL); normalizeErr == nil {
					return remote, false, nil
				}
			}
		}
	}
	name := safeRemoteName(sourceRepo.Workspace)
	used := map[string]bool{}
	for _, remote := range remotes {
		used[remote.Name] = true
	}
	baseName := name
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d", baseName, i)
	}
	manager, ok := gitRunner.(gitRemoteManager)
	if !ok {
		return Remote{}, false, fmt.Errorf("source repository requires remote %q, but this git runner cannot add remotes", name)
	}
	if err := manager.AddRemote(ctx, name, wanted); err != nil {
		return Remote{}, false, err
	}
	return Remote{Name: name, FetchURL: wanted}, true, nil
}

func safeRemoteName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "pr-source"
	}
	return name
}

func validRemoteName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00 \t\r\n") && !strings.Contains(value, "..")
}

func fetchPRSource(ctx context.Context, source prSource, remote Remote, prID int) (string, error) {
	if !validRemoteName(remote.Name) {
		return "", fmt.Errorf("invalid git remote name %q", remote.Name)
	}
	if source.Branch != "" {
		refspec := "refs/heads/" + source.Branch + ":refs/remotes/" + remote.Name + "/" + source.Branch
		if err := gitRunner.Fetch(ctx, remote.Name, refspec, "--no-tags"); err == nil {
			return "refs/remotes/" + remote.Name + "/" + source.Branch, nil
		} else if source.Commit == "" {
			return "", fmt.Errorf("source branch %q could not be fetched: %w", source.Branch, err)
		}
	}
	if source.Commit == "" {
		return "", fmt.Errorf("source branch %q was deleted and the pull request has no source commit", source.Branch)
	}
	if err := gitRunner.Fetch(ctx, remote.Name, source.Commit, "--no-tags"); err != nil {
		return "", fmt.Errorf("source branch %q is unavailable and source commit %s could not be fetched: %w", source.Branch, source.Commit, err)
	}
	return source.Commit, nil
}

func checkoutPR(ctx context.Context, source prSource, remote Remote, prID int, localBranch string, detach, force, recurse bool) (map[string]any, error) {
	status, err := gitRunner.StatusPorcelain(ctx)
	if err != nil {
		return nil, err
	}
	if len(status) != 0 {
		return nil, fmt.Errorf("refusing to checkout pull request with a dirty working tree")
	}
	fetchedRef, err := fetchPRSource(ctx, source, remote, prID)
	if err != nil {
		return nil, err
	}
	if !detach && source.Commit != "" && fetchedRef == source.Commit {
		return nil, fmt.Errorf("source branch %q was deleted; use --detach to check out source commit %s", source.Branch, source.Commit)
	}
	result := map[string]any{
		"source_branch": source.Branch, "destination_branch": source.Destination,
		"remote": remote.Name, "fetched_ref": fetchedRef, "commit": source.Commit,
		"detached": detach, "created": false, "reset": false,
	}
	if detach {
		target := source.Commit
		if target == "" {
			target = fetchedRef
		}
		if err := gitRunner.Checkout(ctx, "--detach", target); err != nil {
			return nil, err
		}
		return result, nil
	}
	if !validGitRef(localBranch) {
		return nil, fmt.Errorf("invalid local branch name %q", localBranch)
	}
	target := fetchedRef
	if source.Commit != "" {
		// The source branch may have advanced between the API read and fetch;
		// always create/reset at the exact commit recorded on the PR.
		target = source.Commit
	}
	state, hasState := gitRunner.(gitBranchState)
	exists, currentCommit := false, ""
	if hasState {
		exists, currentCommit, err = state.BranchStatus(ctx, localBranch)
		if err != nil {
			return nil, err
		}
	}
	if !exists {
		if err := gitRunner.Checkout(ctx, "-b", localBranch, target); err != nil {
			return nil, err
		}
		result["created"] = true
	} else if !hasState {
		if !force {
			return nil, fmt.Errorf("local branch %q already exists; use --force to reset it", localBranch)
		}
		if err := gitRunner.Checkout(ctx, "-B", localBranch, target); err != nil {
			return nil, err
		}
		result["reset"] = true
	} else if currentCommit != source.Commit && source.Commit != "" {
		if !force {
			ancestor, ancestorErr := state.IsAncestor(ctx, currentCommit, source.Commit)
			if ancestorErr != nil {
				return nil, ancestorErr
			}
			if !ancestor {
				return nil, fmt.Errorf("local branch %q diverges from pull request commit; use --force to reset it", localBranch)
			}
		}
		if err := gitRunner.Checkout(ctx, "-B", localBranch, target); err != nil {
			return nil, err
		}
		result["reset"] = true
	} else if err := gitRunner.Checkout(ctx, localBranch); err != nil {
		return nil, err
	}
	if hasState {
		upstream := strings.TrimPrefix(fetchedRef, "refs/remotes/")
		if err := state.SetUpstream(ctx, localBranch, upstream); err != nil {
			return nil, err
		}
	}
	if recurse {
		updater, ok := gitRunner.(gitSubmodules)
		if !ok {
			return nil, fmt.Errorf("git runner cannot update submodules")
		}
		if err := updater.UpdateSubmodules(ctx); err != nil {
			return nil, err
		}
	}
	result["branch"] = localBranch
	return result, nil
}

func checkoutSummary(result map[string]any) string {
	return fmt.Sprintf("Checked out PR #%v: %s -> %s\nBranch: %v\nRemote: %v\nCommit: %v", result["pull_request_id"], result["source_branch"], result["destination_branch"], result["branch"], result["remote"], result["commit"])
}

func renderCheckoutResult(result map[string]any) error {
	mode, err := outputMode()
	if err != nil {
		return err
	}
	if mode == "table" {
		return writeOutput([]byte(checkoutSummary(result) + "\n"))
	}
	return renderJSON(result)
}

func init() {
	var branch string
	var detach, force, recurse bool
	checkoutCmd := &cobra.Command{
		Use: "checkout [<selector>]", Short: "Check out a pull request locally",
		Long: "Check out a Bitbucket pull request locally. This is a mutating git operation; use only when explicitly requested.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveCheckoutPR(cmd, args)
			if err != nil {
				return fail(err)
			}
			source, err := extractPRSource(resolved.object)
			if err != nil {
				return fail(err)
			}
			sourceRepo, err := repositoryFromFullName(source.FullName)
			if err != nil {
				return fail(err)
			}
			remotes, err := gitRunner.Remotes(ctx(cmd))
			if err != nil {
				return fail(err)
			}
			remote, _, err := chooseRemote(ctx(cmd), source, sourceRepo, selector.Repository{Workspace: resolved.ref.Workspace, Repo: resolved.ref.Repo}, remotes)
			if err != nil {
				return fail(err)
			}
			local := branch
			if local == "" {
				local = source.Branch
			}
			result, err := checkoutPR(ctx(cmd), source, remote, resolved.selected.ID, local, detach, force, recurse)
			if err != nil {
				return fail(err)
			}
			result["pull_request_id"] = resolved.selected.ID
			result["title"] = resolved.object["title"]
			return renderCheckoutResult(result)
		},
	}
	checkoutCmd.Flags().StringVar(&branch, "branch", "", "Local branch name (defaults to the pull request source branch)")
	checkoutCmd.Flags().BoolVar(&detach, "detach", false, "Check out the source commit without creating a local branch")
	checkoutCmd.Flags().BoolVar(&force, "force", false, "Allow resetting an existing or diverged local branch")
	checkoutCmd.Flags().BoolVar(&recurse, "recurse-submodules", false, "Initialize and update submodules recursively")
	prCmd.AddCommand(checkoutCmd)

	var web bool
	currentCmd := &cobra.Command{
		Use: "current", Short: "Find the open pull request for the current branch", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolveCheckoutPR(cmd, nil)
			if err != nil {
				return fail(err)
			}
			if web {
				return openWeb(buildPRURL(resolved.ref.Workspace, resolved.ref.Repo, resolved.selected.ID))
			}
			return emitObjectFields(resolved.raw, output.PullRequestFields, output.PullRequestSummary)
		},
	}
	currentCmd.Flags().BoolVar(&web, "web", false, "Open the current branch pull request in a browser")
	prCmd.AddCommand(currentCmd)
}
