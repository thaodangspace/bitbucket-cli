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

type fieldSet struct{ fields []string }

func (f fieldSet) AllowedFields() []string { return append([]string(nil), f.fields...) }

func (f fieldSet) Project(value any, requested []string) (any, error) {
	if len(requested) == 0 {
		return value, nil
	}
	allowed := make(map[string]bool, len(f.fields))
	for _, name := range f.fields {
		allowed[name] = true
	}
	for _, path := range requested {
		root := strings.SplitN(path, ".", 2)[0]
		if path == "*" {
			return value, nil
		}
		if !allowed[root] {
			return nil, fmt.Errorf("unknown JSON field %q (valid fields: %s)", path, strings.Join(f.fields, ", "))
		}
	}
	return projectValue(value, requested), nil
}

func projectValue(value any, requested []string) any {
	if items, ok := value.([]any); ok {
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = projectValue(item, requested)
		}
		return out
	}
	m, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := make(map[string]any, len(requested))
	for _, path := range requested {
		if path == "*" {
			return value
		}
		parts := strings.Split(path, ".")
		copyPath(out, m, parts)
	}
	return out
}

func copyPath(dst map[string]any, src map[string]any, parts []string) {
	if len(parts) == 0 {
		return
	}
	value, ok := src[parts[0]]
	if !ok {
		return
	}
	if len(parts) == 1 {
		dst[parts[0]] = value
		return
	}
	child, ok := value.(map[string]any)
	if !ok {
		dst[parts[0]] = value
		return
	}
	childDst, _ := dst[parts[0]].(map[string]any)
	if childDst == nil {
		childDst = make(map[string]any)
		dst[parts[0]] = childDst
	}
	copyPath(childDst, child, parts[1:])
}

func fields(names ...string) FieldSet {
	sort.Strings(names)
	return fieldSet{fields: names}
}

// Documented field sets for the wrapped read commands.
var (
	PullRequestFields = fields("author", "close_source_branch", "comment_count", "created_on", "description", "destination", "id", "links", "merge_commit", "reviewers", "source", "state", "task_count", "title", "updated_on")
	CommentFields     = fields("content", "created_on", "deleted", "id", "links", "parent", "updated_on", "user")
	CommitFields      = fields("author", "date", "hash", "links", "message", "repository")
	BranchFields      = fields("links", "merge_strategies", "name", "target", "type")
	PipelineFields    = fields("build_number", "completed_on", "created_on", "creator", "duration_in_seconds", "links", "repository", "state", "steps", "target", "trigger", "uuid")
	RepoFields        = fields("description", "full_name", "is_private", "links", "mainbranch", "name", "owner", "project", "scm", "size", "updated_on", "uuid", "website")
)
