package output

import (
	"fmt"
	"strings"
)

// These helpers reproduce the exact text summaries emitted by the original Pi
// extension's tools, operating on decoded JSON objects (map[string]any).

func str(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// PullRequestSummary renders "#<id> <title> [<state>]".
func PullRequestSummary(pr map[string]any) string {
	id := "?"
	if v, ok := pr["id"]; ok {
		id = fmt.Sprintf("%v", numberish(v))
	}
	title, ok := str(pr, "title")
	if !ok || title == "" {
		title = "(untitled)"
	}
	state, ok := str(pr, "state")
	if !ok || state == "" {
		state = "unknown"
	}
	return fmt.Sprintf("#%s %s [%s]", id, title, state)
}

// CommentSummary renders "#<id>: <raw>".
func CommentSummary(comment map[string]any) string {
	id := "?"
	if v, ok := comment["id"]; ok {
		id = fmt.Sprintf("%v", numberish(v))
	}
	raw := ""
	if content, ok := comment["content"].(map[string]any); ok {
		raw, _ = str(content, "raw")
	}
	return fmt.Sprintf("#%s: %s", id, raw)
}

// TaskSummary renders "#<id>: <raw> [<state>]".
func TaskSummary(task map[string]any) string {
	id := "?"
	if v, ok := task["id"]; ok {
		id = fmt.Sprintf("%v", numberish(v))
	}
	body := ""
	if content, ok := task["content"].(map[string]any); ok {
		body, _ = str(content, "raw")
	}
	state, _ := str(task, "state")
	if state == "" {
		state = "UNKNOWN"
	}
	return fmt.Sprintf("#%s: %s [%s]", id, body, state)
}

// CommitSummary renders "<hash[:12]> <message>" trimmed.
func CommitSummary(commit map[string]any) string {
	hash := "unknown"
	if h, ok := str(commit, "hash"); ok && h != "" {
		if len(h) > 12 {
			h = h[:12]
		}
		hash = h
	}
	msg, _ := str(commit, "message")
	out := hash + " " + msg
	// Trim a trailing space when message is empty, matching the source.
	if msg == "" {
		return hash
	}
	return out
}

// BranchSummary renders the branch name, falling back to target hash.
func BranchSummary(branch map[string]any) string {
	if name, ok := str(branch, "name"); ok && name != "" {
		return name
	}
	if target, ok := branch["target"].(map[string]any); ok {
		if hash, ok := str(target, "hash"); ok && hash != "" {
			return hash
		}
	}
	return "unknown"
}

// PipelineSummary renders "#<build_number> <state> [<result>] <branch> <trigger> <duration>".
// For running pipelines (no result), the result field is omitted.
// Duration is formatted as "XmYs" or "Ys" or "-" when unavailable.
func PipelineSummary(pipeline map[string]any) string {
	buildNum := "?"
	if v, ok := pipeline["build_number"]; ok {
		buildNum = fmt.Sprintf("%v", numberish(v))
	}

	stateName := "UNKNOWN"
	if state, ok := pipeline["state"].(map[string]any); ok {
		if s, ok2 := str(state, "name"); ok2 && s != "" {
			stateName = s
		}
	}

	resultName := ""
	if state, ok := pipeline["state"].(map[string]any); ok {
		if result, ok2 := state["result"].(map[string]any); ok2 {
			if s, ok3 := str(result, "name"); ok3 && s != "" {
				resultName = s
			}
		}
	}

	branch := "?"
	if target, ok := pipeline["target"].(map[string]any); ok {
		if s, ok2 := str(target, "ref_name"); ok2 && s != "" {
			branch = s
		}
	}

	trigger := "unknown"
	if tr, ok := pipeline["trigger"].(map[string]any); ok {
		if s, ok2 := str(tr, "name"); ok2 && s != "" {
			trigger = s
		}
	}

	dur := "-"
	if d, ok := pipeline["duration_in_seconds"]; ok {
		if seconds, ok2 := d.(float64); ok2 && seconds > 0 {
			m := int(seconds) / 60
			s := int(seconds) % 60
			if m > 0 {
				dur = fmt.Sprintf("%dm%ds", m, s)
			} else {
				dur = fmt.Sprintf("%ds", s)
			}
		}
	}

	if resultName != "" {
		return fmt.Sprintf("#%s %s %s %s %s %s", buildNum, stateName, resultName, branch, trigger, dur)
	}
	return fmt.Sprintf("#%s %s %s %s %s", buildNum, stateName, branch, trigger, dur)
}

// RepoSummary renders "Repository: <full_name|name|unknown>".
func RepoSummary(repo map[string]any) string {
	if full, ok := str(repo, "full_name"); ok && full != "" {
		return "Repository: " + full
	}
	if name, ok := str(repo, "name"); ok && name != "" {
		return "Repository: " + name
	}
	return "Repository: unknown"
}

// StepGlyph returns a single-character glyph representing a pipeline step's
// state: ✓ (successful), ✗ (failed), ● (pending/in-progress), ○ (other).
func StepGlyph(state map[string]any) string {
	if result, ok := state["result"].(map[string]any); ok {
		switch result["name"] {
		case "SUCCESSFUL":
			return "✓"
		case "FAILED":
			return "✗"
		case "STOPPED", "EXPIRED":
			return "○"
		}
	}
	switch state["name"] {
	case "PENDING", "IN_PROGRESS":
		return "●"
	default:
		return "○"
	}
}

// PipelineGetSummary renders a multi-line summary of a single pipeline with
// its steps. The pipeline map is the pipeline API response; steps is the list
// of step objects (may be empty).
func PipelineGetSummary(pipeline map[string]any, steps []map[string]any) string {
	var b strings.Builder

	buildNum := "?"
	if v, ok := pipeline["build_number"]; ok {
		buildNum = fmt.Sprintf("%v", numberish(v))
	}
	fmt.Fprintf(&b, "Pipeline #%s\n", buildNum)

	// State line.
	stateName := "UNKNOWN"
	resultName := ""
	if state, ok := pipeline["state"].(map[string]any); ok {
		if s, ok2 := str(state, "name"); ok2 && s != "" {
			stateName = s
		}
		if result, ok2 := state["result"].(map[string]any); ok2 {
			if s, ok3 := str(result, "name"); ok3 && s != "" {
				resultName = s
			}
		}
	}
	if resultName != "" {
		fmt.Fprintf(&b, "  State: %s (%s)\n", stateName, resultName)
	} else {
		fmt.Fprintf(&b, "  State: %s\n", stateName)
	}

	// Branch / commit.
	if target, ok := pipeline["target"].(map[string]any); ok {
		if s, ok2 := str(target, "ref_name"); ok2 {
			fmt.Fprintf(&b, "  Branch: %s\n", s)
		}
		if commit, ok2 := target["commit"].(map[string]any); ok2 {
			if hash, ok3 := str(commit, "hash"); ok3 && hash != "" {
				if len(hash) > 12 {
					hash = hash[:12]
				}
				fmt.Fprintf(&b, "  Commit: %s\n", hash)
			}
		}
	}

	// Trigger.
	if tr, ok := pipeline["trigger"].(map[string]any); ok {
		if s, ok2 := str(tr, "name"); ok2 && s != "" {
			fmt.Fprintf(&b, "  Trigger: %s\n", s)
		}
	}

	// Created.
	if s, ok := str(pipeline, "created_on"); ok && s != "" {
		fmt.Fprintf(&b, "  Created: %s\n", s)
	}

	// Duration.
	if d, ok := pipeline["duration_in_seconds"]; ok {
		if seconds, ok2 := d.(float64); ok2 && seconds > 0 {
			m := int(seconds) / 60
			s := int(seconds) % 60
			if m > 0 {
				fmt.Fprintf(&b, "  Duration: %dm%ds\n", m, s)
			} else {
				fmt.Fprintf(&b, "  Duration: %ds\n", s)
			}
		}
	}

	// Steps section.
	fmt.Fprintf(&b, "\nSteps (%d):\n", len(steps))
	if len(steps) == 0 {
		fmt.Fprint(&b, "  (none)\n")
	} else {
		for _, step := range steps {
			glyph := StepGlyph(stepState(step))
			name, _ := str(step, "name")
			stepStateName := ""
			stepResultName := ""
			if st, ok := step["state"].(map[string]any); ok {
				if s, ok2 := str(st, "name"); ok2 {
					stepStateName = s
				}
				if result, ok2 := st["result"].(map[string]any); ok2 {
					if s, ok3 := str(result, "name"); ok3 {
						stepResultName = s
					}
				}
			}
			if stepResultName != "" {
				fmt.Fprintf(&b, "  %s %-30s (%s %s)\n", glyph, name, stepStateName, stepResultName)
			} else {
				fmt.Fprintf(&b, "  %s %-30s (%s)\n", glyph, name, stepStateName)
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// stepState extracts the "state" sub-object from a step map, or returns nil.
func stepState(step map[string]any) map[string]any {
	if state, ok := step["state"].(map[string]any); ok {
		return state
	}
	return nil
}

// numberish normalizes JSON numbers (float64) to integers for display when
// they are whole, so PR ids render as "12" not "12".
func numberish(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}

// CommitStatusSummary renders the state, key, name, and target URL.
func CommitStatusSummary(status map[string]any) string {
	state, _ := str(status, "state")
	if state == "" {
		state = "UNKNOWN"
	}
	key, _ := str(status, "key")
	name, _ := str(status, "name")
	target, _ := str(status, "url")
	updated, _ := str(status, "updated_on")
	return fmt.Sprintf("[%s] %s %s %s %s", state, key, name, updated, target)
}

// ReportSummary renders a concise Code Insights report line.
func ReportSummary(report map[string]any) string {
	title, _ := str(report, "title")
	result, _ := str(report, "result")
	if result == "" {
		result = "UNKNOWN"
	}
	return fmt.Sprintf("[%s] %s", result, title)
}

// AnnotationSummary renders a concise Code Insights annotation line.
func AnnotationSummary(annotation map[string]any) string {
	path, _ := str(annotation, "path")
	if path == "" {
		path, _ = str(annotation, "file_path")
	}
	line := annotation["line"]
	summary, _ := str(annotation, "summary")
	if summary == "" {
		summary, _ = str(annotation, "message")
	}
	return fmt.Sprintf("%s:%v %s", path, numberish(line), summary)
}
