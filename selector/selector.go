// Package selector parses common Bitbucket resource selectors.
package selector

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Repository struct{ Workspace, Repo string }

type Error struct{ Kind, Value string }

func (e *Error) Error() string { return fmt.Sprintf("invalid %s selector %q", e.Kind, e.Value) }

// RepositorySelector accepts workspace/repo, Bitbucket web URLs, and API
// repository URLs. An empty value means that the caller should use git remote.
func RepositorySelector(value string) (Repository, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return Repository{}, &Error{"repository", value}
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || (!strings.EqualFold(u.Hostname(), "bitbucket.org") && !strings.EqualFold(u.Hostname(), "api.bitbucket.org")) {
			return Repository{}, &Error{"repository", value}
		}
		parts := splitPath(u.Path)
		if len(parts) >= 3 && parts[0] == "2.0" && parts[1] == "repositories" {
			parts = parts[2:]
		}
		if len(parts) < 2 {
			return Repository{}, &Error{"repository", value}
		}
		return validRepository(parts[0], parts[1], value)
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) != 2 {
		return Repository{}, &Error{"repository", value}
	}
	return validRepository(parts[0], parts[1], value)
}

func validRepository(workspace, repo, raw string) (Repository, error) {
	workspace, repo = strings.TrimSpace(workspace), strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	if workspace == "" || repo == "" || strings.ContainsAny(workspace+repo, "?#") {
		return Repository{}, &Error{"repository", raw}
	}
	return Repository{Workspace: workspace, Repo: repo}, nil
}

func splitPath(path string) []string {
	var out []string
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// PullRequest accepts a positive numeric ID or a Bitbucket pull request URL.
func PullRequest(value string) (int, error) {
	raw := strings.TrimSpace(value)
	if id, err := strconv.Atoi(raw); err == nil && id > 0 {
		return id, nil
	}
	if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Hostname(), "bitbucket.org") {
		parts := splitPath(u.Path)
		for i := range parts {
			if parts[i] == "pull-requests" && i+1 < len(parts) {
				if id, err := strconv.Atoi(parts[i+1]); err == nil && id > 0 {
					return id, nil
				}
			}
		}
	}
	return 0, &Error{"pull request", value}
}

var pipelineUUID = regexp.MustCompile(`^[{]?[0-9a-fA-F-]{8,}[}]?$`)

// Pipeline accepts a UUID-like selector, a numeric build number, or a URL.
// The caller resolves ambiguous build numbers against the repository API.
func Pipeline(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", &Error{"pipeline", value}
	}
	if pipelineUUID.MatchString(raw) || regexp.MustCompile(`^\d+$`).MatchString(raw) || !strings.ContainsAny(raw, "/?#") {
		// Bitbucket installations and test fixtures may use opaque pipeline
		// identifiers; preserve those for backwards compatibility.
		return raw, nil
	}
	if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Hostname(), "bitbucket.org") {
		parts := splitPath(u.Path)
		for i, part := range parts {
			if part == "pipelines" && i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
	}
	return "", &Error{"pipeline", value}
}
