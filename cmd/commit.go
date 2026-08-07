package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
	"github.com/thaodangspace/bitbucket-cli/selector"
)

// commitContext keeps a resolved repository and commit selector together. A
// selector is retained in output so abbreviated hashes and branch names are
// not silently lost by a mutating command.
type commitContext struct {
	client    *bitbucket.Client
	base      string
	workspace string
	repo      string
	requested string
	resolved  string
}

var (
	hexPrefix          = regexp.MustCompile(`^[0-9a-fA-F]{7,39}$`)
	commitListIncludes []string
	commitListExcludes []string
)

func commitPath(base, hash string) string {
	return fmt.Sprintf("%s/commit/%s", base, bitbucket.EncodePathSegment(hash))
}

func commitSelector(value string) (selector.Repository, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return selector.Repository{}, "", fmt.Errorf("commit selector is required")
	}
	u, err := url.Parse(value)
	if err != nil {
		return selector.Repository{}, "", fmt.Errorf("parse commit selector: %w", err)
	}
	if !u.IsAbs() {
		return selector.Repository{}, value, nil
	}
	if u.Scheme != "https" || (u.Hostname() != "bitbucket.org" && u.Hostname() != "api.bitbucket.org") || u.User != nil {
		return selector.Repository{}, "", fmt.Errorf("unsupported Bitbucket commit URL %q", value)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := range parts {
		parts[i], err = url.PathUnescape(parts[i])
		if err != nil {
			return selector.Repository{}, "", fmt.Errorf("decode commit URL: %w", err)
		}
	}
	// API URLs contain /2.0/repositories/<workspace>/<repo>/commit/<hash>.
	for i := 0; i+4 < len(parts); i++ {
		if parts[i] == "repositories" && (parts[i+3] == "commit" || parts[i+3] == "commits") {
			return selector.Repository{Workspace: parts[i+1], Repo: parts[i+2]}, parts[i+4], nil
		}
	}
	if len(parts) >= 4 && (parts[2] == "commit" || parts[2] == "commits") {
		return selector.Repository{Workspace: parts[0], Repo: parts[1]}, parts[3], nil
	}
	return selector.Repository{}, "", fmt.Errorf("unsupported Bitbucket commit URL %q", value)
}

func newCommitContext(cmd *cobra.Command, value string, resolve bool) (commitContext, error) {
	selectedRepo, spec, err := commitSelector(value)
	if err != nil {
		return commitContext{}, err
	}
	cfg, client, err := newClient()
	if err != nil {
		return commitContext{}, err
	}
	var repo *selector.Repository
	if selectedRepo.Workspace != "" {
		repo = &selectedRepo
	}
	ref, base, err := resolveRepoFor(cfg, repo)
	if err != nil {
		return commitContext{}, err
	}
	resolved := spec
	if resolve {
		resolved, err = resolveCommitHash(ctx(cmd), client, base, spec)
		if err != nil {
			return commitContext{}, err
		}
	}
	return commitContext{client: client, base: base, workspace: ref.Workspace, repo: ref.RepoSlug, requested: value, resolved: resolved}, nil
}

func resolveCommitRefDirect(ctx context.Context, client *bitbucket.Client, base, spec string) (string, error) {
	var v struct {
		Hash string `json:"hash"`
	}
	if err := client.Request(ctx, base+"/commits/"+bitbucket.EncodePathSegment(spec), bitbucket.RequestOptions{}, &v); err != nil {
		return "", fmt.Errorf("resolve target %q: %w", spec, err)
	}
	if v.Hash == "" {
		return "", fmt.Errorf("target %q did not resolve to a commit hash", spec)
	}
	return v.Hash, nil
}

func resolveCommitRef(ctx context.Context, client *bitbucket.Client, base, spec string) (string, error) {
	for _, kind := range []string{"branches", "tags"} {
		var v struct {
			Target struct {
				Hash string `json:"hash"`
			} `json:"target"`
			Hash string `json:"hash"`
		}
		err := client.Request(ctx, fmt.Sprintf("%s/refs/%s/%s", base, kind, bitbucket.EncodePathSegment(spec)), bitbucket.RequestOptions{}, &v)
		if err == nil {
			if v.Target.Hash != "" {
				return v.Target.Hash, nil
			}
			return v.Hash, nil
		}
		if !isNotFoundCommit(err) {
			return "", err
		}
	}
	return resolveCommitRefDirect(ctx, client, base, spec)
}

