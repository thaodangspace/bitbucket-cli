package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Git is the small git surface used by PR checkout/current. Keeping it
// injectable makes command tests independent of the developer's repository.
type Git interface {
	Root(ctx context.Context) (string, error)
	CurrentBranch(ctx context.Context) (string, error)
	StatusPorcelain(ctx context.Context) ([]byte, error)
	Remotes(ctx context.Context) ([]Remote, error)
	Fetch(ctx context.Context, remote, refspec string, args ...string) error
	Checkout(ctx context.Context, args ...string) error
}

type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
}

type execGit struct{}

// gitRunner is replaced by tests and otherwise invokes git with argument
// arrays. It is deliberately not a shell command runner.
var gitRunner Git = execGit{}

func (execGit) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return out, nil
}

func (g execGit) Root(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("not inside a git repository")
	}
	return root, nil
}

func (g execGit) CurrentBranch(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("resolve current git branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", fmt.Errorf("could not determine current git branch (HEAD is detached)")
	}
	return branch, nil
}

func (g execGit) StatusPorcelain(ctx context.Context) ([]byte, error) {
	return g.run(ctx, "status", "--porcelain")
}

func (g execGit) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := g.run(ctx, "remote", "-v")
	if err != nil {
		return nil, fmt.Errorf("list git remotes: %w", err)
	}
	byName := map[string]*Remote{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name, rawURL, kind := fields[0], fields[1], strings.Trim(fields[2], "()")
		remote := byName[name]
		if remote == nil {
			remote = &Remote{Name: name}
			byName[name] = remote
			order = append(order, name)
		}
		if kind == "push" {
			remote.PushURL = rawURL
		} else {
			remote.FetchURL = rawURL
		}
	}
	remotes := make([]Remote, 0, len(order))
	for _, name := range order {
		remotes = append(remotes, *byName[name])
	}
	return remotes, nil
}

func (g execGit) Fetch(ctx context.Context, remote, refspec string, args ...string) error {
	argv := []string{"fetch"}
	argv = append(argv, args...)
	argv = append(argv, remote, refspec)
	if _, err := g.run(ctx, argv...); err != nil {
		return fmt.Errorf("fetch source ref: %w", err)
	}
	return nil
}

func (g execGit) Checkout(ctx context.Context, args ...string) error {
	argv := append([]string{"checkout"}, args...)
	if _, err := g.run(ctx, argv...); err != nil {
		return fmt.Errorf("checkout source: %w", err)
	}
	return nil
}

// The following optional interfaces add operations needed for safe mutation
// without expanding the public Git test seam from the issue description.
type gitRemoteManager interface {
	AddRemote(context.Context, string, string) error
}
type gitBranchState interface {
	BranchStatus(context.Context, string) (bool, string, error)
	IsAncestor(context.Context, string, string) (bool, error)
	SetUpstream(context.Context, string, string) error
}
type gitSubmodules interface{ UpdateSubmodules(context.Context) error }
type gitTracking interface {
	TrackingRemote(context.Context) (string, string, error)
}
type gitCommit interface {
	CurrentCommit(context.Context) (string, error)
}

func (g execGit) AddRemote(ctx context.Context, name, remoteURL string) error {
	if _, err := g.run(ctx, "remote", "add", name, remoteURL); err != nil {
		return fmt.Errorf("add git remote %q: %w", name, err)
	}
	return nil
}

func (g execGit) BranchStatus(ctx context.Context, branch string) (bool, string, error) {
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, "", nil
		}
		return false, "", fmt.Errorf("check local branch %q: %w", branch, err)
	}
	out, err := g.run(ctx, "rev-parse", "refs/heads/"+branch)
	return true, strings.TrimSpace(string(out)), err
}

func (g execGit) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("compare git commits: %w", err)
	}
	return true, nil
}

func (g execGit) SetUpstream(ctx context.Context, branch, upstream string) error {
	if _, err := g.run(ctx, "branch", "--set-upstream-to", upstream, branch); err != nil {
		return fmt.Errorf("set branch upstream: %w", err)
	}
	return nil
}

func (g execGit) UpdateSubmodules(ctx context.Context) error {
	if _, err := g.run(ctx, "submodule", "update", "--init", "--recursive"); err != nil {
		return fmt.Errorf("update submodules: %w", err)
	}
	return nil
}

func (g execGit) CurrentCommit(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve current git commit: %w", err)
	}
	commit := strings.TrimSpace(string(out))
	if !validCommit(commit) {
		return "", fmt.Errorf("git returned an invalid current commit")
	}
	return commit, nil
}

func (g execGit) TrackingRemote(ctx context.Context) (string, string, error) {
	out, err := g.run(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", "", nil
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", nil
	}
	return parts[0], parts[1], nil
}

func currentGitBranch() (string, error) {
	return gitRunner.CurrentBranch(context.Background())
}
