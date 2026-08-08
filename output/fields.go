package output

import (
	"fmt"
	"sort"
	"strings"
)

// FieldSet describes the documented fields a command exposes through --json.
// Implementations deliberately project instead of returning an arbitrary API
// object so scripts can rely on a stable output contract.
type FieldSet interface {
	AllowedFields() []string
	Project(any, []string) (any, error)
}

type fieldNode struct {
	leaf     bool
	children map[string]*fieldNode
}

type fieldSet struct {
	fields []string
	root   map[string]*fieldNode
}

func (f fieldSet) AllowedFields() []string { return append([]string(nil), f.fields...) }

func (f fieldSet) Project(value any, requested []string) (any, error) {
	if len(requested) == 0 {
		return value, nil
	}
	paths := make([][]string, 0, len(requested))
	for _, path := range requested {
		if path == "*" {
			return value, nil
		}
		parts := strings.Split(path, ".")
		if err := validatePath(f.root, parts, path); err != nil {
			return nil, err
		}
		paths = append(paths, parts)
	}
	return project(value, paths), nil
}

func validatePath(root map[string]*fieldNode, parts []string, path string) error {
	nodes := root
	for i, part := range parts {
		node, ok := nodes[part]
		if !ok {
			return fmt.Errorf("unknown JSON field %q (valid fields: %s)", path, strings.Join(sortedNodeNames(root), ", "))
		}
		if i == len(parts)-1 {
			return nil
		}
		if node.children == nil {
			return fmt.Errorf("invalid JSON field %q: %q is a scalar field", path, strings.Join(parts[:i+1], "."))
		}
		nodes = node.children
	}
	return nil
}

func project(value any, paths [][]string) any {
	if items, ok := value.([]any); ok {
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = project(item, paths)
		}
		return out
	}
	m, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := make(map[string]any)
	for _, path := range paths {
		copyPath(out, m, path)
	}
	return out
}

func copyPath(dst, src map[string]any, parts []string) {
	if len(parts) == 0 {
		return
	}
	value, ok := src[parts[0]]
	if !ok {
		return
	} // A valid but missing field is omitted, like jq.
	if len(parts) == 1 {
		dst[parts[0]] = value
		return
	}
	childPaths := [][]string{parts[1:]}
	if existing, ok := dst[parts[0]]; ok {
		// A root selection wins over a nested selection.
		if _, full := existing.(map[string]any); !full {
			return
		}
	}
	if arr, ok := value.([]any); ok {
		projected := make([]any, len(arr))
		for i, item := range arr {
			projected[i] = project(item, childPaths)
		}
		dst[parts[0]] = projected
		return
	}
	child, ok := value.(map[string]any)
	if !ok {
		return
	}
	childDst, _ := dst[parts[0]].(map[string]any)
	if childDst == nil {
		childDst = make(map[string]any)
		dst[parts[0]] = childDst
	}
	copyPath(childDst, child, parts[1:])
}