func isNotFoundCommit(err error) bool {
	e, ok := err.(*bitbucket.HTTPError)
	return ok && e.Status == http.StatusNotFound
}

func resolveCommitHash(ctx context.Context, client *bitbucket.Client, base, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("commit selector is required")
	}
	if strings.Contains(spec, "..") {
		return "", fmt.Errorf("commit range %q is not valid for this operation", spec)
	}
	if hashLooksComplete(spec) {
		return strings.ToLower(spec), nil
	}
	// Query hash-like prefixes first. This makes an ambiguous abbreviation a
	// clear error instead of allowing a mutation to target an arbitrary commit.
	if hexPrefix.MatchString(spec) {
		q := url.Values{"q": []string{fmt.Sprintf(`hash ~ "%s"`, strings.ToLower(spec))}, "pagelen": []string{"100"}}
		values, err := client.Paginate(ctx, base+"/commits?"+q.Encode(), 100, bitbucket.DefaultMaxPages)
		if err != nil {
			return "", err
		}
		matches := make([]string, 0, len(values))
		for _, raw := range values {
			var item struct {
				Hash string `json:"hash"`
			}
			if json.Unmarshal(raw, &item) == nil && strings.HasPrefix(strings.ToLower(item.Hash), strings.ToLower(spec)) {
				matches = append(matches, item.Hash)
			}
		}
		if len(matches) > 1 {
			return "", fmt.Errorf("ambiguous commit abbreviation %q matches %d commits", spec, len(matches))
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		return resolveCommitRefDirect(ctx, client, base, spec)
	}
	return resolveCommitRef(ctx, client, base, spec)
}

func addCommitMetadata(value map[string]any, requested, resolved string) {
	value["requested_selector"] = requested
	value["resolved_hash"] = resolved
}

func enrichCommit(raw json.RawMessage, requested string) json.RawMessage {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	resolved, _ := value["hash"].(string)
	addCommitMetadata(value, requested, resolved)
	encoded, _ := json.Marshal(value)
	return encoded
}

func commitValues(ctx context.Context, client *bitbucket.Client, path string, limit int) ([]json.RawMessage, error) {
	return client.Paginate(ctx, path, limit, bitbucket.DefaultMaxPages)
}

