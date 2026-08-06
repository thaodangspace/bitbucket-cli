package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

type attachedFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type attachResult struct {
	PullRequestID int            `json:"pullrequest_id"`
	Files         []attachedFile `json:"files"`
	Comment       any            `json:"comment,omitempty"`
}

var (
	attachFiles   []string
	attachMessage string
)

func init() {
	attachCmd := &cobra.Command{
		Use:   "attach <id>",
		Short: "Upload files and link them from a pull request comment",
		Long: "Upload one or more local files to Bitbucket repository Downloads and " +
			"post a pull request comment with markdown links. This is a write operation; " +
			"use only when the user has asked to attach files to the pull request. " +
			"Bitbucket Cloud does not support native pull request attachments.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := parsePullRequestSelector(args[0])
			if err != nil {
				return fail(err)
			}
			id := selected.ID
			uploads, names, err := validateAttachFiles(attachFiles)
			if err != nil {
				return fail(err)
			}

			cfg, client, err := newClient()
			if err != nil {
				return fail(err)
			}
			ref, base, err := resolveRepoFor(cfg, selected.Repository)
			if err != nil {
				return fail(err)
			}

			attached := make([]attachedFile, 0, len(names))
			for _, name := range names {
				attached = append(attached, attachedFile{
					Name: name,
					URL:  downloadURL(ref.Workspace, ref.RepoSlug, name),
				})
			}

			uploadPath := fmt.Sprintf("%s/downloads", base)
			if err := client.UploadFiles(ctx(cmd), uploadPath, "files", uploads, nil); err != nil {
				return fail(err)
			}

			commentBody := attachCommentBody(attachMessage, attached)
			payload := map[string]any{
				"content": map[string]any{"raw": commentBody},
			}
			var raw json.RawMessage
			commentPath := fmt.Sprintf("%s/pullrequests/%d/comments", base, id)
			if err := client.Request(ctx(cmd), commentPath, bitbucket.RequestOptions{Method: http.MethodPost, Body: payload}, &raw); err != nil {
				return fail(fmt.Errorf("files uploaded to repository Downloads, but posting PR comment failed: %w", err))
			}

			if flagPretty {
				label := "files"
				if len(attached) == 1 {
					label = "file"
				}
				_, err = fmt.Fprintf(os.Stdout, "Attached %d %s to pull request #%d.\n", len(attached), label, id)
				return err
			}

			var comment any
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &comment); err != nil {
					return fail(err)
				}
			}
			return output.RenderJSON(os.Stdout, attachResult{PullRequestID: id, Files: attached, Comment: comment})
		},
	}
	attachCmd.Flags().StringArrayVar(&attachFiles, "file", nil, "Local file to upload (repeatable, required)")
	attachCmd.Flags().StringVar(&attachMessage, "message", "", "Optional markdown text before the attachment links")

	prCmd.AddCommand(attachCmd)
}

func validateAttachFiles(paths []string) ([]bitbucket.UploadFile, []string, error) {
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("Provide at least one --file.")
	}
	uploads := make([]bitbucket.UploadFile, 0, len(paths))
	names := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, nil, fmt.Errorf("--file cannot be empty")
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, fmt.Errorf("stat upload file %q: %w", path, err)
		}
		if info.IsDir() {
			return nil, nil, fmt.Errorf("upload file %q is a directory", path)
		}
		name := filepath.Base(path)
		if _, ok := seen[name]; ok {
			return nil, nil, fmt.Errorf("duplicate upload filename %q; Bitbucket Downloads replaces artifacts with the same name", name)
		}
		seen[name] = struct{}{}
		uploads = append(uploads, bitbucket.UploadFile{Path: path, Name: name})
		names = append(names, name)
	}
	return uploads, names, nil
}

func downloadURL(workspace, repo, filename string) string {
	return fmt.Sprintf("https://bitbucket.org/%s/%s/downloads/%s",
		bitbucket.EncodePathSegment(workspace),
		bitbucket.EncodePathSegment(repo),
		bitbucket.EncodePathSegment(filename))
}

func attachCommentBody(message string, files []attachedFile) string {
	var b strings.Builder
	message = strings.TrimSpace(message)
	if message != "" {
		b.WriteString(message)
		b.WriteString("\n\n")
	}
	b.WriteString("Attached files:\n")
	for _, file := range files {
		b.WriteString(fmt.Sprintf("- [%s](%s)\n", file.Name, file.URL))
	}
	return strings.TrimRight(b.String(), "\n")
}
