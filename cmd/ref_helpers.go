package cmd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

var refNamePattern = regexp.MustCompile(`^[^\x00-\x1f\x7f?#]+$`)

func validateRefName(kind, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || !refNamePattern.MatchString(name) {
		return fmt.Errorf("invalid %s name %q", kind, name)
	}
	return nil
}

func refPath(base, kind, name string) string {
	return fmt.Sprintf("%s/refs/%s/%s", base, kind, bitbucket.EncodePathSegment(name))
}

func isNotFound(err error) bool {
	e, ok := err.(*bitbucket.HTTPError)
	return ok && e.Status == http.StatusNotFound
}

func hashLooksComplete(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// resolveRefTarget turns a branch, tag, abbreviated commit, or full commit
// into the full immutable commit hash accepted by the refs API.
func resolveRefTarget(ctx context.Context, client *bitbucket.Client, base, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target is required")
	}
	if hashLooksComplete(target) {
		return target, nil
	}

	// Branches are tried first because branch names are the common case and
	// may contain slashes. The tag lookup also makes tag targets explicit.
	for _, kind := range []string{"branches", "tags"} {
		var value struct {
			Target struct {
				Hash string `json:"hash"`
			} `json:"target"`
			Hash string `json:"hash"`
		}
		err := client.Request(ctx, refPath(base, kind, target), bitbucket.RequestOptions{}, &value)
		if err == nil {
			if value.Target.Hash != "" {
				return value.Target.Hash, nil
			}
			if value.Hash != "" {
				return value.Hash, nil
			}
			return "", fmt.Errorf("%s %q did not contain a target commit hash", strings.TrimSuffix(kind, "es"), target)
		}
		if !isNotFound(err) {
			return "", err
		}
	}

	// Bitbucket accepts commit specifications at /commits/{spec}; use it for
	// abbreviated hashes and for callers that pass a commit-ish explicitly.
	var commit struct {
		Hash string `json:"hash"`
	}
	if err := client.Request(ctx, base+"/commits/"+bitbucket.EncodePathSegment(target), bitbucket.RequestOptions{}, &commit); err != nil {
		return "", fmt.Errorf("resolve target %q: %w", target, err)
	}
	if commit.Hash == "" {
		return "", fmt.Errorf("target %q did not resolve to a commit hash", target)
	}
	return commit.Hash, nil
}

func ensureRefAbsent(ctx context.Context, client *bitbucket.Client, path, kind, name string) error {
	var existing json.RawMessage
	if err := client.Request(ctx, path, bitbucket.RequestOptions{}, &existing); err == nil {
		return fmt.Errorf("%s %q already exists; refusing to overwrite it", kind, name)
	} else if !isNotFound(err) {
		return err
	}
	return nil
}

func addTargetMetadata(raw json.RawMessage, requested, resolved string) json.RawMessage {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		value = map[string]any{}
	}
	value["requested_target"] = requested
	value["resolved_target"] = resolved
	encoded, _ := json.Marshal(value)
	return encoded
}