func init() {
	var listPath, listQuery string
	var listLimit int
	listCmd := &cobra.Command{
		Use: "list [<ref>]", Short: "List repository commits", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}
			requested := ref
			selectedRepo, spec, err := commitSelector(ref)
			if err != nil && ref != "" {
				return fail(err)
			}
			if ref != "" {
				ref = spec
			}
			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			var repo *selector.Repository
			if selectedRepo.Workspace != "" {
				repo = &selectedRepo
			}
			_, base, err := resolveRepoFor(cfg, repo)
			if err != nil {
				return fail(err)
			}
			path := base + "/commits"
			if ref != "" {
				path += "/" + bitbucket.EncodePathSegment(ref)
			}
			q := url.Values{}
			if listPath != "" {
				q.Set("path", listPath)
			}
			if listQuery != "" {
				q.Set("q", listQuery)
			}
			for _, v := range commitListIncludes {
				q.Add("include", v)
			}
			for _, v := range commitListExcludes {
				q.Add("exclude", v)
			}
			if listLimit > 0 {
				q.Set("pagelen", fmt.Sprint(minInt(listLimit, bitbucket.DefaultPageLen)))
			}
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}
			values, err := commitValues(ctx(cmd), client, path, listLimit)
			if err != nil {
				return fail(err)
			}
			for i := range values {
				values[i] = enrichCommit(values[i], requested)
			}
			return emitListFields(values, output.CommitFields, output.CommitSummary, "No commits found.")
		},
	}
	listCmd.Flags().StringVar(&listPath, "path", "", "Only commits affecting this path")
	listCmd.Flags().StringArrayVar(&commitListIncludes, "include", nil, "Include commits reachable from this ref (repeatable)")
	listCmd.Flags().StringArrayVar(&commitListExcludes, "exclude", nil, "Exclude commits reachable from this ref (repeatable)")
	listCmd.Flags().StringVar(&listQuery, "query", "", "Bitbucket query expression (BBQL)")
	listCmd.Flags().IntVar(&listLimit, "limit", bitbucket.DefaultLimit, "Maximum commits to return")

	var comments, statuses, reports, web bool
	viewCmd := &cobra.Command{
		Use: "view <commit>", Short: "View a repository commit", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := newCommitContext(cmd, args[0], true)
			if err != nil {
				return fail(err)
			}
			var value map[string]any
			if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved), bitbucket.RequestOptions{}, &value); err != nil {
				return fail(err)
			}
			addCommitMetadata(value, cc.requested, cc.resolved)
			if comments {
				v, e := commitValues(ctx(cmd), cc.client, commitPath(cc.base, cc.resolved)+"/comments", 0)
				if e != nil {
					return fail(e)
				}
				value["comments"] = rawValues(v)
			}
			if statuses {
				v, e := commitValues(ctx(cmd), cc.client, commitPath(cc.base, cc.resolved)+"/statuses", 0)
				if e != nil {
					return fail(e)
				}
				value["statuses"] = rawValues(v)
			}
			if reports {
				v, e := commitValues(ctx(cmd), cc.client, commitPath(cc.base, cc.resolved)+"/reports", 0)
				if e != nil {
					return fail(e)
				}
				value["reports"] = rawValues(v)
			}
			if web {
				if err := openWeb(fmt.Sprintf("https://bitbucket.org/%s/%s/commits/%s", url.PathEscape(cc.workspace), url.PathEscape(cc.repo), url.PathEscape(cc.resolved))); err != nil {
					return fail(err)
				}
			}
			return renderValue(value, output.CommitFields, output.CommitSummary, false, "")
		},
	}
	viewCmd.Flags().BoolVar(&comments, "comments", false, "Include commit comments")
	viewCmd.Flags().BoolVar(&statuses, "statuses", false, "Include commit build statuses")
	viewCmd.Flags().BoolVar(&reports, "reports", false, "Include Code Insights reports")
	viewCmd.Flags().BoolVar(&web, "web", false, "Open the commit in a browser")

	var patch, stat, names bool
	var contextLines int
	diffCmd := &cobra.Command{
		Use: "diff <commit-or-range>", Short: "Show a commit or range diff", Long: "Show a raw Bitbucket diff. For A..B, Bitbucket means commits reachable from B excluding commits reachable from A; the selector is passed to Bitbucket unchanged.", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if patch && stat || patch && names || stat && names {
				return fail(fmt.Errorf("--patch, --stat, and --name-only are mutually exclusive"))
			}
			if contextLines < 0 {
				return fail(fmt.Errorf("--context must not be negative"))
			}
			cc, err := newCommitContext(cmd, args[0], false)
			if err != nil {
				return fail(err)
			}
			spec := cc.resolved
			endpoint := "/diff/" + bitbucket.EncodePathSegment(spec)
			if patch {
				endpoint = "/patch/" + bitbucket.EncodePathSegment(spec)
			}
			if stat || names {
				endpoint = "/diffstat/" + bitbucket.EncodePathSegment(spec)
			}
			q := url.Values{}
			if contextLines > 0 {
				q.Set("context", fmt.Sprint(contextLines))
			}
			if stat || names {
				statPath := cc.base + endpoint
				if encoded := q.Encode(); encoded != "" {
					statPath += "?" + encoded
				}
				values, err := cc.client.PaginateAll(ctx(cmd), statPath, bitbucket.DefaultMaxPages)
				if err != nil {
					return fail(err)
				}
				if names {
					paths := []string{}
					for _, raw := range values {
						var v map[string]any
						if json.Unmarshal(raw, &v) == nil {
							if p := diffPath(v); p != "" {
								paths = append(paths, p)
							}
						}
					}
					return output.RenderLines(os.Stdout, paths, "")
				}
				return emitListFields(values, nil, func(v map[string]any) string {
					return fmt.Sprintf("%s +%v -%v", diffPath(v), v["lines_added"], v["lines_removed"])
				}, "No changes.")
			}
			resp, err := cc.client.Do(ctx(cmd), cc.base+endpoint, bitbucket.RequestOptions{Query: q, Headers: http.Header{"Accept": []string{"text/plain, application/json"}}})
			if err != nil {
				return fail(err)
			}
			defer resp.Body.Close()
			_, err = io.Copy(os.Stdout, resp.Body)
			return err
		},
	}
	diffCmd.Flags().BoolVar(&patch, "patch", false, "Use Bitbucket's patch representation")
	diffCmd.Flags().BoolVar(&stat, "stat", false, "Show diffstat")
	diffCmd.Flags().BoolVar(&names, "name-only", false, "Show changed file names")
	diffCmd.Flags().IntVar(&contextLines, "context", 0, "Number of context lines")

	commitCmd := &cobra.Command{Use: "commit", Short: "Browse commits and commit checks", Args: cobra.NoArgs}
	commitCmd.AddCommand(listCmd, viewCmd, diffCmd)
	addCommitCommentCommands(commitCmd)
	addCommitReviewCommands(commitCmd)
	addCommitStatusCommands(commitCmd)
	rootCmd.AddCommand(commitCmd)
	addReportCommands()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func diffPath(v map[string]any) string {
	for _, key := range []string{"new", "old"} {
		if m, ok := v[key].(map[string]any); ok {
			if p, ok := m["path"].(string); ok && p != "" {
				return p
			}
		}
	}
	if p, ok := v["path"].(string); ok {
		return p
	}
	return ""
}

