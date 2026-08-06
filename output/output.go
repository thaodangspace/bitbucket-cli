// Package output renders command results as JSON (default) or human-readable
// text (--pretty), and provides a single structured error envelope.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"gopkg.in/yaml.v3"
)

// RenderJSON writes v as indented JSON followed by a newline.
func RenderJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// RenderRaw writes compact JSON. It is useful when a consumer needs a
// machine-readable value without the default indentation.
func RenderRaw(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}

// RenderYAML writes v as YAML followed by a newline.
func RenderYAML(w io.Writer, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// RenderLines writes one string per line. When lines is empty, it writes the
// provided empty message.
func RenderLines(w io.Writer, lines []string, emptyMsg string) error {
	if len(lines) == 0 {
		_, err := fmt.Fprintln(w, emptyMsg)
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// errorEnvelope is the structured error written to stderr.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string         `json:"message"`
	Status  int            `json:"status,omitempty"`
	Method  string         `json:"method,omitempty"`
	URL     string         `json:"url,omitempty"`
	Excerpt string         `json:"excerpt,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// WriteError renders err as a JSON envelope to w. HTTP errors include their
// status/method/url/excerpt; other errors carry only a message.
func WriteError(w io.Writer, err error) {
	body := errorBody{Message: err.Error()}

	var he *bitbucket.HTTPError
	if errors.As(err, &he) {
		body.Status = he.Status
		body.Method = he.Method
		body.URL = he.URL
		body.Excerpt = he.Excerpt
	}
	if detailed, ok := err.(interface{ Details() map[string]any }); ok {
		body.Details = detailed.Details()
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Best effort: if encoding the envelope fails, fall back to a plain line.
	if encErr := enc.Encode(errorEnvelope{Error: body}); encErr != nil {
		fmt.Fprintln(w, err.Error())
	}
}
