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

// PullRequestSelector retains repository context when a URL supplies it.
type PullRequestSelector struct {
	Repository *Repository
	ID         int
	// Branch is a source branch selector that must be resolved against the
	// repository's pull requests before an API request can be made.
	Branch string
}

// PipelineSelector distinguishes UUIDs from numeric build numbers and retains
// repository context from URLs.
type PipelineSelector struct {
	Repository  *Repository
	UUID        string
	BuildNumber *int
}

// RepositorySelector accepts workspace/repo, Bitbucket web/API URLs, and an
// explicit repository reference. An empty value is invalid; callers use their
// git-remote fallback before calling this function.
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

// PullRequest accepts an ID or a Bitbucket pull request URL.
func PullRequest(value string) (PullRequestSelector, error) {
	raw := strings.TrimSpace(value)
	if id, err := strconv.Atoi(raw); err == nil && id > 0 {
		return PullRequestSelector{ID: id}, nil
	}
	if u, err := url.Parse(raw); err == nil && (strings.EqualFold(u.Hostname(), "bitbucket.org") || strings.EqualFold(u.Hostname(), "api.bitbucket.org")) {
		parts := splitPath(u.Path)
		if len(parts) >= 4 && parts[0] == "2.0" && parts[1] == "repositories" {
			parts = parts[2:]
		}
		for i := range parts {
			if parts[i] == "pull-requests" && i+1 < len(parts) {
				if id, err := strconv.Atoi(parts[i+1]); err == nil && id > 0 && i >= 2 {
					repo, rerr := validRepository(parts[0], parts[1], raw)
					if rerr != nil {
						return PullRequestSelector{}, rerr
					}
					return PullRequestSelector{Repository: &repo, ID: id}, nil
				}
			}
		}
	}
	return PullRequestSelector{}, &Error{"pull request", value}
}

var pipelineUUID = regexp.MustCompile(`^[{]?[0-9a-fA-F-]{8,}[}]?$`)
var integer = regexp.MustCompile(`^\d+$`)

// Pipeline accepts a UUID-like selector, a numeric build number, an opaque
// identifier, or a Bitbucket pipeline URL.
func Pipeline(value string) (PipelineSelector, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return PipelineSelector{}, &Error{"pipeline", value}
	}
	if u, err := url.Parse(raw); err == nil && (strings.EqualFold(u.Hostname(), "bitbucket.org") || strings.EqualFold(u.Hostname(), "api.bitbucket.org")) {
		parts := splitPath(u.Path)
		if len(parts) >= 4 && parts[0] == "2.0" && parts[1] == "repositories" {
			parts = parts[2:]
		}
		if len(parts) >= 2 && strings.HasPrefix(u.Fragment, "!/results/") {
			repo, rerr := validRepository(parts[0], parts[1], raw)
			if rerr != nil {
				return PipelineSelector{}, rerr
			}
			id := strings.TrimPrefix(u.Fragment, "!/results/")
			if id != "" {
				return PipelineSelector{Repository: &repo, UUID: id}, nil
			}
		}
		for i, part := range parts {
			if part == "pipelines" && i+1 < len(parts) {
				if i < 2 {
					return PipelineSelector{}, &Error{"pipeline", value}
				}
				repo, rerr := validRepository(parts[0], parts[1], raw)
				if rerr != nil {
					return PipelineSelector{}, rerr
				}
				id := parts[i+1]
				if integer.MatchString(id) {
					n, _ := strconv.Atoi(id)
					return PipelineSelector{Repository: &repo, BuildNumber: &n}, nil
				}
				return PipelineSelector{Repository: &repo, UUID: id}, nil
			}
		}
	}
	if integer.MatchString(raw) {
		n, _ := strconv.Atoi(raw)
		return PipelineSelector{BuildNumber: &n}, nil
	}
	if pipelineUUID.MatchString(raw) || !strings.ContainsAny(raw, "/?#") {
		return PipelineSelector{UUID: raw}, nil
	}
	return PipelineSelector{}, &Error{"pipeline", value}
}