func addCommitCommentCommands(parent *cobra.Command) {
	comment := &cobra.Command{Use: "comment", Short: "Manage commit comments"}
	var limit int
	list := &cobra.Command{Use: "list <commit>", Short: "List comments on a commit", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		values, err := commitValues(ctx(cmd), cc.client, commitPath(cc.base, cc.resolved)+"/comments", limit)
		if err != nil {
			return fail(err)
		}
		return emitListFields(values, output.CommentFields, func(v map[string]any) string { return fmt.Sprintf("#%v %s", v["id"], commentText(v)) }, "No comments found.")
	}}
	list.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum comments to return")
	var body, file, path, from, to string
	create := &cobra.Command{Use: "create <commit>", Short: "Create a commit comment", Long: "Create a commit comment. This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		value, provided, err := readBody(body, file, os.Stdin)
		if err != nil {
			return fail(err)
		}
		if !provided || strings.TrimSpace(value) == "" {
			return fail(fmt.Errorf("provide a non-empty --body or --body-file"))
		}
		inline, err := parseInline(path, from, to, cmd.Flags().Changed("path"), cmd.Flags().Changed("from"), cmd.Flags().Changed("to"))
		if err != nil {
			return fail(err)
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		payload := map[string]any{"content": map[string]any{"raw": value}}
		if inline != nil {
			payload["inline"] = inline
		}
		var out json.RawMessage
		if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved)+"/comments", bitbucket.RequestOptions{Method: http.MethodPost, Body: payload}, &out); err != nil {
			return fail(err)
		}
		return emitObjectFields(out, output.CommentFields, func(v map[string]any) string { return fmt.Sprintf("#%v %s", v["id"], commentText(v)) })
	}}
	create.Flags().StringVar(&body, "body", "", "Markdown comment body")
	create.Flags().StringVar(&file, "body-file", "", "Read the body from a file (- for stdin)")
	create.Flags().StringVar(&path, "path", "", "File path for an inline comment")
	create.Flags().StringVar(&from, "from", "", "Old-side line number")
	create.Flags().StringVar(&to, "to", "", "New-side line number")
	var editBody, editFile string
	edit := &cobra.Command{Use: "edit <commit> <comment-id>", Short: "Edit a commit comment", Long: "Edit a commit comment. This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parsePositiveID("comment id", args[1])
		if err != nil {
			return fail(err)
		}
		value, provided, err := readBody(editBody, editFile, os.Stdin)
		if err != nil {
			return fail(err)
		}
		if !provided || strings.TrimSpace(value) == "" {
			return fail(fmt.Errorf("provide a non-empty --body or --body-file"))
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		var out json.RawMessage
		if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved)+fmt.Sprintf("/comments/%d", id), bitbucket.RequestOptions{Method: http.MethodPut, Body: map[string]any{"content": map[string]any{"raw": value}}}, &out); err != nil {
			return fail(err)
		}
		return emitObject(out, nil)
	}}
	edit.Flags().StringVar(&editBody, "body", "", "Replacement markdown body")
	edit.Flags().StringVar(&editFile, "body-file", "", "Read the replacement body from a file (- for stdin)")
	var yes bool
	remove := &cobra.Command{Use: "delete <commit> <comment-id>", Short: "Delete a commit comment", Long: "Delete a commit comment. This is a write operation and requires --yes in non-interactive mode.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !yes {
			return fail(fmt.Errorf("refusing to delete comment without --yes"))
		}
		id, err := parsePositiveID("comment id", args[1])
		if err != nil {
			return fail(err)
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved)+fmt.Sprintf("/comments/%d", id), bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return renderValue(map[string]any{"deleted": true, "id": id}, nil, nil, false, "")
	}}
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm deletion")
	comment.AddCommand(list, create, edit, remove)
	parent.AddCommand(comment)
}