func sortedNodeNames(nodes map[string]*fieldNode) []string {
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fields(names ...string) FieldSet {
	root := make(map[string]*fieldNode)
	for _, path := range names {
		parts := strings.Split(path, ".")
		nodes := root
		for i, part := range parts {
			node := nodes[part]
			if node == nil {
				node = &fieldNode{}
				nodes[part] = node
			}
			if i == len(parts)-1 {
				node.leaf = true
			} else {
				if node.children == nil {
					node.children = make(map[string]*fieldNode)
				}
				nodes = node.children
			}
		}
	}
	sort.Strings(names)
	return fieldSet{fields: names, root: root}
}

// Documented field sets for the wrapped read commands. Nested paths are
// explicit so selecting reviewers.display_name cannot leak other fields.
var (
	PullRequestFields     = fields("author", "author.display_name", "close_source_branch", "comment_count", "created_on", "description", "destination", "destination.branch", "destination.branch.name", "id", "links", "merge_commit", "reviewers", "reviewers.display_name", "reviewers.nickname", "source", "source.branch", "source.branch.name", "state", "task_count", "title", "updated_on")
	CommentFields         = fields("content", "content.raw", "created_on", "deleted", "id", "inline", "inline.from", "inline.path", "inline.to", "links", "parent", "parent.id", "pending", "resolution", "resolution.created_on", "resolution.type", "resolution.user", "resolution.user.display_name", "updated_on", "user", "user.display_name")
	TaskFields            = fields("comment", "comment.id", "content", "content.raw", "created_on", "creator", "creator.display_name", "creator.nickname", "id", "links", "pending", "resolved_by", "resolved_by.display_name", "resolved_on", "state", "updated_on")
	CommitFields          = fields("author", "author.display_name", "comments", "date", "hash", "links", "message", "reports", "repository", "requested_selector", "resolved_hash", "statuses")
	BranchFields          = fields("links", "merge_strategies", "name", "requested_target", "resolved_target", "target", "target.hash", "target.author", "type")
	TagFields             = fields("links", "name", "message", "target", "target.hash", "target.author", "type", "requested_target", "resolved_target")
	RestrictionFields     = fields("branch_match_kind", "branch_type", "groups", "id", "kind", "links", "pattern", "users", "value")
	AccountFields         = fields("account_id", "display_name", "nickname", "type", "uuid", "username", "links")
	BranchingModelFields  = fields("branch_types", "branch_types.kind", "branch_types.prefix", "branch_types.enabled", "development", "development.name", "development.enabled", "development.use_mainbranch", "production", "production.name", "production.enabled", "production.use_mainbranch", "default_branch_deletion")
	PipelineFields        = fields("build_number", "completed_on", "created_on", "creator", "creator.display_name", "duration_in_seconds", "links", "repository", "state", "state.name", "state.result", "state.result.name", "steps", "steps.name", "steps.state", "target", "target.ref_name", "target.commit", "target.commit.hash", "trigger", "trigger.name", "uuid")
	RepoFields            = fields("description", "fork_policy", "full_name", "has_issues", "has_wiki", "is_fork", "is_private", "links", "links.clone", "links.clone.href", "links.clone.name", "mainbranch", "mainbranch.name", "name", "owner", "owner.display_name", "parent", "project", "scm", "size", "updated_on", "uuid", "website")
	CommitStatusFields    = fields("created_on", "description", "key", "links", "name", "refname", "state", "updated_on", "url", "uuid")
	ReportFields          = fields("created_on", "details", "external_id", "link", "logo_url", "result", "reporter", "report_type", "title", "updated_on", "uuid", "data")
	AnnotationFields      = fields("annotation_type", "external_id", "file_path", "line", "link", "message", "path", "severity", "summary", "result", "start_line", "end_line", "created_on", "updated_on")
	WorkspaceFields       = fields("name", "slug", "uuid", "type", "links", "links.html", "links.self", "website", "members", "members.user", "members.user.display_name", "members.user.nickname", "members.user.uuid", "projects", "projects.key", "projects.name", "projects.uuid", "email")
	WorkspaceMemberFields = fields("user", "user.display_name", "user.nickname", "user.uuid", "user.account_id", "user.email", "role", "links", "type", "email")
	ProjectFields         = fields("key", "name", "uuid", "type", "description", "is_private", "owner", "owner.display_name", "links", "links.html", "links.self", "created_on", "updated_on", "repositories", "repositories.metadata", "repositories.metadata.workspace", "repositories.metadata.project", "repositories.metadata.filter", "repositories.values", "email")
	PermissionFields      = fields("type", "permission", "raw_permission", "user", "user.display_name", "user.nickname", "user.uuid", "user.account_id", "group", "group.name", "group.slug", "repository", "repository.name", "repository.full_name", "repository.uuid", "links", "before", "after", "before_raw", "after_raw", "changed", "operation", "target", "principal", "metadata", "metadata.workspace", "metadata.repository", "email")
)