func commentText(v map[string]any) string {
	if c, ok := v["content"].(map[string]any); ok {
		if s, ok := c["raw"].(string); ok {
			return s
		}
	}
	return ""
}

func addCommitReviewCommands(parent *cobra.Command) {
	for _, item := range []struct{ name, short, method string }{{"approve", "Approve a commit", http.MethodPost}, {"unapprove", "Remove your approval from a commit", http.MethodDelete}} {
		s := item
		cmd := &cobra.Command{Use: s.name + " <commit>", Short: s.short, Long: s.short + ". This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := newCommitContext(cmd, args[0], true)
			if err != nil {
				return fail(err)
			}
			var out json.RawMessage
			if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved)+"/approve", bitbucket.RequestOptions{Method: s.method}, &out); err != nil {
				return fail(err)
			}
			if out == nil {
				return renderValue(map[string]any{"commit": cc.resolved, "action": s.name}, nil, nil, false, "")
			}
			return emitObject(out, nil)
		}}
		parent.AddCommand(cmd)
	}
}

func addCommitStatusCommands(parent *cobra.Command) {
	status := &cobra.Command{Use: "status", Short: "Manage commit build statuses"}
	var limit int
	list := &cobra.Command{Use: "list <commit>", Short: "List build statuses for a commit", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		values, err := commitValues(ctx(cmd), cc.client, commitPath(cc.base, cc.resolved)+"/statuses", limit)
		if err != nil {
			return fail(err)
		}
		if mode, _ := outputMode(); mode == "table" {
			sort.SliceStable(values, func(i, j int) bool { return statusState(values[i]) < statusState(values[j]) })
		}
		return emitListFields(values, output.CommitStatusFields, output.CommitStatusSummary, "No statuses found.")
	}}
	list.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum statuses to return")
	var key, state, name, description, target, ref string
	set := &cobra.Command{Use: "set <commit>", Short: "Create or update a commit build status", Long: "Create or update a commit build status. This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		state = strings.ToUpper(strings.TrimSpace(state))
		key = strings.TrimSpace(key)
		if key == "" {
			return fail(fmt.Errorf("--key is required"))
		}
		if len(key) > 255 || strings.ContainsAny(key, "\r\n") {
			return fail(fmt.Errorf("invalid --key %q", key))
		}
		if !map[string]bool{"INPROGRESS": true, "SUCCESSFUL": true, "FAILED": true, "STOPPED": true}[state] {
			return fail(fmt.Errorf("invalid --state %q", state))
		}
		if target != "" {
			if err := validateTargetURL(target); err != nil {
				return fail(err)
			}
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		body := map[string]any{"key": key, "state": state}
		if name != "" {
			body["name"] = name
		}
		if description != "" {
			body["description"] = description
		}
		if target != "" {
			body["url"] = target
		}
		if ref != "" {
			body["refname"] = ref
		}
		var out json.RawMessage
		if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved)+"/statuses/build", bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &out); err != nil {
			return fail(err)
		}
		return emitObject(out, nil)
	}}
	set.Flags().StringVar(&key, "key", "", "Unique status key")
	set.Flags().StringVar(&state, "state", "", "Status: INPROGRESS, SUCCESSFUL, FAILED, or STOPPED")
	set.Flags().StringVar(&name, "name", "", "Status name")
	set.Flags().StringVar(&description, "description", "", "Status description")
	set.Flags().StringVar(&target, "url", "", "Build target URL")
	set.Flags().StringVar(&ref, "ref", "", "Reference name")
	status.AddCommand(list, set)
	parent.AddCommand(status)
}

func statusState(raw json.RawMessage) string {
	var v map[string]any
	if json.Unmarshal(raw, &v) == nil {
		if s, ok := v["state"].(string); ok {
			return s
		}
	}
	return ""
}

func validateTargetURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return fmt.Errorf("--url must be an http or https URL without credentials")
	}
	for _, key := range []string{"token", "access_token", "password", "secret"} {
		if u.Query().Get(key) != "" {
			return fmt.Errorf("--url must not contain credentials")
		}
	}
	return nil
}

type partialAnnotationError struct {
	succeeded int
	err       error
}

func (e *partialAnnotationError) Error() string {
	return fmt.Sprintf("annotation upload partially succeeded: %d annotations uploaded: %v", e.succeeded, e.err)
}
func (e *partialAnnotationError) Unwrap() error { return e.err }
func (e *partialAnnotationError) Details() map[string]any {
	return map[string]any{"partial_success": true, "annotations_uploaded": e.succeeded}
}

func addReportCommands() {
	report := &cobra.Command{Use: "report", Short: "Manage Code Insights reports"}
	var limit int
	list := &cobra.Command{Use: "list <commit>", Short: "List reports for a commit", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		values, err := commitValues(ctx(cmd), cc.client, commitPath(cc.base, cc.resolved)+"/reports", limit)
		if err != nil {
			return fail(err)
		}
		return emitListFields(values, output.ReportFields, output.ReportSummary, "No reports found.")
	}}
	list.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum reports to return")
	view := &cobra.Command{Use: "view <commit> <report-id>", Short: "View a Code Insights report", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		var out json.RawMessage
		if err := cc.client.Request(ctx(cmd), commitPath(cc.base, cc.resolved)+"/reports/"+bitbucket.EncodePathSegment(args[1]), bitbucket.RequestOptions{}, &out); err != nil {
			return fail(err)
		}
		return emitObjectFields(out, output.ReportFields, output.ReportSummary)
	}}
	var title, details, result, dataFile string
	upsert := &cobra.Command{Use: "upsert <commit> <report-id>", Short: "Create or update a Code Insights report", Long: "Create or update a Code Insights report. This is a write operation; use only when explicitly requested. Report data is visible to repository users with access; do not include secrets.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(title) == "" {
			return fail(fmt.Errorf("--title is required"))
		}
		if result != "" {
			result = strings.ToUpper(result)
			if !map[string]bool{"PASSED": true, "FAILED": true, "PENDING": true}[result] {
				return fail(fmt.Errorf("invalid --result %q", result))
			}
		}
		body := map[string]any{"title": title}
		if details != "" {
			body["details"] = details
		}
		if result != "" {
			body["result"] = result
		}
		if dataFile != "" {
			raw, err := os.ReadFile(dataFile)
			if err != nil {
				return fail(fmt.Errorf("read data file: %w", err))
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
				return fail(fmt.Errorf("--data-file must contain a JSON object"))
			}
			for k, v := range fields {
				if k != "title" && k != "details" && k != "result" {
					body[k] = v
				}
			}
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		var out json.RawMessage
		path := commitPath(cc.base, cc.resolved) + "/reports/" + bitbucket.EncodePathSegment(args[1])
		if err := cc.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &out); err != nil {
			return fail(err)
		}
		return emitObject(out, nil)
	}}
	upsert.Flags().StringVar(&title, "title", "", "Report title")
	upsert.Flags().StringVar(&details, "details", "", "Report details")
	upsert.Flags().StringVar(&result, "result", "", "Result: PASSED, FAILED, or PENDING")
	upsert.Flags().StringVar(&dataFile, "data-file", "", "JSON report fields")
	var yes bool
	remove := &cobra.Command{Use: "delete <commit> <report-id>", Short: "Delete a Code Insights report", Long: "Delete a Code Insights report. This is a write operation and requires --yes in non-interactive mode.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !yes {
			return fail(fmt.Errorf("refusing to delete report without --yes"))
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		path := commitPath(cc.base, cc.resolved) + "/reports/" + bitbucket.EncodePathSegment(args[1])
		if err := cc.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return renderValue(map[string]any{"deleted": true, "report_id": args[1]}, nil, nil, false, "")
	}}
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm deletion")
	report.AddCommand(list, view, upsert, remove)
	rootCmd.AddCommand(report)
	addAnnotationCommands()
}

func addAnnotationCommands() {
	annotation := &cobra.Command{Use: "annotation", Short: "Manage Code Insights annotations"}
	var limit int
	list := &cobra.Command{Use: "list <commit> <report-id>", Short: "List report annotations", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		path := commitPath(cc.base, cc.resolved) + "/reports/" + bitbucket.EncodePathSegment(args[1]) + "/annotations"
		values, err := commitValues(ctx(cmd), cc.client, path, limit)
		if err != nil {
			return fail(err)
		}
		return emitListFields(values, output.AnnotationFields, output.AnnotationSummary, "No annotations found.")
	}}
	list.Flags().IntVar(&limit, "limit", bitbucket.DefaultLimit, "Maximum annotations to return")
	var file string
	upsert := &cobra.Command{Use: "upsert <commit> <report-id>", Short: "Create or update report annotations", Long: "Create or update report annotations. This is a write operation; use only when explicitly requested.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if file == "" {
			return fail(fmt.Errorf("--file is required"))
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return fail(err)
		}
		var data any
		if err := json.Unmarshal(raw, &data); err != nil {
			return fail(fmt.Errorf("annotation file is not valid JSON: %w", err))
		}
		items := []any{}
		switch v := data.(type) {
		case map[string]any:
			items = append(items, v)
		case []any:
			items = v
		default:
			return fail(fmt.Errorf("annotation file must contain one object or an array"))
		}
		if len(items) == 0 {
			return fail(fmt.Errorf("annotation file contains no annotations"))
		}
		seenIDs := map[string]bool{}
		for i, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				return fail(fmt.Errorf("annotation %d must be a JSON object", i+1))
			}
			id, _ := obj["external_id"].(string)
			if strings.TrimSpace(id) == "" {
				return fail(fmt.Errorf("annotation %d requires external_id", i+1))
			}
			if seenIDs[id] {
				return fail(fmt.Errorf("duplicate annotation external_id %q", id))
			}
			seenIDs[id] = true
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		endpoint := commitPath(cc.base, cc.resolved) + "/reports/" + bitbucket.EncodePathSegment(args[1]) + "/annotations"
		succeeded := 0
		for start := 0; start < len(items); start += 100 {
			end := start + 100
			if end > len(items) {
				end = len(items)
			}
			batch := items[start:end]
			// Bitbucket's bulk endpoint accepts a JSON array (up to 100
			// annotations per request) and uses external_id for upsert.
			body := batch
			var out json.RawMessage
			if err := cc.client.Request(ctx(cmd), endpoint, bitbucket.RequestOptions{Method: http.MethodPost, Body: body}, &out); err != nil {
				if succeeded > 0 {
					return fail(&partialAnnotationError{succeeded: succeeded, err: err})
				}
				return fail(err)
			}
			succeeded += len(batch)
		}
		return renderValue(map[string]any{"uploaded": succeeded}, nil, nil, false, "")
	}}
	upsert.Flags().StringVar(&file, "file", "", "JSON annotation object or array")
	var yes bool
	remove := &cobra.Command{Use: "delete <commit> <report-id> <annotation-id>", Short: "Delete a report annotation", Long: "Delete a report annotation. This is a write operation and requires --yes in non-interactive mode.", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if !yes {
			return fail(fmt.Errorf("refusing to delete annotation without --yes"))
		}
		cc, err := newCommitContext(cmd, args[0], true)
		if err != nil {
			return fail(err)
		}
		path := commitPath(cc.base, cc.resolved) + "/reports/" + bitbucket.EncodePathSegment(args[1]) + "/annotations/" + bitbucket.EncodePathSegment(args[2])
		if err := cc.client.Request(ctx(cmd), path, bitbucket.RequestOptions{Method: http.MethodDelete}, nil); err != nil {
			return fail(err)
		}
		return renderValue(map[string]any{"deleted": true, "annotation_id": args[2]}, nil, nil, false, "")
	}}
	remove.Flags().BoolVar(&yes, "yes", false, "Confirm deletion")
	annotation.AddCommand(list, upsert, remove)
	rootCmd.AddCommand(annotation)
}
